// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"bytes"
	"encoding/json"

	"github.com/creachadair/jrpc2/code"
)

// ParseRequests parses a single request or a batch of requests from JSON.
// This function reports an error only if msg is not valid JSON. The caller
// must check the individual results for their validity.
func ParseRequests(msg []byte) ([]*ParsedRequest, error) {
	var reqs jmessages
	if err := reqs.parseJSON(msg); err != nil {
		return nil, err
	}
	var err error
	out := make([]*ParsedRequest, len(reqs))
	for i, req := range reqs {
		out[i] = &ParsedRequest{
			ID:     string(fixID(req.ID)),
			Method: req.M,
			Params: req.P,
			Error:  req.err,
		}
	}
	return out, err
}

// A ParsedRequest is the parsed form of a request message. If a request is
// valid, its Error field is nil. Otherwise, the Error field describes why the
// request is invalid, and the other fields may be incomplete or missing.
type ParsedRequest struct {
	ID     string
	Method string
	Params json.RawMessage
	Error  *Error
}

// ToRequest converts p to an equivalent server Request. If p.Error is not nil,
// ToRequest returns nil.
//
// This method does not check validity. If p is from a successful call of
// ParseRequests, the result will be valid; otherwise the caller must ensure
// that the ID and parameters are valid JSON.
func (p *ParsedRequest) ToRequest() *Request {
	if p == nil || p.Error != nil {
		return nil
	}
	return &Request{
		id:     fixID(json.RawMessage(p.ID)),
		method: p.Method,
		params: p.Params,
	}
}

// jmessages is either a single protocol message or an array of protocol
// messages.  This handles the decoding of batch requests in JSON-RPC 2.0.
type jmessages []*jmessage

