// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/code"
	"golang.org/x/sync/semaphore"
)

var (
	serverMetrics = new(expvar.Map)

	serversActiveGauge     = new(expvar.Int)
	rpcRequestsCount       = new(expvar.Int)
	rpcErrorsCount         = new(expvar.Int)
	bytesReadCount         = new(expvar.Int)
	bytesWrittenCount      = new(expvar.Int)
	rpcCallsPushed         = new(expvar.Int)
	rpcNotificationsPushed = new(expvar.Int)
)

func init() {
	serverMetrics.Set("servers_active", serversActiveGauge)
	serverMetrics.Set("rpc_requests", rpcRequestsCount)
	serverMetrics.Set("rpc_errors", rpcErrorsCount)
	serverMetrics.Set("bytes_read", bytesReadCount)
	serverMetrics.Set("bytes_written", bytesWrittenCount)
	serverMetrics.Set("calls_pushed", rpcCallsPushed)
	serverMetrics.Set("notifications_pushed", rpcNotificationsPushed)
}

// ServerMetrics returns a map of exported server metrics for use with the
// expvar package. This map is shared among all server instances created by
// NewServer. The caller is free to add or remove metrics in the map, but note
// that such changes will affect all servers.
//
// The caller is responsible for publishing the metrics to the exporter via
// expvar.Publish or similar.
func ServerMetrics() *expvar.Map { return serverMetrics }

// A Server is a JSON-RPC 2.0 server. The server receives requests and sends
// responses on a channel.Channel provided by the caller, and dispatches
// requests to user-defined Handlers.
type Server struct {
	wg  sync.WaitGroup      // ready when workers are done at shutdown time
	mux Assigner            // associates method names with handlers
	sem *semaphore.Weighted // bounds concurrent execution (default 1)

	// Configurable settings
	allowP  bool                   // allow server notifications to the client
	log     func(string, ...any)   // write debug logs here
	rpcLog  RPCLogger              // log RPC requests and responses here
	newctx  func() context.Context // create a new base request context
	start   time.Time              // when Start was called
	builtin bool                   // whether built-in rpc.* methods are enabled

	mu *sync.Mutex // protects the fields below

	nbar sync.WaitGroup  // notification barrier (see the dispatch method)
	err  error           // error from a previous operation
	work chan struct{}   // for signaling message availability
	inq  *queue          // inbound requests awaiting processing
	ch   channel.Channel // the channel to the client

	// For each request ID currently in-flight, this map carries a cancel
	// function attached to the context that was sent to the handler.
	used map[string]context.CancelFunc

	// For each push-call ID currently in flight, this map carries the response
	// waiting for its reply.
	call   map[string]*Response
	callID int64
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
		allowP:  opts.allowPush(),
		log:     opts.logFunc(),
		rpcLog:  opts.rpcLog(),
		newctx:  opts.newContext(),
		mu:      new(sync.Mutex),
		start:   opts.startTime(),
		builtin: opts.allowBuiltin(),
		inq:     newQueue(),
		used:    make(map[string]context.CancelFunc),
		call:    make(map[string]*Response),
		callID:  1,
	}
	return s
}

// Start enables processing of requests from c and returns. Start does not
// block while the server runs. This function will panic if the server is
// already running. It returns s to allow chaining with construction.
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
	serversActiveGauge.Add(1)

	// Reset all the I/O structures and start up the workers.
	s.err = nil

	// Reset the signal channel.
	s.work = make(chan struct{}, 1)

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
//	serve             -- main serving loop
//	* nextRequest     -- process the next request batch
//	  * dispatch
//	    * assign      -- assign handlers to requests
//	    | ...
//	    |
//	    * invoke      -- invoke handlers
//	    | \ handler   -- handle an individual request
//	    |   ...
//	    * deliver     -- send responses to the client
func (s *Server) serve() {
	for {
		next, err := s.nextRequest()
		if err != nil {
			s.log("Error reading from client: %v", err)
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			next()
		}()
	}
}

