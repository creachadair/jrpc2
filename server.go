package jrpc2

import (
	"container/list"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// A Server is a JSON-RPC 2.0 server. The server receives requests and sends
// responses on a Channel provided by the caller, and dispatches requests to
// user-defined Method handlers.
type Server struct {
	wg     sync.WaitGroup               // ready when workers are done at shutdown time
	mux    Assigner                     // associates method names with handlers
	sem    *semaphore.Weighted          // bounds concurrent execution (default 1)
	allow1 bool                         // allow v1 requests with no version marker
	log    func(string, ...interface{}) // write debug logs here
	dectx  func(context.Context, json.RawMessage) (context.Context, json.RawMessage, error)

	mu      *sync.Mutex // protects the fields below
	err     error       // error from a previous operation
	work    *sync.Cond  // for signaling message availability
	inq     *list.List  // inbound requests awaiting processing
	ch      Channel     // the channel to the client
	metrics *Metrics    // metrics collected during execution

	// For each request ID currently in-flight, this map carries a cancel
	// function attached to the context that was sent to the handler.
	used map[string]context.CancelFunc
}

// NewServer returns a new unstarted server that will dispatch incoming
// JSON-RPC requests according to mux. To start serving, call Start.
//
// N.B. It is only safe to modify mux after the server has been started if mux
// itself is safe for concurrent use by multiple goroutines.
//
// This function will panic if mux == nil.
func NewServer(mux Assigner, opts *ServerOptions) *Server {
	if mux == nil {
		panic("nil assigner")
	}
	s := &Server{
		mux:     mux,
		sem:     semaphore.NewWeighted(opts.concurrency()),
		allow1:  opts.allowV1(),
		log:     opts.logger(),
		dectx:   opts.decodeContext(),
		mu:      new(sync.Mutex),
		metrics: newMetrics(),
	}
	return s
}

// Start enables processing of requests from c. This function will panic if the
// server is already running.
func (s *Server) Start(c Channel) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		panic("server is already running")
	}

	// Set up the queues and condition variable used by the workers.
	s.ch = c
	s.work = sync.NewCond(s.mu)
	s.inq = list.New()
	s.used = make(map[string]context.CancelFunc)

	// Reset all the I/O structures and start up the workers.
	s.err = nil

	// TODO(fromberger): Disallow extra fields once 1.10 lands.

	// The task group carries goroutines dispatched to handle individual
	// request messages; the waitgroup maintains the persistent goroutines for
	// receiving input and processing the request queue.
	s.wg.Add(2)

	// Accept requests from the client and enqueue them for processing.
	go func() { defer s.wg.Done(); s.read(c) }()

	// Remove requests from the queue and dispatch them to handlers.  The
	// responses are written back by the handler goroutines.
	go func() {
		defer s.wg.Done()
		for {
			next, err := s.nextRequest()
			if err != nil {
				s.log("Reading next request: %v", err)
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				next()
			}()
		}
	}()
	return s
}

