package jrpc2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/code"
)

// An Assigner assigns a Handler to handle the specified method name, or nil if
// no method is available to handle the request.
type Assigner interface {
	// Assign returns the handler for the named method, or nil.
	Assign(ctx context.Context, method string) Handler

	// Names returns a slice of all known method names for the assigner.  The
	// resulting slice is ordered lexicographically and contains no duplicates.
	Names() []string
}

// A Handler handles a single request.
type Handler interface {
	// Handle invokes the method with the specified request. The response value
	// must be JSON-marshalable or nil. In case of error, the handler can
	// return a value of type *jrpc2.Error to control the response code sent
	// back to the caller; otherwise the server will wrap the resulting value.
	//
	// The context passed to the handler by a *jrpc2.Server includes two extra
	// values that the handler may extract.
	//
	// To obtain a server metrics value, write:
	//
	//    sm := jrpc2.ServerMetrics(ctx)
	//
	// To obtain the inbound request message, write:
	//
	//    req := jrpc2.InboundRequest(ctx)
	//
	// The inbound request is the same value passed to the Handle method -- the
	// latter is primarily useful in handlers generated by handler.New, which do
	// not receive this value directly.
	Handle(context.Context, *Request) (interface{}, error)
}

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
// parameters, it returns nil without modifying v. If r is invalid it returns
// an InvalidParams error.
//
// By default, unknown keys are disallowed when unmarshaling into a v of struct
// type. This can be overridden by implementing an UnknownFields method that
// returns true, on the concrete type of v.
//
// If v has type *json.RawMessage, decoding cannot fail.
func (r *Request) UnmarshalParams(v interface{}) error {
	if len(r.params) == 0 {
		return nil
	} else if raw, ok := v.(*json.RawMessage); ok {
		*raw = json.RawMessage(string(r.params)) // copy
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(r.params))
	if uf, ok := v.(UnknownFielder); !ok || !uf.UnknownFields() {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return Errorf(code.InvalidParams, "invalid parameters: %v", err.Error())
	}
	return nil
}

// ParamString returns the encoded request parameters of r as a string.
// If r has no parameters, it returns "".
func (r *Request) ParamString() string { return string(r.params) }

// ErrInvalidVersion is returned by ParseRequests if one or more of the
// requests in the input has a missing or invalid version marker.
var ErrInvalidVersion = Errorf(code.InvalidRequest, "incorrect version marker")

// ParseRequests parses a single request or a batch of requests from JSON.
// The result parameters are either nil or have concrete type json.RawMessage.
//
// If any of the requests is missing or has an invalid JSON-RPC version, it
// returns ErrInvalidVersion along with the parsed results. Otherwise, no
// validation apart from basic structure is performed on the results.
func ParseRequests(msg []byte) ([]*Request, error) {
	var req jmessages
	if err := req.parseJSON(msg); err != nil {
		return nil, err
	}
	var err error
	out := make([]*Request, len(req))
	for i, req := range req {
		if req.V != Version {
			err = ErrInvalidVersion
		}
		out[i] = &Request{
			id:     fixID(req.ID),
			method: req.M,
			params: req.P,
		}
	}
	return out, err
}

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

// SetID sets the request identifier for r. This is for use in proxies.
func (r *Response) SetID(id string) { r.id = id }

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

// ResultString returns the encoded result message of r as a string.
// If r has no result, for example if r is an error response, it returns "".
func (r *Response) ResultString() string { return string(r.result) }