func (j jmessages) toJSON() ([]byte, error) {
	if len(j) == 1 && !j[0].batch {
		return j[0].toJSON()
	}
	var sb bytes.Buffer
	sb.WriteByte('[')
	for i, msg := range j {
		if i > 0 {
			sb.WriteByte(',')
		}
		bits, err := msg.toJSON()
		if err != nil {
			return nil, err
		}
		sb.Write(bits)
	}
	sb.WriteByte(']')
	return sb.Bytes(), nil
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
	if firstByte(data) != '[' {
		msgs = append(msgs, nil)
		if err := json.Unmarshal(data, &msgs[0]); err != nil {
			return errInvalidRequest
		}
	} else if err := json.Unmarshal(data, &msgs); err != nil {
		return errInvalidRequest
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
	V  string          // must be Version
	ID json.RawMessage // may be nil

	// Fields belonging to request or notification objects
	M string
	P json.RawMessage // may be nil

	// Fields belonging to response or error objects
	E *Error          // set on error
	R json.RawMessage // set on success

	// N.B.: In a valid protocol message, M and P are mutually exclusive with E
	// and R. Specifically, if M != "" then E and R must both be unset. This is
	// checked during parsing.

	batch bool   // this message was part of a batch
	err   *Error // if not nil, this message is invalid and err is why
}

// isValidID reports whether v is a valid JSON encoding of a request ID.
// Precondition: v is a valid JSON value, or empty.
func isValidID(v json.RawMessage) bool {
	if len(v) == 0 || isNull(v) {
		return true // nil or empty is OK, as is "null"
	} else if v[0] == '"' || v[0] == '-' || (v[0] >= '0' && v[0] <= '9') {
		return true // strings and numbers are OK

		// N.B. This definition does not reject fractional numbers, although the
		// spec says numeric IDs should not have fractional parts.
	}
	return false // anything else is garbage
}

// isValidVersion reports whether v is a valid JSON-RPC version string.
func isValidVersion(v string) bool { return v == Version }

func (j *jmessage) fail(code code.Code, msg string) {
	if j.err == nil {
		j.err = &Error{Code: code, Message: msg}
	}
}

func (j *jmessage) toJSON() ([]byte, error) {
	var sb bytes.Buffer
	sb.WriteString(`{"jsonrpc":"2.0"`)
	if len(j.ID) != 0 {
		sb.WriteString(`,"id":`)
		sb.Write(j.ID)
	}
	switch {
	case j.M != "":
		m, err := json.Marshal(j.M)
		if err != nil {
			return nil, err
		}
		sb.WriteString(`,"method":`)
		sb.Write(m)
		if len(j.P) != 0 {
			sb.WriteString(`,"params":`)
			sb.Write(j.P)
		}

	case len(j.R) != 0:
		sb.WriteString(`,"result":`)
		sb.Write(j.R)

	case j.E != nil:
		e, err := json.Marshal(j.E)
		if err != nil {
			return nil, err
		}
		sb.WriteString(`,"error":`)
		sb.Write(e)
	}

	sb.WriteByte('}')
	return sb.Bytes(), nil
}

func (j *jmessage) parseJSON(data []byte) error {
	// Unmarshal into a map so we can check for extra keys.  The json.Decoder
	// has DisallowUnknownFields, but fails decoding eagerly for fields that do
	// not map to known tags. We want to fully parse the object so we can
	// propagate the "id" in error responses, if it is set. So we have to decode
	// and check the fields ourselves.

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		j.fail(code.ParseError, "request is not a JSON object")
		return j.err
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
			if isValidID(val) {
				j.ID = val
			} else {
				j.fail(code.InvalidRequest, "invalid request ID")
			}
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
			if fb := firstByte(j.P); fb != 0 && fb != '[' && fb != '{' {
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

	// Report an error for an invalid version marker
	if !isValidVersion(j.V) {
		j.fail(code.InvalidRequest, "invalid version marker")
	}

	// Report an error if request/response fields overlap.
	if j.M != "" && (j.E != nil || j.R != nil) {
		j.fail(code.InvalidRequest, "mixed request and reply fields")
	}

	// Report an error for extraneous fields.
	if j.err == nil && len(extra) != 0 {
		j.err = Errorf(code.InvalidRequest, "extra fields in request").WithData(extra)
	}
	return nil
}

// isRequestOrNotification reports whether j is a request or notification.
func (j *jmessage) isRequestOrNotification() bool { return j.M != "" && j.E == nil && j.R == nil }

// isNotification reports whether j is a notification
func (j *jmessage) isNotification() bool { return j.isRequestOrNotification() && fixID(j.ID) == nil }

// fixID filters id, treating "null" as a synonym for an unset ID.  Some
// implementations (possibly a vestige of v1) emit "null" as an ID for
// notifications.
func fixID(id json.RawMessage) json.RawMessage {
	if !isNull(id) {
		return id
	}
	return nil
}

// sender is the subset of channel.Channel needed to send messages.
type sender interface{ Send([]byte) error }

// receiver is the subset of channel.Channel needed to receive messages.
type receiver interface{ Recv() ([]byte, error) }

// encode marshals rsps as JSON and forwards it to the channel.
func encode(ch sender, rsps jmessages) (int, error) {
	bits, err := rsps.toJSON()
	if err != nil {
		return 0, err
	}
	return len(bits), ch.Send(bits)
}

// isNull reports whether msg is exactly the JSON "null" value.
func isNull(msg json.RawMessage) bool {
	return len(msg) == 4 && msg[0] == 'n' && msg[1] == 'u' && msg[2] == 'l' && msg[3] == 'l'
}

// firstByte returns the first non-whitespace byte of data, or 0 if there is none.
func firstByte(data []byte) byte {
	clean := bytes.TrimSpace(data)
	if len(clean) == 0 {
		return 0
	}
	return clean[0]
}

// strictFielder is an optional interface that can be implemented by a type to
// reject unknown fields when unmarshaling from JSON.  If a type does not
// implement this interface, unknown fields are ignored.
type strictFielder interface {
	DisallowUnknownFields()
}

// StrictFields wraps a value v to require unknown fields to be rejected when
// unmarshaling from JSON.
//
// For example:
//
//	var obj RequestType
//	err := req.UnmarshalParams(jrpc2.StrictFields(&obj))`
func StrictFields(v any) any { return &strict{v: v} }

type strict struct{ v any }

func (s *strict) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(s.v)
}
