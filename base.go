package jrpc2

import (
	"encoding/json"
	"errors"
	"fmt"

	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/code"
)

// A Request is a request message from a client to a server.
type Request struct {
	id     json.RawMessage // the request ID, nil for notifications
	method string          // the name of the method being requested
	params json.RawMessage // method parameters
}

// IsNotification reports whether the request is a notification, and thus does
// not require a value response.
func (r *Request) IsNotification() bool { return r.id == nil }

// ID returns the request identifier for r, or "" if r is a notification.
func (r *Request) ID() string { return string(r.id) }

// Method reports the method name for the request.
func (r *Request) Method() string { return r.method }

// HasParams reports whether the request has non-empty parameters.
func (r *Request) HasParams() bool { return len(r.params) != 0 }

// UnmarshalParams decodes the parameters into v.
func (r *Request) UnmarshalParams(v interface{}) error { return json.Unmarshal(r.params, v) }

// A Response is a response message from a server to a client.
type Response struct {
	id     string
	err    *Error
	result json.RawMessage

	// Waiters synchronize on reading from ch. The first successful reader from
	// ch completes the request and is responsible for updating rsp and then
	// closing ch. The client owns writing to ch, and is responsible to ensure
	// that at most one write is ever performed.
	ch     chan *jresponse
	cancel func()
}

// ID returns the request identifier for r.
func (r *Response) ID() string { return r.id }

// Error returns a non-nil *Error if the response contains an error.
func (r *Response) Error() *Error { return r.err }

// UnmarshalResult decodes the result message into v. If the request failed,
// UnmarshalResult returns the *Error value that would also be returned by
// r.Error(), and v is unmodified.
func (r *Response) UnmarshalResult(v interface{}) error {
	if r.err != nil {
		return r.err
	}
	return json.Unmarshal(r.result, v)
}

// MarshalJSON converts the request to equivalent JSON.
func (r *Response) MarshalJSON() ([]byte, error) {
	jr := &jresponse{
		V:  Version,
		ID: json.RawMessage(r.id),
		R:  r.result,
	}
	if r.err != nil {
		jr.E = r.err.tojerror()
	}
	return json.Marshal(jr)
}

// wait blocks until p is complete. It is safe to call this multiple times and
// from concurrent goroutines.
func (r *Response) wait() {
	raw, ok := <-r.ch
	if ok {
		// N.B. We intentionally DO NOT have the sender close the channel, to
		// prevent a data race between callers of Wait. The channel is closed
		// by the first waiter to get a real value (ok == true).
		//
		// The first waiter must update the response value, THEN close the
		// channel and cancel the context. This order ensures that subsequent
		// waiters all get the same response, and do not race on accessing it.
		r.id = string(fixID(raw.ID))
		r.err = raw.E.toError()
		r.result = raw.R
		close(r.ch)
		r.cancel() // release the context observer
	}
}

// jrequests is either a single request or a slice of requests.  This handles
// the decoding of batch requests in JSON-RPC 2.0.
type jrequests []*jrequest

func (j jrequests) MarshalJSON() ([]byte, error) {
	if len(j) == 1 {
		return json.Marshal(j[0])
	}
	return json.Marshal([]*jrequest(j))
}

func (j *jrequests) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty request message")
	} else if data[0] != '[' {
		*j = jrequests{new(jrequest)}
		return json.Unmarshal(data, (*j)[0])
	}
	return json.Unmarshal(data, (*[]*jrequest)(j))
}

// jrequest is the transmission format of a request message.
type jrequest struct {
	V  string          `json:"jsonrpc"`      // must be Version
	ID json.RawMessage `json:"id,omitempty"` // rendered by the constructor, may be nil
	M  string          `json:"method"`
	P  json.RawMessage `json:"params,omitempty"` // rendered by the constructor
}

func (j *jrequest) UnmarshalJSON(data []byte) error {
	type stub jrequest
	if err := json.Unmarshal(data, (*stub)(j)); err != nil {
		return err
	} else if len(j.P) != 0 && j.P[0] != '[' && j.P[0] != '{' {
		return DataErrorf(code.InvalidRequest, j.ID, "parameters must be list or object")
	}
	return nil
}

// jresponses is a slice of responses, encoded as a single response if there is
// exactly one.
type jresponses []*jresponse

func (j jresponses) MarshalJSON() ([]byte, error) {
	if len(j) == 1 {
		return json.Marshal(j[0])
	}
	return json.Marshal([]*jresponse(j))
}

func (j *jresponses) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return errors.New("empty request message")
	} else if data[0] != '[' {
		*j = jresponses{new(jresponse)}
		return json.Unmarshal(data, (*j)[0])
	}
	return json.Unmarshal(data, (*[]*jresponse)(j))
}

// jresponse is the transmission format of a response message.
type jresponse struct {
	V  string          `json:"jsonrpc"`          // must be Version
	ID json.RawMessage `json:"id,omitempty"`     // set if request had an ID
	E  *jerror         `json:"error,omitempty"`  // set on error
	R  json.RawMessage `json:"result,omitempty"` // set on success

	// Allow the server to send a response that looks like a notification.
	// This is an extension of JSON-RPC 2.0.
	M string          `json:"method,omitempty"`
	P json.RawMessage `json:"params,omitempty"`
}

func (j jresponse) isServerRequest() bool { return j.E == nil && j.R == nil && j.M != "" }

// jerror is the transmission format of an error object.
type jerror struct {
	Code int32           `json:"code"`
	Msg  string          `json:"message,omitempty"` // optional
	Data json.RawMessage `json:"data,omitempty"`    // optional
}

// toError converts a wire-format error object into an *Error.
func (e *jerror) toError() *Error {
	if e == nil {
		return nil
	}
	return &Error{
		message: e.Msg,
		code:    code.Code(e.Code),
		data:    e.Data,
	}
}

func jerrorf(code code.Code, msg string, args ...interface{}) *jerror {
	return &jerror{
		Code: int32(code),
		Msg:  fmt.Sprintf(msg, args...),
	}
}

// fixID filters id, treating "null" as a synonym for an unset ID.  This
// supports interoperation with JSON-RPC v1 where "null" is used as an ID for
// notifications.
func fixID(id json.RawMessage) json.RawMessage {
	if string(id) != "null" {
		return id
	}
	return nil
}

// encode marshals v as JSON and forwards it to the channel.
func encode(ch channel.Sender, v interface{}) (int, error) {
	bits, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return len(bits), ch.Send(bits)
}

// decode receives a message from the channel and unmarshals it as JSON to v.
func decode(ch channel.Receiver, v interface{}) error {
	bits, err := ch.Recv()
	if err != nil {
		return err
	}
	return json.Unmarshal(bits, v)
}