// MarshalJSON converts the response to equivalent JSON.
func (r *Response) MarshalJSON() ([]byte, error) {
	return json.Marshal(&jmessage{
		V:  Version,
		ID: json.RawMessage(r.id),
		R:  r.result,
		E:  r.err,
	})
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

// jmessages is either a single protocol message or an array of protocol
// messages.  This handles the decoding of batch requests in JSON-RPC 2.0.
type jmessages []*jmessage

func (j jmessages) toJSON() ([]byte, error) {
	if len(j) == 1 && !j[0].batch {
		return json.Marshal(j[0])
	}
	return json.Marshal([]*jmessage(j))
}

// N.B. Not UnmarshalJSON, because json.Unmarshal checks for validity early and
// here we want to control the error that is returned.
func (j *jmessages) parseJSON(data []byte) error {
	*j = (*j)[:0] // reset state

	// When parsing requests, validation checks are deferred: The only immediate
	// mode of failure for unmarshaling is if the request is not a valid object
	// or array.
	var msgs []json.RawMessage
	var batch bool
	if len(data) == 0 || data[0] != '[' {
		msgs = append(msgs, nil)
		if err := json.Unmarshal(data, &msgs[0]); err != nil {
			return Errorf(code.ParseError, "invalid request message")
		}
	} else if err := json.Unmarshal(data, &msgs); err != nil {
		return Errorf(code.ParseError, "invalid request batch")
	} else {
		batch = true
	}

	// Now parse the individual request messages, but do not fail on errors.  We
	// know that the messages are intact, but validity is checked at usage.
	for _, raw := range msgs {
		req := new(jmessage)
		req.parseJSON(raw)
		req.batch = batch
		*j = append(*j, req)
	}
	return nil
}

// jmessage is the transmission format of a protocol message.
type jmessage struct {
	V  string          `json:"jsonrpc"`      // must be Version
	ID json.RawMessage `json:"id,omitempty"` // may be nil

	// Fields belonging to request or notification objects
	M string          `json:"method,omitempty"`
	P json.RawMessage `json:"params,omitempty"` // may be nil

	// Fields belonging to response or error objects
	E *Error          `json:"error,omitempty"`  // set on error
	R json.RawMessage `json:"result,omitempty"` // set on success

	// N.B.: In a valid protocol message, M and P are mutually exclusive with E
	// and R. Specifically, if M != "" then E and R must both be unset. This is
	// checked during parsing.

	batch bool  // this message was part of a batch
	err   error // if not nil, this message is invalid and err is why
}

func (j *jmessage) fail(code code.Code, msg string) error {
	j.err = Errorf(code, msg)
	return j.err
}

func (j *jmessage) parseJSON(data []byte) error {
	// Unmarshal into a map so we can check for extra keys.  The json.Decoder
	// has DisallowUnknownFields, but fails decoding eagerly for fields that do
	// not map to known tags. We want to fully parse the object so we can
	// propagate the "id" in error responses, if it is set. So we have to decode
	// and check the fields ourselves.

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return j.fail(code.ParseError, "request is not a JSON object")
	}

	*j = jmessage{}    // reset content
	var extra []string // extra field names
	for key, val := range obj {
		switch key {
		case "jsonrpc":
			if json.Unmarshal(val, &j.V) != nil {
				j.fail(code.ParseError, "invalid version key")
			}
		case "id":
			j.ID = val
		case "method":
			if json.Unmarshal(val, &j.M) != nil {
				j.fail(code.ParseError, "invalid method name")
			}
		case "params":
			// As a special case, reduce "null" to nil in the parameters.
			// Otherwise, require per spec that val is an array or object.
			if !isNull(val) {
				j.P = val
			}
			if len(j.P) != 0 && j.P[0] != '[' && j.P[0] != '{' {
				j.fail(code.InvalidRequest, "parameters must be array or object")
			}
		case "error":
			if json.Unmarshal(val, &j.E) != nil {
				j.fail(code.ParseError, "invalid error value")
			}
		case "result":
			j.R = val
		default:
			extra = append(extra, key)
		}
	}

	// Report an error if request/response fields overlap.
	if j.M != "" && (j.E != nil || j.R != nil) {
		j.fail(code.InvalidRequest, "mixed request and reply fields")
	}

	// Report an error for extraneous fields.
	if j.err == nil && len(extra) != 0 {
		j.err = DataErrorf(code.InvalidRequest, extra, "extra fields in request")
	}
	return nil
}

// isRequestOrNotification reports whether j is a request or notification.
func (j *jmessage) isRequestOrNotification() bool { return j.E == nil && j.R == nil && j.M != "" }

// isNotification reports whether j is a notification
func (j *jmessage) isNotification() bool { return j.isRequestOrNotification() && fixID(j.ID) == nil }

type jerror struct {
	C int32           `json:"code"`
	M string          `json:"message,omitempty"`
	D json.RawMessage `json:"data,omitempty"`
}

// fixID filters id, treating "null" as a synonym for an unset ID.  This
// supports interoperation with JSON-RPC v1 where "null" is used as an ID for
// notifications.
func fixID(id json.RawMessage) json.RawMessage {
	if !isNull(id) {
		return id
	}
	return nil
}

// encode marshals rsps as JSON and forwards it to the channel.
func encode(ch channel.Sender, rsps jmessages) (int, error) {
	bits, err := rsps.toJSON()
	if err != nil {
		return 0, err
	}
	return len(bits), ch.Send(bits)
}

// Network guesses a network type for the specified address.  The assignment of
// a network type uses the following heuristics:
//
// If s does not have the form [host]:port, the network is assigned as "unix".
// The network "unix" is also assigned if port == "", port contains characters
// other than ASCII letters, digits, and "-", or if host contains a "/".
//
// Otherwise, the network is assigned as "tcp". Note that this function does
// not verify whether the address is lexically valid.
func Network(s string) string {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "unix"
	}
	host, port := s[:i], s[i+1:]
	if port == "" || !isServiceName(port) {
		return "unix"
	} else if strings.IndexByte(host, '/') >= 0 {
		return "unix"
	}
	return "tcp"
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

// isNull reports whether msg is exactly the JSON "null" value.
func isNull(msg json.RawMessage) bool {
	return len(msg) == 4 && msg[0] == 'n' && msg[1] == 'u' && msg[2] == 'l' && msg[3] == 'l'
}

// filterError filters an *Error value to distinguish context errors from other
// error types. If err is not a context error, it is returned unchanged.
func filterError(e *Error) error {
	switch e.code {
	case code.Cancelled:
		return context.Canceled
	case code.DeadlineExceeded:
		return context.DeadlineExceeded
	}
	return e
}

// UnknownFielder is an optional interface that can be implemented by a
// parameter type to allow control over whether unknown fields should be
// allowed when unmarshaling from JSON.
//
// If a type does not implement this interface, unknown fields are disallowed.
type UnknownFielder interface {
	// Report whether unknown fields should be permitted when unmarshaling into
	// the receiver.
	UnknownFields() bool
}

// NonStrict wraps a value v so that it can be unmarshaled as a parameter value
// from JSON without checking for unknown fields.
//
// For example:
//
//       var obj RequestType
//       err := req.UnmarshalParams(jrpc2.NonStrict(&obj))
//
func NonStrict(v interface{}) interface{} { return &nonStrict{v: v} }

type nonStrict struct{ v interface{} }

func (nonStrict) UnknownFields() bool { return true }
