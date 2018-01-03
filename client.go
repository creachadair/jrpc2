package jrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// A Client is a JSON-RPC 2.0 client. The client sends requests and receives
// responses on a Conn provided by the caller.
type Client struct {
	wg sync.WaitGroup // ready when the reader is done at shutdown time

	log    func(string, ...interface{}) // write debug logs here
	allow1 bool                         // tolerate v1 replies with no version marker

	mu      sync.Mutex          // protects the fields below
	closer  io.Closer           // close to shut down the connection
	enc     *json.Encoder       // encode requests to the server
	err     error               // error from a previous operation
	pending map[string]*Pending // requests pending completion, by ID
	nextID  int64               // next unused request ID
}

// NewClient returns a new client that communicates with the server via conn.
func NewClient(conn Conn, opts *ClientOptions) *Client {
	c := &Client{
		log:    opts.logger(),
		allow1: opts.allowV1(),

		// Lock-protected fields
		closer:  conn,
		enc:     json.NewEncoder(conn),
		pending: make(map[string]*Pending),
	}

	// The main client loop reads responses from the server and delivers them
	// back to pending requests by their ID. Outbound requests do not queue;
	// they are sent synchronously in the Send method.

	dec := json.NewDecoder(conn)
	dec.UseNumber()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			// Accept the next batch of responses from the server.  This may
			// either be a list or a single object, the decoder for jresponses
			// knows how to handle both.
			var in jresponses
			err := dec.Decode(&in)
			c.mu.Lock()
			if isRecoverableJSONError(err) {
				c.log("Recoverable decoding error: %v", err)
			} else if err != nil {
				c.log("Unrecoverable decoding error: %v", err)
				c.stop(err)
				c.mu.Unlock()
				return
			}

			// For each response, find the request pending on its ID and
			// deliver it.  Unknown response IDs are logged and discarded.  As
			// we are under the lock, we do not wait for the pending receiver
			// to pick up the response; we just drop it in their channel.  The
			// channel is buffered so we don't need to rendezvous.
			c.log("Received %d responses", len(in))
			for _, rsp := range in {
				if id := string(fixID(rsp.ID)); id == "" {
					c.log("Discarding response without ID: %v", rsp)
				} else if p := c.pending[id]; p == nil {
					c.log("Discarding response for unknown ID %q", id)
				} else if !c.versionOK(rsp.V) {
					delete(c.pending, id)
					p.complete(&jresponse{
						ID: rsp.ID,
						E:  jerrorf(E_InvalidRequest, "incorrect version marker %q", rsp.V),
					})
					c.log("Invalid response for ID %q", id)
				} else {
					// Remove the pending request from the set and deliver its response.
					// Determining whether it's an error is the caller's responsibility.
					delete(c.pending, id)
					p.complete(rsp)
					c.log("Completed request for ID %q", id)
				}
			}
			c.mu.Unlock()
		}
	}()
	return c
}

// req constructs a fresh request for the specified method and parameters.
// This does not transmit the request to the server; use c.send to do so.
func (c *Client) req(method string, params interface{}) (*Request, error) {
	bits, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	c.log("Request params: %+v", params)

	c.mu.Lock()
	defer c.mu.Unlock()
	id := json.RawMessage(strconv.FormatInt(c.nextID, 10))
	c.nextID++
	return &Request{
		id:     id,
		method: method,
		params: bits,
	}, nil
}

// send transmits the specified requests to the server and returns a slice of
// Pending stubs that can be used to wait for their responses.
//
// The resulting slice will contain one entry for each input request that
// expects a response (that is, all those that are not notifications). If all
// the requests are notifications, the slice will be empty.
//
// This method blocks until the entire batch of requests has been transmitted.
func (c *Client) send(reqs ...*Request) ([]*Pending, error) {
	if len(reqs) == 0 {
		return nil, errors.New("empty request batch")
	}

	batch := make(jrequests, len(reqs))
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, req := range reqs {
		if id := req.ID(); id != "" && c.pending[id] != nil {
			return nil, fmt.Errorf("duplicate request ID %q", id)
		}
		batch[i] = &jrequest{
			V:  Version,
			ID: req.id,
			M:  req.method,
			P:  jparams(req.params),
		}
	}

	b, err := json.Marshal(batch)
	if err != nil {
		c.log("Marshal failed: %v", err)
	} else {
		c.log("Outgoing batch: %s", string(b))
	}
	if err := c.enc.Encode(json.RawMessage(b)); err != nil {
		return nil, err
	}

	// Now that we have sent them, record pending requests for each that is not
	// a notification. We do this after transmission so that an error does not
	// leave us with dead pending requests awaiting responses.
	var pends []*Pending
	for _, req := range reqs {
		if id := req.ID(); id != "" {
			p := newPending(id)
			c.pending[id] = p
			pends = append(pends, p)
		}
	}
	return pends, nil
}

// Call initiates a single request.  It blocks until the request is sent.
func (c *Client) Call(method string, params interface{}) (*Pending, error) {
	req, err := c.req(method, params)
	if err != nil {
		return nil, err
	}
	ps, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return ps[0], nil
}

