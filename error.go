package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Error is the concrete type of errors returned from RPC calls.
type Error struct {
	Code    Code
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
func Errorf(code Code, msg string, args ...interface{}) error {
	return DataErrorf(code, nil, msg, args...)
}

// DataErrorf returns an error value of concrete type *Error having the
// specified code, error data, and formatted message string.
// If v == nil this behaves identically to Errorf(code, msg, args...).
func DataErrorf(code Code, v interface{}, msg string, args ...interface{}) error {
	e := &Error{Code: code, Message: fmt.Sprintf(msg, args...)}
	if v != nil {
		if data, err := json.Marshal(v); err == nil {
			e.data = data
		}
	}
	return e
}

// ErrorCode reports the error code associated with err. If err == nil,
// E_NoError is returned. If err is a Code, that code is returned. If err has
// type *Error, its code is returned. Otherwise E_SystemError is returned.
func ErrorCode(err error) Code {
	switch t := err.(type) {
	case nil:
		return E_NoError
	case *Error:
		return t.Code
	case Code:
		return t
	default:
		switch err {
		case context.Canceled:
			return E_Cancelled
		case context.DeadlineExceeded:
			return E_DeadlineExceeded
		default:
			return E_SystemError
		}
	}
}

// A Code is an error response code, that satisfies the Error interface.  Codes
// can be used directly as error values, but a more useful value can be
// obtained by passing a Code to the Errorf function along with a descriptive
// message.
type Code int32

func (c Code) Error() string {
	if s, ok := stdError[c]; ok {
		return s
	}
	return fmt.Sprintf("error code %d", c)
}

// ToError converts a Code to an *Error using its default message.
func (c Code) ToError() *Error { return &Error{Code: c, Message: c.Error()} }

// Pre-defined error codes, including the standard ones from the JSON-RPC
// specification and some specific to this implementation.
const (
	E_ParseError     Code = -32700 // Invalid JSON received by the server
	E_InvalidRequest Code = -32600 // The JSON sent is not a valid request object
	E_MethodNotFound Code = -32601 // The method does not exist or is unavailable
	E_InvalidParams  Code = -32602 // Invalid method parameters
	E_InternalError  Code = -32603 // Internal JSON-RPC error

	// The JSON-RPC 2.0 specification reserves the range -32000 to -32099 for
	// implementation-defined server errors, such as the following:

	E_NoError          Code = -32099 // Denotes a nil error
	E_SystemError      Code = -32098 // Errors from the operating environment
	E_Cancelled        Code = -32097 // Request cancelled
	E_DeadlineExceeded Code = -32096 // Request deadline exceeded
)

var stdError = map[Code]string{
	E_ParseError:     "parse error",
	E_InvalidRequest: "invalid request",
	E_MethodNotFound: "method not found",
	E_InvalidParams:  "invalid parameters",
	E_InternalError:  "internal error",

	E_NoError:          "no error (success)",
	E_SystemError:      "system error",
	E_Cancelled:        "request cancelled",
	E_DeadlineExceeded: "deadline exceeded",
}

// RegisterCode adds a new Code value with the specified message string.  This
// function will panic if the proposed value is already registered.
func RegisterCode(value int32, message string) Code {
	code := Code(value)
	if s, ok := stdError[code]; ok {
		panic(fmt.Sprintf("code %d is already registered for %q", code, s))
	}
	stdError[code] = message
	return code
}