// nextRequest blocks until a request batch is available and returns a function
// dispatches it to the appropriate handlers. The result is only an error if
// the connection failed; errors reported by the handler are reported to the
// caller and not returned here.
//
// The caller must invoke the returned function to complete the request.
func (s *Server) nextRequest() (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.ch != nil && s.inq.Len() == 0 {
		s.work.Wait()
	}
	if s.ch == nil && s.inq.Len() == 0 {
		return nil, s.err
	}
	ch := s.ch // capture

	next := s.inq.Remove(s.inq.Front()).(jrequests)
	s.log("Processing %d requests", len(next))

	// Resolve all the task handlers or record errors.
	var tasks tasks
	for _, req := range next {
		s.log("Checking request for %q: %s", req.M, string(req.P))
		t := &task{req: req}
		req.ID = fixID(req.ID)
		if id := string(req.ID); id != "" && s.used[id] != nil {
			t.err = Errorf(E_InvalidRequest, "duplicate request id %q", id)
		} else if !s.versionOK(req.V) {
			t.err = Errorf(E_InvalidRequest, "incorrect version marker %q", req.V)
		} else if req.M == "" {
			t.err = Errorf(E_InvalidRequest, "empty method name")
		} else if m := s.assign(req.M); m == nil {
			t.err = Errorf(E_MethodNotFound, "no such method %q", req.M)
		} else if base, params, err := s.dectx(context.Background(), json.RawMessage(req.P)); err != nil {
			t.err = Errorf(E_InternalError, "invalid request context: %v", err)
		} else {
			t.m = m
			t.params = params
			t.ctx = base
			if id != "" {
				ctx, cancel := context.WithCancel(base)
				s.used[id] = cancel
				t.ctx = ctx
			}
		}
		if t.err != nil {
			s.log("Task error: %v", t.err)
			s.metrics.Count("rpc.errors", 1)
		}
		tasks = append(tasks, t)
	}

	// Invoke the handlers outside the lock.
	return func() error {
		start := time.Now()
		var wg sync.WaitGroup
		for _, t := range tasks {
			if t.err != nil {
				continue // nothing to do here; this was a bogus one
			}
			t := t
			wg.Add(1)
			go func() {
				defer wg.Done()
				t.val, t.err = s.dispatch(t.ctx, t.m, &Request{
					id:     t.req.ID,
					method: t.req.M,
					params: t.params,
				})
			}()
		}
		wg.Wait()
		rsps := tasks.responses()
		s.log("Completed %d responses [%v elapsed]", len(rsps), time.Since(start))

		// Deliver any responses (or errors) we owe.
		if len(rsps) != 0 {
			s.log("Sending response: %v", rsps)
			s.mu.Lock()
			defer s.mu.Unlock()

			// Ensure all the inflight requests get their contexts cancelled.
			for _, rsp := range rsps {
				if cancel, ok := s.used[string(rsp.ID)]; ok {
					cancel()
				}
			}

			nw, err := encode(ch, rsps)
			s.metrics.Count("rpc.bytesWritten", int64(nw))
			return err
		}
		return nil
	}, nil
}

// dispatch invokes m for the specified request type, and marshals the return
// value into JSON if there is one.
func (s *Server) dispatch(base context.Context, m Method, req *Request) (json.RawMessage, error) {
	ctx := context.WithValue(base, inboundRequestKey, req)
	ctx = context.WithValue(ctx, metricsWriterKey, s.metrics)
	if err := s.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.sem.Release(1)

	v, err := m.Call(ctx, req)
	if err != nil {
		if req.IsNotification() {
			s.log("Discarding error from notification to %q: %v", req.Method(), err)
			return nil, nil // a notification
		}
		return nil, err // a call reporting an error
	}
	return json.Marshal(v)
}

// ServerInfo returns an atomic snapshot of the current server info for s.
func (s *Server) ServerInfo() *ServerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverInfo()
}

// serverInfo returns a snapshot of the current server info for s. It requires
// the caller hold s.mu.
func (s *Server) serverInfo() *ServerInfo {
	info := &ServerInfo{
		Methods:  s.mux.Names(),
		Counter:  make(map[string]int64),
		MaxValue: make(map[string]int64),
	}
	s.metrics.snapshot(info.Counter, info.MaxValue)
	return info
}

// Stop shuts down the server. It is safe to call this method multiple times or
// from concurrent goroutines; it will only take effect once.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop(errServerStopped)
}

// Wait blocks until the connection terminates and returns the resulting error.
func (s *Server) Wait() error {
	s.wg.Wait()
	s.work = nil
	s.used = nil
	return s.err
}