// CallWait initiates a single request and blocks until the response returns.
// It is shorthand for Call + Wait. Any error returned is from the initial
// Call; errors from the pending Wait must be checked by the caller:
//
//    rsp, err := c.CallWait(method, params)
//    if err != nil {
//       log.Fatalf("Call failed: %v", err)
//    } else if err := rsp.Error(); err != nil {
//       log.Printf("Error from server: %v", err)
//    }
//
func (c *Client) CallWait(method string, params interface{}) (*Response, error) {
	p, err := c.Call(method, params)
	if err != nil {
		return nil, err
	}
	return p.Wait(), nil
}

// Batch initiates a batch of concurrent requests.  It blocks until the entire
// batch is sent.
func (c *Client) Batch(specs []Spec) (Batch, error) {
	reqs := make([]*Request, len(specs))
	for i, spec := range specs {
		req, err := c.req(spec.Method, spec.Params)
		if err != nil {
			return nil, err
		}
		reqs[i] = req
	}
	return c.send(reqs...)
}

// A Spec combines a method name and parameter value.
type Spec struct {
	Method string
	Params interface{}
}

// A Batch is a group of pending requests awaiting responses.
type Batch []*Pending

// Wait blocks until all the requests in b have completed, and returns the
// corresponding responses. The caller is responsible for checking for errors
// in each of the responses.
func (b Batch) Wait() []*Response {
	rsps := make([]*Response, len(b))
	for i, p := range b {
		rsps[i] = p.Wait()
	}
	return rsps
}

// Notify transmits a notification to the specified method and parameters.  It
// blocks until the notification has been sent.
func (c *Client) Notify(method string, params interface{}) error {
	bits, err := marshalParams(params)
	if err != nil {
		return err
	}
	c.log("Notification params: %+v", params)
	_, err = c.send(&Request{
		method: method,
		params: bits,
	})
	return err
}

// Close shuts down the client, abandoning any pending in-flight requests.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stop(errClientStopped)
	return c.err
}

// stop closes down the reader for c and records err as its final state.  The
// caller must hold c.mu. If multiple callers invoke stop, only the first will
// successfully record its error status.
func (c *Client) stop(err error) {
	if c.closer == nil {
		return // nothing is running
	}
	c.closer.Close()
	for id, p := range c.pending {
		delete(c.pending, id)
		p.abandon()
	}
	c.err = err
	c.closer = nil
}

func (c *Client) versionOK(v string) bool {
	if v == "" {
		return c.allow1
	}
	return v == Version
}

// A Pending tracks a single pending request whose response is awaited.
// Calling Wait blocks until the response is received. It is safe to call Wait
// multiple times from concurrent goroutines.
type Pending struct {
	// When the response is delivered, it is sent to ch, which is the point of
	// synchronization between the client and the caller. Once a value is
	// received, the pending call is complete.
	ch  chan *jresponse
	id  string // the ID from the request
	rsp *Response
}

func newPending(id string) *Pending {
	// Buffer the channel so the response reader does not need to rendezvous
	// with the recipient.
	return &Pending{ch: make(chan *jresponse, 1), id: id}
}

// complete delivers a response to p, completing the request.
func (p *Pending) complete(rsp *jresponse) { p.ch <- rsp }

// abandon delivers a failure to p, indicating it will never complete.
func (p *Pending) abandon() {
	p.ch <- &jresponse{
		ID: json.RawMessage(p.id),
		E:  jerrorf(E_InternalError, "request incomplete"),
	}
}

// ID reports the request identifier of the request p is waiting for.  It is
// safe to call ID even if the request has not yet completed.
func (p *Pending) ID() string { return p.id }

// Wait blocks until p is complete, then returns the response.  The caller must
// check the response for an error from the server.
func (p *Pending) Wait() *Response {
	raw, ok := <-p.ch
	if ok {
		// N.B. We intentionally did not have the sender close the channel, to
		// avoid a race between callers of Wait. The channel is closed by the
		// first waiter to get a real value, after ensuring the response values
		// are updated -- that way subsequent waiters will get a zero from the
		// closed channel and correctly fall through to the stored responses.

		p.rsp = &Response{
			id:     fixID(raw.ID),
			err:    raw.E.toError(),
			result: raw.R,
		}
		close(p.ch)
	}
	return p.rsp
}

// marshalParams validates and marshals params to JSON for a request.  It's
// okay for the parameters to be empty, but if they are not they must be valid
// JSON. We check for the required structural properties also.
func marshalParams(params interface{}) (json.RawMessage, error) {
	if params == nil {
		return nil, nil // no parameters, that is OK
	}
	bits, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if len(bits) != 0 && bits[0] != '[' && bits[0] != '{' {
		// JSON-RPC requires that if parameters are provided at all, they are
		// an array or an object
		return nil, Errorf(E_InvalidRequest, "invalid parameters: array or object required")
	}
	return bits, err
}