func (s *Server) signal() {
	select {
	case s.work <- struct{}{}:
	default:
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
	for s.ch != nil && s.inq.isEmpty() {
		s.mu.Unlock()
		<-s.work
		s.mu.Lock()
	}
	if s.ch == nil && s.inq.isEmpty() {
		return nil, s.err
	}
	ch := s.ch // capture

	next := s.inq.pop()
	s.log("Dequeued request batch of length %d (qlen=%d)", len(next), s.inq.size())

	// Construct a dispatcher to run the handlers outside the lock.
	return s.dispatch(next, ch), nil
}

// waitForBarrier blocks until all notification handlers that have been issued
// have completed, then adds n to the barrier.
//
// The caller must hold s.mu, but the lock is released during the wait to avert
// a deadlock with handlers calling back into the server.  See #27.
// s.nbar counts the number of notifications that have been issued and are not
// yet complete.
func (s *Server) waitForBarrier(n int) {
	s.mu.Unlock()
	defer s.mu.Lock()
	s.nbar.Wait()
	s.nbar.Add(n)
}

// dispatch constructs a function that invokes each of the specified tasks.
// The caller must hold s.mu when calling dispatch, but the returned function
// should be executed outside the lock to wait for the handlers to return.
//
// dispatch blocks until any notification received prior to this batch has
// completed, to ensure that notifications are processed in a partial order
// that respects order of receipt. Notifications within a batch are handled
// concurrently.
func (s *Server) dispatch(next jmessages, ch sender) func() error {
	// Resolve all the task handlers or record errors.
	start := time.Now()
	tasks := s.checkAndAssign(next)

	// Ensure all notifications already issued have completed; see #24.
	todo, notes := tasks.numToDo()
	s.waitForBarrier(notes)

	return func() error {
		var wg sync.WaitGroup
		for _, t := range tasks {
			if t.err != nil {
				continue // nothing to do here; this task has already failed
			}

			todo--
			if todo == 0 {
				t.val, t.err = s.invoke(t.ctx, t.m, t.hreq)
				if t.hreq.IsNotification() {
					s.nbar.Done()
				}
				break
			}
			t := t
			wg.Add(1)
			go func() {
				defer wg.Done()
				t.val, t.err = s.invoke(t.ctx, t.m, t.hreq)
				if t.hreq.IsNotification() {
					s.nbar.Done()
				}
			}()
		}

		// Wait for all the handlers to return, then deliver any responses.
		wg.Wait()
		return s.deliver(tasks.responses(s.rpcLog), ch, time.Since(start))
	}
}

// deliver cleans up completed responses and arranges their replies (if any) to
// be sent back to the client.
func (s *Server) deliver(rsps jmessages, ch sender, elapsed time.Duration) error {
	if len(rsps) == 0 {
		return nil
	}
	s.log("Completed %d requests [%v elapsed]", len(rsps), elapsed)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel the contexts of all the inflight requests that were executed.
	// The extra check is necessary, to prevent a duplicate request from
	// cancelling its valid predecessor in that ID.
	for _, rsp := range rsps {
		if rsp.err == nil {
			s.cancel(string(rsp.ID))
		}
	}

	nw, err := encode(ch, rsps)
	bytesWrittenCount.Add(int64(nw))
	return err
}

// checkAndAssign resolves all the task handlers for the given batch, or
// records errors for them as appropriate. The caller must hold s.mu.
func (s *Server) checkAndAssign(next jmessages) tasks {
	var ts tasks
	var ids []string
	dup := make(map[string]*task) // :: id ⇒ first task in batch with id

	// Phase 1: Check for errors and duplicate request IDs.
	for _, req := range next {
		fid := fixID(req.ID)
		t := &task{
			hreq:  &Request{id: fid, method: req.M, params: req.P},
			batch: req.batch,
		}
		if req.err != nil {
			t.err = req.err
		}
		id := string(fid)
		if old := dup[id]; old != nil {
			// A previous task already used this ID, fail both.
			old.err = errDuplicateID.WithData(id)
			t.err = old.err
		} else if id != "" && s.used[id] != nil {
			// A task from a previous batch already used this ID, fail this one.
			t.err = errDuplicateID.WithData(id)
		} else if id != "" {
			// This is the first task with this ID in the batch.
			dup[id] = t
		}
		ts = append(ts, t)
		ids = append(ids, id)
	}

	// Phase 2: Assign method handlers and set up contexts.
	for i, t := range ts {
		id := ids[i]
		if t.err != nil {
			// deferred validation error
		} else if t.hreq.method == "" {
			t.err = errEmptyMethod
		} else {
			s.setContext(t, id)
			t.m = s.assign(t.ctx, t.hreq.method)
			if t.m == nil {
				t.err = errNoSuchMethod.WithData(t.hreq.method)
			}
		}

		if t.err != nil {
			s.log("Request check error for %q (params %q): %v",
				t.hreq.method, string(t.hreq.params), t.err)
			rpcErrorsCount.Add(1)
		}
	}
	return ts
}

// setContext constructs and attaches a request context to t, and reports
// whether this succeeded.
func (s *Server) setContext(t *task, id string) {
	t.ctx = context.WithValue(s.newctx(), inboundRequestKey{}, t.hreq)

	// Store the cancellation for a request that needs a reply, so that we can
	// respond to cancellation requests.
	if id != "" {
		ctx, cancel := context.WithCancel(t.ctx)
		s.used[id] = cancel
		t.ctx = ctx
	}
}

// invoke invokes the handler m for the specified request type, and marshals
// the return value into JSON if there is one.
func (s *Server) invoke(base context.Context, h Handler, req *Request) (json.RawMessage, error) {
	ctx := context.WithValue(base, serverKey{}, s)
	if err := s.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer s.sem.Release(1)

	s.rpcLog.LogRequest(ctx, req)
	v, err := h(ctx, req)
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
	info := &ServerInfo{
		Methods:   []string{"*"},
		Metrics:   make(map[string]any),
		StartTime: s.start,
	}
	serverMetrics.Do(func(kv expvar.KeyValue) {
		info.Metrics[kv.Key] = json.RawMessage(kv.Value.String())
	})
	if n, ok := s.mux.(Namer); ok {
		info.Methods = n.Names()
	}
	return info
}

// ErrPushUnsupported is returned by the Notify and Call methods if server
// pushes are not enabled.
var ErrPushUnsupported = errors.New("server push is not enabled")

// Notify posts a single server-side notification to the client.
//
// This is a non-standard extension of JSON-RPC, and may not be supported by
// all clients.  Unless s was constructed with the AllowPush option set true,
// this method will always report an error (ErrPushUnsupported) without sending
// anything.  If Notify is called after the client connection is closed, it
// returns ErrConnClosed.
func (s *Server) Notify(ctx context.Context, method string, params any) error {
	if !s.allowP {
		return ErrPushUnsupported
	}
	_, err := s.pushReq(ctx, false /* no ID */, method, params)
	return err
}

// Callback posts a single server-side call to the client. It blocks until a
// reply is received, ctx ends, or the client connection terminates.  A
// successful callback reports a nil error and a non-nil response. Errors
// returned by the client have concrete type *jrpc2.Error.
//
// This is a non-standard extension of JSON-RPC, and may not be supported by
// all clients. If you are not sure whether the client supports push calls, you
// should set a deadline on ctx, otherwise the callback may block forever for a
// client response that will never arrive.
//
// Unless s was constructed with the AllowPush option set true, this method
// will always report an error (ErrPushUnsupported) without sending
// anything. If Callback is called after the client connection is closed, it
// returns ErrConnClosed.
func (s *Server) Callback(ctx context.Context, method string, params any) (*Response, error) {
	if !s.allowP {
		return nil, ErrPushUnsupported
	}
	rsp, err := s.pushReq(ctx, true /* set ID */, method, params)
	if err != nil {
		return nil, err
	}
	rsp.wait()
	if err := rsp.Error(); err != nil {
		return nil, filterError(err)
	}
	return rsp, nil
}

// waitCallback blocks until pctx ends, and then if p is still waiting for a
// response, deliver an error to the caller.
func (s *Server) waitCallback(pctx context.Context, id string, p *Response) {
	<-pctx.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.call[id]; !ok {
		return
	}
	delete(s.call, id)
	err := pctx.Err()
	s.log("Context ended for callback id %q, err=%v", id, err)

	p.ch <- &jmessage{
		ID: json.RawMessage(id),
		E:  &Error{Code: code.FromError(err), Message: err.Error()},
	}
}

func (s *Server) pushReq(ctx context.Context, wantID bool, method string, params any) (rsp *Response, _ error) {
	var bits []byte
	if params != nil {
		v, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		bits = v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		return nil, ErrConnClosed
	}

	kind := "notification"
	var jid json.RawMessage
	if wantID {
		kind = "call"
		id := strconv.FormatInt(s.callID, 10)
		s.callID++

		cbctx, cancel := context.WithCancel(ctx)
		jid = json.RawMessage(id)
		rsp = &Response{
			ch:     make(chan *jmessage, 1),
			id:     id,
			cancel: cancel,
		}
		s.call[id] = rsp
		go s.waitCallback(cbctx, id, rsp)
		rpcCallsPushed.Add(1)
	} else {
		rpcNotificationsPushed.Add(1)
	}

	s.log("Posting server %s %q %s", kind, method, string(bits))
	nw, err := encode(s.ch, jmessages{{
		ID: jid,
		M:  method,
		P:  bits,
	}})
	bytesWrittenCount.Add(int64(nw))
	return rsp, err
}

// Stop shuts down the server. It is safe to call this method multiple times or
// from concurrent goroutines; it will only take effect once.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop(errServerStopped)
}