// stop shuts down the connection and records err as its final state.  The
// caller must hold s.mu. If multiple callers invoke stop, only the first will
// successfully record its error status.
func (s *Server) stop(err error) {
	if s.ch == nil {
		return // nothing is running
	}
	s.log("Server signaled to stop with err=%v", err)
	s.ch.Close()

	// Remove any pending requests from the queue, but retain notifications.
	// The server will process pending notifications before giving up.
	for cur := s.inq.Front(); cur != nil; cur = cur.Next() {
		var keep jrequests
		for _, req := range cur.Value.(jrequests) {
			if req.ID == nil {
				keep = append(keep, req)
				s.log("Retaining notification %+v", req)
			} else if cancel, ok := s.used[string(req.ID)]; ok {
				cancel()
			}
		}
		if len(keep) != 0 {
			s.inq.PushBack(keep)
		}
		s.inq.Remove(cur)
	}
	s.work.Broadcast()
	s.err = err
	s.ch = nil
}

func isRecoverableJSONError(err error) bool {
	switch err.(type) {
	case *json.UnmarshalTypeError, *json.UnsupportedTypeError:
		// Do not include syntax errors, as the decoder will not generally
		// recover from these without more serious help.
		return true
	default:
		return false
	}
}

func (s *Server) read(ch Channel) {
	for {
		// If the message is not sensible, report an error; otherwise enqueue
		// it for processing.
		var in jrequests
		bits, err := ch.Recv()
		if err == nil || (err == io.EOF && len(bits) != 0) {
			err = json.Unmarshal(bits, &in)
		}

		s.mu.Lock()
		s.metrics.Count("rpc.requests", int64(len(in)))
		s.metrics.Count("rpc.bytesRead", int64(len(bits)))
		s.metrics.SetMaxValue("rpc.maxBytesRead", int64(len(bits)))
		if isRecoverableJSONError(err) {
			s.pushError(nil, jerrorf(E_ParseError, "invalid JSON request message"))
		} else if err != nil {
			s.stop(err)
			s.mu.Unlock()
			return
		} else if len(in) == 0 {
			s.pushError(nil, jerrorf(E_InvalidRequest, "empty request batch"))
		} else {
			s.log("Received %d new requests", len(in))
			s.inq.PushBack(in)
			s.work.Broadcast()
		}
		s.mu.Unlock()
	}
}

// ServerInfo is the concrete type of responses from the rpc.serverInfo method.
type ServerInfo struct {
	// The list of method names exported by this server.
	Methods []string `json:"methods,omitempty"`

	// Metric values defined by the evaluation of methods.
	Counter  map[string]int64 `json:"counters,omitempty"`
	MaxValue map[string]int64 `json:"maxValue,omitempty"`
}

