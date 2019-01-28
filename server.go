package jrpc2

import (
	"container/list"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/code"
	"bitbucket.org/creachadair/jrpc2/metrics"
	"golang.org/x/sync/semaphore"
)

type logger = func(string, ...interface{})

// A Server is a JSON-RPC 2.0 server. The server receives requests and sends
// responses on a channel.Channel provided by the caller, and dispatches
// requests to user-defined Handlers.
type Server struct {
	wg      sync.WaitGroup      // ready when workers are done at shutdown time
	mux     Assigner            // associates method names with handlers
	sem     *semaphore.Weighted // bounds concurrent execution (default 1)
	allow1  bool                // allow v1 requests with no version marker
	allowP  bool                // allow server notifications to the client
	log     logger              // write debug logs here
	dectx   decoder             // decode context from request
	ckauth  verifier            // check request authorization
	expctx  bool                // whether to expect request context
	metrics *metrics.M          // metrics collected during execution
	start   time.Time           // when Start was called

	// If rpc.* method handlers are enabled, these are their handlers.
	rpcHandlers map[string]Handler

	mu *sync.Mutex // protects the fields below

	err  error           // error from a previous operation
	work *sync.Cond      // for signaling message availability
	inq  *list.List      // inbound requests awaiting processing
	ch   channel.Channel // the channel to the client

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
	dc, exp := opts.decodeContext()
	s := &Server{
		mux:     mux,
		sem:     semaphore.NewWeighted(opts.concurrency()),
		allow1:  opts.allowV1(),
		allowP:  opts.allowPush(),
		log:     opts.logger(),
		dectx:   dc,
		ckauth:  opts.checkAuth(),
		expctx:  exp,
		mu:      new(sync.Mutex),
		metrics: opts.metrics(),
		start:   opts.startTime(),
		inq:     list.New(),
		used:    make(map[string]context.CancelFunc),
	}
	s.work = sync.NewCond(s.mu)
	if opts.allowBuiltin() {
		s.installBuiltins()
	}
	return s
}

// Start enables processing of requests from c. This function will panic if the
// server is already running.
func (s *Server) Start(c channel.Channel) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		panic("server is already running")
	}

	// Set up the queues and condition variable used by the workers.
	s.ch = c
	if s.start.IsZero() {
		s.start = time.Now().In(time.UTC)
	}

	// Reset all the I/O structures and start up the workers.
	s.err = nil

	// s.wg waits for the maintenance goroutines for receiving input and
	// processing the request queue. In addition, each request in flight adds a
	// goroutine to s.wg. At server shutdown, s.wg completes when the
	// maintenance goroutines and all pending requests are finished.
	s.wg.Add(2)

	// Accept requests from the client and enqueue them for processing.
	go func() { defer s.wg.Done(); s.read(c) }()

	// Remove requests from the queue and dispatch them to handlers.
	go func() { defer s.wg.Done(); s.serve() }()

	return s
}

// serve processes requests from the queue and dispatches them to handlers.
// The responses are written back by the handler goroutines.
//
// The flow of an inbound request is:
//
//   serve             -- main serving loop
//   * nextRequest     -- process the next request batch
//     * dispatch
//       * assign      -- assign handlers to requests
//       | ...
//       |
//       * invoke      -- invoke handlers
//       | \ handler   -- handle an individual request
//       |   ...
//       * deliver     -- send responses to the client
//
func (s *Server) serve() {
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
}

// nextRequest blocks until a request batch is available and returns a function
// that dispatches it to the appropriate handlers. The result is only an error
// if the connection failed; errors reported by the handler are reported to the
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

	// Construct a dispatcher to run the handlers outside the lock.
	return s.dispatch(next, ch), nil
}

// dispatch constructs a function that invokes each of the specified tasks.
// The caller must hold s.mu when calling dispatch, but the returned function
// should be executed outside the lock to wait for the handlers to return.
func (s *Server) dispatch(next jrequests, ch channel.Sender) func() error {
	// Resolve all the task handlers or record errors.
	start := time.Now()
	tasks := s.checkAndAssign(next)
	var wg sync.WaitGroup
	for _, t := range tasks {
		if t.err != nil {
			continue // nothing to do here; this was a bogus one
		}
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.val, t.err = s.invoke(t.ctx, t.m, &Request{
				id:     t.reqID,
				method: t.reqM,
				params: t.params,
			})
		}()
	}

	// Wait for all the handlers to return, then deliver any responses.
	return func() error {
		wg.Wait()
		return s.deliver(tasks.responses(), ch, time.Since(start))
	}
}

// deliver cleans up completed responses and arranges their replies (if any) to
// be sent back to the client.
func (s *Server) deliver(rsps jresponses, ch channel.Sender, elapsed time.Duration) error {
	if len(rsps) == 0 {
		return nil
	}
	s.log("Completed %d requests [%v elapsed]", len(rsps), elapsed)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure all the inflight requests get their contexts cancelled.
	for _, rsp := range rsps {
		s.cancel(string(rsp.ID))
	}

	nw, err := encode(ch, rsps)
	s.metrics.CountAndSetMax("rpc.bytesWritten", int64(nw))
	return err
}