// ServerStatus describes the status of a stopped server.
//
// A server is said to have succeeded if it stopped because the client channel
// closed or because its Stop method was called. On success, Err == nil, and
// the flag fields indicate the reason why the server exited.
// Otherwise, Err != nil is the error value that caused the server to exit.
type ServerStatus struct {
	Err error // the error that caused the server to stop (nil on success)

	// On success, these flags explain the reason why the server stopped.
	// At most one of these fields will be true.
	Stopped bool // server exited because Stop was called
	Closed  bool // server exited because the client channel closed
}

// Success reports whether the server exited without error.
func (s ServerStatus) Success() bool { return s.Err == nil }

// WaitStatus blocks until the server terminates, and returns the resulting
// status. After WaitStatus returns, whether or not there was an error, it is
// safe to call s.Start again to restart the server with a fresh channel.
func (s *Server) WaitStatus() ServerStatus {
	s.wg.Wait()
	// Postcondition check.
	if !s.inq.isEmpty() {
		panic("s.inq is not empty at shutdown")
	}
	stat := ServerStatus{Err: s.err}
	if s.err == io.EOF || channel.IsErrClosing(s.err) {
		stat.Err = nil
		stat.Closed = true
	} else if s.err == errServerStopped {
		stat.Err = nil
		stat.Stopped = true
	}
	return stat
}

