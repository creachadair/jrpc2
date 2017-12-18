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

	log func(string, ...interface{}) // write debug logs here

	mu      sync.Mutex          // protects the fields below
	closer  io.Closer           // close to shut down the connection
	enc     *json.Encoder       // encode requests to the server
	dec     *json.Decoder       // decode responses from the server
	err     error               // error from a previous operation
	pending map[string]*Pending // requests pending completion, by ID
	nextID  int64               // next unused request ID
}

// NewClient returns a new client that communicates with the server via conn.
func NewClient(conn Conn, opts ...ClientOption) *Client {
	c := &Client{
		log:     func(string, ...interface{}) {},
		closer:  conn,
		enc:     json.NewEncoder(conn),
		pending: make(map[string]*Pending),
	}
	for _, opt := range opts {
		opt(c)
	}

	dec := json.NewDecoder(conn)
	dec.UseNumber()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
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
			c.log("Received %d responses", len(in))
			for _, rsp := range in {
				id := string(rsp.ID)
				if id == "" {
					c.log("Discarding response without ID: %v", rsp)
				} else if p := c.pending[id]; p == nil {
					c.log("Discarding response for unknown ID %q", id)
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

// Req constructs a fresh request for the specified method and parameters.
// This does not transmit the request to the server; use c.Send to do so.
func (c *Client) Req(method string, params interface{}) (*Request, error) {
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

// Note constructs a notification request for the specified method and parameters.
// This does not transmit the request to the server; use c.Send to do so.
func (c *Client) Note(method string, params interface{}) (*Request, error) {
	bits, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	c.log("Notification params: %+v", params)
	return &Request{
		method: method,
		params: bits,
	}, nil
}

// Send transmits the specified requests to the server and returns a slice of
// Pending stubs that can be used to wait for their responses.
//
// The resulting slice will only contain entries for requests that expect
// responses -- if all the requests are notifications, the slice will be empty.
//
// Send blocks until the entire batch of requests has been transmitted.
func (c *Client) Send(reqs ...*Request) ([]*Pending, error) {
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
	if err := c.enc.Encode(batch); err != nil {
		return nil, err
	}

	// Now that we have sent them, record pending requests for each that is not
	// a notification. We do this after transmission so that an error does not
	// leave us with dead pending requests awaiting responses.
	var pends []*Pending
	for _, req := range reqs {
		if id := req.ID(); id != "" {
			p := newPending()
			c.pending[id] = p
			pends = append(pends, p)
		}
	}
	return pends, nil
}

// Call is shorthand for Req + Send + Wait for a single request.  It
// blocks until the request is complete.
func (c *Client) Call(method string, params interface{}) (*Response, error) {
	req, err := c.Req(method, params)
	if err != nil {
		return nil, err
	}
	ps, err := c.Send(req)
	if err != nil {
		return nil, err
	}
	return ps[0].Wait()
}

// Notify is shorthand for Note + Send for a single request. It blocks until
// the notification has been sent.
func (c *Client) Notify(method string, params interface{}) error {
	req, err := c.Note(method, params)
	if err != nil {
		return err
	}
	_, err = c.Send(req)
	return err
}

// Close shuts down the client, abandoning any pending in-flight requests.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stop(ErrClientStopped)
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

// A Pending tracks a single pending request whose response is awaited.
// Calling Wait blocks until the response is received. It is safe to call Wait
// multiple times from concurrent goroutines.
type Pending struct {
	// When the response is delivered, it is sent to ch, which is the point of
	// synchronization between the client and the caller. Once a value is
	// received, the pending call is complete.
	ch  chan *jresponse
	rsp *Response
	err error
}

func newPending() *Pending {
	return &Pending{ch: make(chan *jresponse, 1), err: errIncomplete}
}

var errIncomplete = errors.New("request incomplete")

// complete delivers a response to p, completing the request.
func (p *Pending) complete(rsp *jresponse) { p.ch <- rsp }

// abandon closes p's channel signaling that it will never complete.
func (p *Pending) abandon() { close(p.ch) }

// Wait blocks until p is complete, then returns the response and any error
// that occurred.  A non-nil response is returned whether or not there is an
// error.
func (p *Pending) Wait() (*Response, error) {
	raw, ok := <-p.ch
	if ok {
		p.err = nil // clear incomplete status
		p.rsp = &Response{
			id:     raw.ID,
			err:    raw.E.toError(),
			result: raw.R,
		}
		if err := raw.E.toError(); err != nil {
			p.err = err
		}
		close(p.ch)
	}
	return p.rsp, p.err
}