// checkAndAssign resolves all the task handlers for the given batch, or
// records errors for them as appropriate. The caller must hold s.mu.
func (s *Server) checkAndAssign(next jrequests) tasks {
	var ts tasks
	for _, req := range next {
		s.log("Checking request for %q: %s", req.M, string(req.P))
		t := &task{reqID: req.ID, reqM: req.M}
		req.ID = fixID(req.ID)
		if id := string(req.ID); id != "" && s.used[id] != nil {
			t.err = Errorf(code.InvalidRequest, "duplicate request id %q", id)
		} else if !s.versionOK(req.V) {
			t.err = ErrInvalidVersion
		} else if req.M == "" {
			t.err = Errorf(code.InvalidRequest, "empty method name")
		} else if m := s.assign(req.M); m == nil {
			t.err = Errorf(code.MethodNotFound, "no such method %q", req.M)
		} else if s.setContext(t, id, req.M, req.P) {
			t.m = m
		}
		if t.err != nil {
			s.log("Task error: %v", t.err)
			s.metrics.Count("rpc.errors", 1)
		}
		ts = append(ts, t)
	}
	return ts
}

// setContext constructs and attaches a request context to t, and reports
// whether this succeeded.
func (s *Server) setContext(t *task, id, method string, rawParams json.RawMessage) bool {
	base, params, err := s.dectx(context.Background(), method, rawParams)
	t.params = params
	if err != nil {
		t.err = Errorf(code.InternalError, "invalid request context: %v", err)
		return false
	}

	// Check authorization.
	if err := s.ckauth(base, method, []byte(params)); err != nil {
		t.err = Errorf(code.NotAuthorized, "%v: %v", code.NotAuthorized.String(), err)
		return false
	}

	// Store the cancellation for a request that needs a reply, so that we can
	// respond to rpc.cancel requests.
	if id != "" {
		ctx, cancel := context.WithCancel(base)
		s.used[id] = cancel
		t.ctx = ctx
	} else {
		t.ctx = base
	}
	return true
}

