package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"bitbucket.org/creachadair/jrpc2/code"
)

// Error is the concrete type of errors returned from RPC calls.
type Error struct {
	Code    code.Code
	Message string

	data json.RawMessage
}

// Error renders e to a human-readable string for the error interface.
func (e Error) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// HasData reports whether e has error data to unmarshal.
func (e Error) HasData() bool { return len(e.data) != 0 }

// UnmarshalData decodes the error data associated with e into v.  It returns
// ErrNoData without modifying v if there was no data message attached to e.
func (e Error) UnmarshalData(v interface{}) error {
	if !e.HasData() {
		return ErrNoData
	}
	return json.Unmarshal([]byte(e.data), v)
}

func (e Error) tojerror() *jerror {
	return &jerror{Code: int32(e.Code), Msg: e.Message, Data: e.data}
}

// ErrNoData indicates that there are no data to unmarshal.
var ErrNoData = errors.New("no data to unmarshal")

// errServerStopped is returned by Server.Wait when the server was shut down by
// an explicit call to its Stop method.
var errServerStopped = errors.New("the server has been stopped")

// errClientStopped is the error reported when a client is shut down by an
// explicit call to its Close method.
var errClientStopped = errors.New("the client has been stopped")

// Errorf returns an error value of concrete type *Error having the specified
// code and formatted message string.
// It is shorthand for DataErrorf(code, nil, msg, args...)
func Errorf(code code.Code, msg string, args ...interface{}) error {
	return DataErrorf(code, nil, msg, args...)
}

// DataErrorf returns an error value of concrete type *Error having the
// specified code, error data, and formatted message string.
// If v == nil this behaves identically to Errorf(code, msg, args...).
func DataErrorf(code code.Code, v interface{}, msg string, args ...interface{}) error {
	e := &Error{Code: code, Message: fmt.Sprintf(msg, args...)}
	if v != nil {
		if data, err := json.Marshal(v); err == nil {
			e.data = data
		}
	}
	return e
}

// ErrorCode reports the error code associated with err.
// If err == nil, code.NoError is returned.
// If err is a Code, that code is returned.
// If err has type *Error, its code is returned.
// Otherwise code.SystemError is returned.
func ErrorCode(err error) code.Code {
	switch t := err.(type) {
	case nil:
		return code.NoError
	case *Error:
		return t.Code
	case code.Code:
		return t
	default:
		switch err {
		case context.Canceled:
			return code.Cancelled
		case context.DeadlineExceeded:
			return code.DeadlineExceeded
		default:
			return code.SystemError
		}
	}
}
