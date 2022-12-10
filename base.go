// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/creachadair/jrpc2/code"
)

// An Assigner assigns a Handler to handle the specified method name, or nil if
// no method is available to handle the request.
type Assigner interface {
	// Assign returns the handler for the named method, or nil.
	// The implementation can obtain the complete request from ctx using the
	// jrpc2.InboundRequest function.
	Assign(ctx context.Context, method string) Handler
}

// Namer is an optional interface that an Assigner may implement to expose the
// names of its methods to the ServerInfo method.
type Namer interface {
	// Names returns all known method names in lexicographic order.
	Names() []string
}

// A Handler implements method given a request. The response value must be
// JSON-marshalable or nil. In case of error, the handler can return a value of
// type *jrpc2.Error to control the response code sent back to the caller;
// otherwise the server will wrap the resulting value.
//
// The context passed to the handler by a *jrpc2.Server includes two special
// values that the handler may extract.
//
// To obtain the server instance running the handler, write:
//
//	srv := jrpc2.ServerFromContext(ctx)
//
// To obtain the inbound request message, write:
//
//	req := jrpc2.InboundRequest(ctx)
//
// The latter is primarily useful for handlers generated by handler.New,
// which do not receive the request directly. For a handler that implements
// the Handle method directly, req is the same value passed as a parameter
// to Handle.
type Handler = func(context.Context, *Request) (any, error)

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

// UnmarshalParams decodes the request parameters of r into v. If r has empty
// parameters, it returns nil without modifying v. If the parameters are
// invalid, UnmarshalParams returns an InvalidParams error.
//
// By default, unknown object keys are ignored and discarded when unmarshaling
// into a v of struct type. If the type of v implements a DisallowUnknownFields
// method, unknown fields will instead generate an InvalidParams error.  The
// jrpc2.StrictFields helper adapts existing struct values to this interface.
// For more specific behaviour, implement a custom json.Unmarshaler.
//
// If v has type *json.RawMessage, unmarshaling will never report an error.
func (r *Request) UnmarshalParams(v any) error {
	if len(r.params) == 0 {
		return nil
	}
	switch t := v.(type) {
	case *json.RawMessage:
		*t = json.RawMessage(string(r.params)) // copy
		return nil
	case strictFielder:
		dec := json.NewDecoder(bytes.NewReader(r.params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return errInvalidParams.WithData(err.Error())
		}
		return nil
	}
	if err := json.Unmarshal(r.params, v); err != nil {
		return errInvalidParams.WithData(err.Error())
	}
	return nil
}

// ParamString returns the encoded request parameters of r as a string.
// If r has no parameters, it returns "".
func (r *Request) ParamString() string { return string(r.params) }

// A Response is a response message from a server to a client.
type Response struct {
	id     string
	err    *Error
	result json.RawMessage

	// Waiters synchronize on reading from ch. The first successful reader from
	// ch completes the request and is responsible for updating rsp and then
	// closing ch. The client owns writing to ch, and is responsible to ensure
	// that at most one write is ever performed.
	ch     chan *jmessage
	cancel func()
}

// ID returns the request identifier for r.
func (r *Response) ID() string { return r.id }

// SetID sets the ID of r to s, for use in proxies.
func (r *Response) SetID(s string) { r.id = s }

// Error returns a non-nil *Error if the response contains an error.
func (r *Response) Error() *Error { return r.err }

// UnmarshalResult decodes the result message into v. If the request failed,
// UnmarshalResult returns the same *Error value that is returned by r.Error(),
// and v is unmodified.
//
// By default, unknown object keys are ignored and discarded when unmarshaling
// into a v of struct type. If the type of v implements a DisallowUnknownFields
// method, unknown fields will instead generate an error.  The
// jrpc2.StrictFields helper adapts existing struct values to this interface.
// For more specific behaviour, implement a custom json.Unmarshaler.
//
// If v has type *json.RawMessage, unmarshaling will never report an error.
func (r *Response) UnmarshalResult(v any) error {
	if r.err != nil {
		return r.err
	}
	switch t := v.(type) {
	case *json.RawMessage:
		*t = json.RawMessage(string(r.result)) // copy
		return nil
	case strictFielder:
		dec := json.NewDecoder(bytes.NewReader(r.result))
		dec.DisallowUnknownFields()
		return dec.Decode(v)
	}
	return json.Unmarshal(r.result, v)
}

// ResultString returns the encoded result message of r as a string.
// If r has no result, for example if r is an error response, it returns "".
func (r *Response) ResultString() string { return string(r.result) }

// MarshalJSON converts the response to equivalent JSON.
func (r *Response) MarshalJSON() ([]byte, error) {
	return (&jmessage{
		ID: json.RawMessage(r.id),
		R:  r.result,
		E:  r.err,
	}).toJSON()
}

// wait blocks until r is complete. It is safe to call this multiple times and
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
		r.err = raw.E
		r.result = raw.R
		close(r.ch)
		r.cancel() // release the context observer

		// Safety check: The response IDs should match. Do this after delivery so
		// a failure does not orphan resources.
		if id := string(fixID(raw.ID)); id != r.id {
			panic(fmt.Sprintf("Mismatched response ID %q expecting %q", id, r.id))
		}
	}
}

// Network guesses a network type for the specified address and returns a tuple
// of that type and the address.
//
// The assignment of a network type uses the following heuristics:
//
// If s does not have the form [host]:port, the network is assigned as "unix".
// The network "unix" is also assigned if port == "", port contains characters
// other than ASCII letters, digits, and "-", or if host contains a "/".
//
// Otherwise, the network is assigned as "tcp". Note that this function does
// not verify whether the address is lexically valid.
func Network(s string) (network, address string) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "unix", s
	}
	host, port := s[:i], s[i+1:]
	if port == "" || !isServiceName(port) {
		return "unix", s
	} else if strings.IndexByte(host, '/') >= 0 {
		return "unix", s
	}
	return "tcp", s
}

// isServiceName reports whether s looks like a legal service name from the
// services(5) file. The grammar of such names is not well-defined, but for our
// purposes it includes letters, digits, and "-".
func isServiceName(s string) bool {
	for i := range s {
		b := s[i]
		if b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b == '-' {
			continue
		}
		return false
	}
	return true
}

// filterError filters an *Error value to distinguish context errors from other
// error types. If err is not a context error, it is returned unchanged.
func filterError(e *Error) error {
	switch e.Code {
	case code.Cancelled:
		return context.Canceled
	case code.DeadlineExceeded:
		return context.DeadlineExceeded
	}
	return e
}