// assign returns a Method to handle the specified name, or nil.
// The caller must hold s.mu.
func (s *Server) assign(name string) Method {
	switch name {
	case "rpc.serverInfo":
		info := s.serverInfo()
		return methodFunc(func(context.Context, *Request) (interface{}, error) {
			return info, nil
		})

	case "rpc.cancel":
		// Handle client-requested cancellation of a pending method. This only
		// works if issued as a notification.
		return methodFunc(func(_ context.Context, req *Request) (interface{}, error) {
			if !req.IsNotification() {
				return nil, Errorf(E_MethodNotFound, "no such method: %q", name)
			}
			var ids []json.RawMessage
			if err := req.UnmarshalParams(&ids); err != nil {
				return nil, err
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, raw := range ids {
				id := string(raw)
				if cancel, ok := s.used[id]; ok {
					cancel()
					s.log("Cancelled request %s by client order", id)
				}
			}
			return nil, nil
		})
	}
	return s.mux.Assign(name)
}

// pushError reports an error for the given request ID.
// Requires that the caller hold s.mu.
func (s *Server) pushError(id json.RawMessage, jerr *jerror) {
	s.log("Error for request %q: %v", string(id), jerr)
	nw, err := encode(s.ch, jresponses{{
		V:  Version,
		ID: id,
		E:  jerr,
	}})
	s.metrics.Count("rpc.errors", 1)
	s.metrics.Count("rpc.bytesWritten", int64(nw))
	if err != nil {
		s.log("Writing error response: %v", err)
	}
}

func (s *Server) versionOK(v string) bool {
	if v == "" {
		return s.allow1 // an empty version is OK if the server allows it
	}
	return v == Version // ... otherwise it must match the spec
}

type task struct {
	m      Method
	req    *jrequest
	val    json.RawMessage
	ctx    context.Context
	params json.RawMessage
	err    error
}

type tasks []*task

func (ts tasks) responses() jresponses {
	var rsps jresponses
	for _, task := range ts {
		if task.req.ID == nil {
			// Spec: "The Server MUST NOT reply to a Notification, including
			// those that are within a batch request.  Notifications are not
			// confirmable by definition, since they do not have a Response
			// object to be returned. As such, the Client would not be aware of
			// any errors."
			continue
		}
		rsp := &jresponse{V: Version, ID: task.req.ID}
		if task.err == nil {
			rsp.R = task.val
		} else if task.err == context.Canceled {
			rsp.E = jerrorf(E_Cancelled, E_Cancelled.Error())
		} else if task.err == context.DeadlineExceeded {
			rsp.E = jerrorf(E_DeadlineExceeded, E_DeadlineExceeded.Error())
		} else if e, ok := task.err.(*Error); ok {
			rsp.E = e.tojerror()
		} else if code, ok := task.err.(Code); ok {
			rsp.E = jerrorf(code, code.Error())
		} else {
			rsp.E = jerrorf(E_InternalError, "internal error: %v", task.err)
		}
		rsps = append(rsps, rsp)
	}
	return rsps
}

// InboundRequest returns the inbound request associated with the given
// context, or nil if ctx does not have an inbound request.
//
// This is mainly of interest to wrapped server methods that do not have the
// request as an explicit parameter; for direct implementations of Method.Call
// the request value returned by InboundRequest will be the same value as was
// passed explicitly.
func InboundRequest(ctx context.Context) *Request {
	if v := ctx.Value(inboundRequestKey); v != nil {
		return v.(*Request)
	}
	return nil
}

// MetricsWriter returns a metrics writer associated with the given context, or
// nil if ctx doees not have a metrics writer.
func MetricsWriter(ctx context.Context) *Metrics {
	if v := ctx.Value(metricsWriterKey); v != nil {
		return v.(*Metrics)
	}
	return nil
}

// requestContextKey is the concrete type of the context key used to dispatch
// the request context in to handlers.
type requestContextKey string

const inboundRequestKey = requestContextKey("inbound-request")
const metricsWriterKey = requestContextKey("metrics-writer")

// A Metrics value captures counters and maximum value trackers.  A nil
// *Metrics is valid, and discards all metrics. A *Metrics value is safe for
// concurrent use by multiple goroutines.
type Metrics struct {
	mu      sync.Mutex
	counter map[string]int64
	maxVal  map[string]int64
}

func newMetrics() *Metrics {
	return &Metrics{counter: make(map[string]int64), maxVal: make(map[string]int64)}
}

// Count adds n to the current value of the counter named, defining the counter
// if it does not already exist.
func (m *Metrics) Count(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.counter[name] += n
	}
}

// SetMaxValue sets the maximum value metric named to the greater of n and its
// current value, defining the value if it does not already exist.
func (m *Metrics) SetMaxValue(name string, n int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		if n > m.maxVal[name] {
			m.maxVal[name] = n
		}
	}
}

// snapshot copies an atomic snapshot of the counters and max value trackers
// into the provided non-nil maps.
func (m *Metrics) snapshot(ctr, mval map[string]int64) {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		for name, val := range m.counter {
			ctr[name] = val
		}
		for name, val := range m.maxVal {
			mval[name] = val
		}
	}
}