// Wait blocks until the server terminates and returns the resulting error.
// It is equivalent to s.WaitStatus().Err.
func (s *Server) Wait() error { return s.WaitStatus().Err }

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
	//
	// TODO(@creachadair): We need better tests for this behaviour.
	var keep jmessages
	s.inq.each(func(cur jmessages) {
		for _, req := range cur {
			if req.isNotification() {
				keep = append(keep, req)
				s.log("Retaining notification %p", req)
			} else {
				s.cancel(string(req.ID))
			}
		}
	})
	s.inq.reset()
	for _, elt := range keep {
		s.inq.push(jmessages{elt})
	}
	close(s.work)

	// Cancel any in-flight requests that made it out of the queue, and
	// terminate any pending callback invocations.
	for _, rsp := range s.call {
		rsp.cancel() // the waiter will clean up the map
	}
	for id, cancel := range s.used {
		cancel()
		delete(s.used, id)
	}

	// Postcondition check.
	if len(s.used) != 0 {
		panic("s.used is not empty at shutdown")
	}

	s.err = err
	s.ch = nil
	serversActiveGauge.Add(-1)
}

// read is the main receiver loop, decoding requests from the client and adding
// them to the queue. Decoding errors and message-format problems are handled
// and reported back to the client directly, so that any message that survives
// into the request queue is structurally valid.
func (s *Server) read(ch receiver) {
	for {
		// If the message is not sensible, report an error; otherwise enqueue it
		// for processing. Errors in individual requests are handled later.
		var in jmessages
		var derr error
		bits, err := ch.Recv()
		bytesReadCount.Add(int64(len(bits)))
		if err == nil || (err == io.EOF && len(bits) != 0) {
			err = nil
			derr = in.parseJSON(bits)
			rpcRequestsCount.Add(int64(len(in)))
		}
		s.mu.Lock()
		if err != nil { // receive failure; shut down
			s.stop(err)
			s.mu.Unlock()
			return
		} else if derr != nil { // parse failure; report and continue
			s.pushError(derr)
		} else if len(in) == 0 {
			s.pushError(errEmptyBatch)
		} else {
			// Filter out response messages. It's possible that the entire batch
			// was responses, so re-check the length after doing this.
			keep := s.filterBatch(in)
			if len(keep) != 0 {
				s.log("Received request batch of size %d (qlen=%d)", len(keep), s.inq.size())
				s.inq.push(keep)
				if s.inq.size() == 1 { // the queue was empty
					s.signal()
				}
			}
		}
		s.mu.Unlock()
	}
}