// invoke invokes the handler m for the specified request type, and marshals
// the return value into JSON if there is one.
func (s *Server) invoke(base context.Context, h Handler, req *Request) (json.RawMessage, error) {
	ctx := context.WithValue(base, inboundRequestKey{}, req)
	ctx = context.WithValue(ctx, serverMetricsKey{}, s.metrics)
	if s.allowP {
		ctx = context.WithValue(ctx, serverPushKey{}, s.Push)
	}
	if err := s.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.sem.Release(1)

	v, err := h.Handle(ctx, req)
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

// Push posts a server-side notification to the client.  This is a non-standard
// extension of JSON-RPC, and may not be supported by all clients.  Unless s
// was constructed with the AllowPush option set true, this method will always
// report an error (ErrNotifyUnsupported) without sending anything.
func (s *Server) Push(ctx context.Context, method string, params interface{}) error {
	if !s.allowP {
		return ErrNotifyUnsupported
	}
	var bits []byte
	if params != nil {
		v, err := json.Marshal(params)
		if err != nil {
			return err
		}
		bits = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("Posting server notification %q %s", method, string(bits))
	nw, err := encode(s.ch, jresponses{{
		V: Version,
		M: method,
		P: bits,
	}})
	s.metrics.CountAndSetMax("rpc.bytesWritten", int64(nw))
	s.metrics.Count("rpc.notifications", 1)
	return err
}

// serverInfo returns a snapshot of the current server info for s. It requires
// the caller hold s.mu.
func (s *Server) serverInfo() *ServerInfo {
	info := &ServerInfo{
		Methods:     s.mux.Names(),
		UsesContext: s.expctx,
		StartTime:   s.start,
		Counter:     make(map[string]int64),
		MaxValue:    make(map[string]int64),
		Label:       make(map[string]string),
	}
	s.metrics.Snapshot(metrics.Snapshot{
		Counter:  info.Counter,
		MaxValue: info.MaxValue,
		Label:    info.Label,
	})
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
// After Wait returns, whether or not there was an error, it is safe to call
// s.Start again to restart the server with a fresh channel.
func (s *Server) Wait() error {
	s.wg.Wait()
	// Sanity check.
	if s.inq.Len() != 0 {
		panic("s.inq is not empty at shutdown")
	}
	// Don't remark on a closed channel or EOF as a noteworthy failure.
	if s.err == io.EOF || channel.IsErrClosing(s.err) || s.err == errServerStopped {
		return nil
	}
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
				s.log("Retaining notification %p", req)
			} else {
				s.cancel(string(req.ID))
			}
		}
		if len(keep) != 0 {
			s.inq.PushBack(keep)
		}
		s.inq.Remove(cur)
	}
	s.work.Broadcast()

	// Cancel any in-flight requests that made it out of the queue.
	for id, cancel := range s.used {
		cancel()
		delete(s.used, id)
	}

	// Sanity check.
	if len(s.used) != 0 {
		panic("s.used is not empty at shutdown")
	}

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

// read is the main receiver loop, decoding requests from the client and adding
// them to the queue. Decoding errors and message-format problems are handled
// and reported back to the client directly, so that any message that survives
// into the request queue is structurally valid.
func (s *Server) read(ch channel.Receiver) {
	for {
		// TODO(fromberger): Disallow extra fields once 1.10 lands.

		// If the message is not sensible, report an error; otherwise enqueue
		// it for processing.
		var in jrequests
		bits, err := ch.Recv()
		if err == nil || (err == io.EOF && len(bits) != 0) {
			err = json.Unmarshal(bits, &in)
		}

		s.metrics.Count("rpc.requests", int64(len(in)))
		s.metrics.CountAndSetMax("rpc.bytesRead", int64(len(bits)))
		s.mu.Lock()
		if err != nil {
			if e, ok := err.(*Error); ok {
				s.pushError(e.data, jerrorf(e.code, e.message))
			} else if isRecoverableJSONError(err) {
				s.pushError(nil, jerrorf(code.ParseError, "invalid JSON request message"))
			} else {
				s.stop(err)
				s.mu.Unlock()
				return
			}
		} else if len(in) == 0 {
			s.pushError(nil, jerrorf(code.InvalidRequest, "empty request batch"))
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

	// Whether this server understands context wrappers.
	UsesContext bool `json:"usesContext"`

	// Metric values defined by the evaluation of methods.
	Counter  map[string]int64  `json:"counters,omitempty"`
	MaxValue map[string]int64  `json:"maxValue,omitempty"`
	Label    map[string]string `json:"labels,omitempty"`

	// When the server started.
	StartTime time.Time `json:"startTime,omitempty"`
}

// assign returns a Handler to handle the specified name, or nil.
// The caller must hold s.mu.
func (s *Server) assign(name string) Handler {
	if h, ok := s.rpcHandlers[name]; ok {
		return h
	} else if strings.HasPrefix(name, "rpc.") && s.rpcHandlers != nil {
		return nil
	}
	return s.mux.Assign(name)
}

// pushError reports an error for the given request ID directly back to the
// client, bypassing the normal request handling mechanism.  The caller must
// hold s.mu when calling this method.
func (s *Server) pushError(id json.RawMessage, jerr *jerror) {
	s.log("Error for request %q: %v", string(id), jerr)
	nw, err := encode(s.ch, jresponses{{
		V:  Version,
		ID: id,
		E:  jerr,
	}})
	s.metrics.Count("rpc.errors", 1)
	s.metrics.CountAndSetMax("rpc.bytesWritten", int64(nw))
	if err != nil {
		s.log("Writing error response: %v", err)
	}
}

// cancel reports whether id is an active call.  If so, it also calls the
// cancellation function associated with id and removes it from the
// reservations. The caller must hold s.mu.
func (s *Server) cancel(id string) bool {
	cancel, ok := s.used[id]
	if ok {
		cancel()
		delete(s.used, id)
	}
	return ok
}

func (s *Server) versionOK(v string) bool {
	if v == "" {
		return s.allow1 // an empty version is OK if the server allows it
	}
	return v == Version // ... otherwise it must match the spec
}

// A task represents a pending method invocation received by the server.
type task struct {
	m Handler // the assigned handler (after assignment)

	ctx    context.Context // the context passed to the handler
	reqID  json.RawMessage // the original request ID
	reqM   string          // the original method name
	params json.RawMessage // the encoded parameters

	val json.RawMessage // the result value (when complete)
	err error           // the error value (when complete)
}

type tasks []*task

func (ts tasks) responses() jresponses {
	var rsps jresponses
	for _, task := range ts {
		if task.reqID == nil {
			// Spec: "The Server MUST NOT reply to a Notification, including
			// those that are within a batch request.  Notifications are not
			// confirmable by definition, since they do not have a Response
			// object to be returned. As such, the Client would not be aware of
			// any errors."
			continue
		}
		rsp := &jresponse{V: Version, ID: task.reqID}
		if task.err == nil {
			rsp.R = task.val
		} else if e, ok := task.err.(*Error); ok {
			rsp.E = e.tojerror()
		} else if c := code.FromError(task.err); c != code.NoError {
			rsp.E = jerrorf(c, "%v: %v", c.String(), task.err)
		} else {
			rsp.E = jerrorf(code.InternalError, "internal error: %v", task.err)
		}
		rsps = append(rsps, rsp)
	}
	return rsps
}
