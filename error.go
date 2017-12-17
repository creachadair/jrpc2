package jrpc2

import (
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

// ErrServerStopped is returned by Server.Wait when the server was shut down by
// an explicit call to its Stop method.
var ErrServerStopped = errors.New("the server has been stopped")

// Errorf returns an error value of concrete type *Error having the specified
// code and formatted message string.
func Errorf(code Code, msg string, args ...interface{}) error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(msg, args...),
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

// Well-known error codes defined by the JSON-RPC specification.
const (
	E_ParseError     Code = -32700 // Invalid JSON received by the server
	E_InvalidRequest Code = -32600 // The JSON sent is not a valid request object
	E_MethodNotFound Code = -32601 // The method does not exist or is unavailable
	E_InvalidParams  Code = -32602 // Invalid method parameters
	E_InternalError  Code = -32603 // Internal JSON-RPC error
)

var stdError = map[Code]string{
	E_ParseError:     "parse error",
	E_InvalidRequest: "invalid request",
	E_MethodNotFound: "method not found",
	E_InvalidParams:  "invalid parameters",
	E_InternalError:  "internal error",
}