// filterBatch removes and handles any response messages from next, dispatching
// replies to pending callbacks as required. The remainder is returned.
// The caller must hold s.mu, and must re-check that the result is not empty.
func (s *Server) filterBatch(next jmessages) jmessages {
	keep := make(jmessages, 0, len(next))
	for _, req := range next {
		if req.isRequestOrNotification() {
			keep = append(keep, req)
			continue
		}

		// If this is a response implicating the ID of a pending push-call,
		// deliver the result to that call. Do this early to avoid deadlocking on
		// the sequencing barrier (see #78).
		//
		// Note, however, if it does NOT correspond to a known push-call, keep it
		// in the batch so it can be serviced as an error.
		id := string(fixID(req.ID))
		if s.call[id] != nil {
			rsp := s.call[id]
			delete(s.call, id)
			rsp.ch <- req
			s.log("Received response for callback %q", id)
		} else {
			keep = append(keep, req)
		}
	}
	return keep
}

// ServerInfo is the concrete type of responses from the rpc.serverInfo method.
type ServerInfo struct {
	// The list of method names exported by this server.
	Methods []string `json:"methods,omitempty"`

	// Metrics defined by the server and handler methods.
	Metrics map[string]any `json:"metrics,omitempty"`

	// When the server started.
	StartTime time.Time `json:"startTime,omitempty"`
}

// assign returns a Handler to handle the specified name, or nil.
// The caller must hold s.mu.
func (s *Server) assign(ctx context.Context, name string) Handler {
	if s.builtin && strings.HasPrefix(name, "rpc.") {
		switch name {
		case rpcServerInfo:
			return methodFunc(s.handleRPCServerInfo)
		default:
			return nil // reserved
		}
	}
	return s.mux.Assign(ctx, name)
}

// pushError reports an error for the given request ID directly back to the
// client, bypassing the normal request handling mechanism.  The caller must
// hold s.mu when calling this method.
func (s *Server) pushError(err error) {
	s.log("Invalid request: %v", err)
	var jerr *Error
	if e, ok := err.(*Error); ok {
		jerr = e
	} else {
		jerr = &Error{Code: code.FromError(err), Message: err.Error()}
	}

	nw, err := encode(s.ch, jmessages{{
		ID: json.RawMessage("null"),
		E:  jerr,
	}})
	rpcErrorsCount.Add(1)
	bytesWrittenCount.Add(int64(nw))
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

// A task represents a pending method invocation received by the server.
type task struct {
	m Handler // the assigned handler (after assignment)

	ctx   context.Context // the context passed to the handler
	hreq  *Request        // the request passed to the handler
	batch bool            // whether the request was part of a batch

	val json.RawMessage // the result value (when complete)
	err error           // the error value (when complete)
}

type tasks []*task

func (ts tasks) responses(rpcLog RPCLogger) jmessages {
	var rsps jmessages
	for _, task := range ts {
		if task.hreq.id == nil {
			// Spec: "The Server MUST NOT reply to a Notification, including
			// those that are within a batch request.  Notifications are not
			// confirmable by definition, since they do not have a Response
			// object to be returned. As such, the Client would not be aware of
			// any errors."
			//
			// However, parse and validation errors must still be reported, with
			// an ID of null if the request ID was not resolvable.
			if c := code.FromError(task.err); c != code.ParseError && c != code.InvalidRequest {
				continue
			}
		}
		rsp := &jmessage{ID: task.hreq.id, batch: task.batch}
		if rsp.ID == nil {
			rsp.ID = json.RawMessage("null")
		}
		if task.m == nil {
			// No method was ever assigned for this task, so it was never run.
			rsp.err = errTaskNotExecuted
		}
		if task.err == nil {
			rsp.R = task.val
		} else if e, ok := task.err.(*Error); ok {
			rsp.E = e
		} else if c := code.FromError(task.err); c != code.NoError {
			rsp.E = &Error{Code: c, Message: task.err.Error()}
		} else {
			rsp.E = &Error{Code: code.InternalError, Message: task.err.Error()}
		}
		rpcLog.LogResponse(task.ctx, &Response{
			id:     string(rsp.ID),
			err:    rsp.E,
			result: rsp.R,
		})
		rsps = append(rsps, rsp)
	}
	return rsps
}

// numToDo reports the number of tasks in ts that need to be executed, and the
// number of those that are notifications.
func (ts tasks) numToDo() (todo, notes int) {
	for _, t := range ts {
		if t.err == nil {
			todo++
			if t.hreq.IsNotification() {
				notes++
			}
		}
	}
	return
}
