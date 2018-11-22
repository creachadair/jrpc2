// Package code defines error code values used by the jrpc2 package.
package code

import (
	"context"
	"errors"
	"fmt"
)

// A Code is an error response code.
//
// Code values from and including -32768 to -32000 are reserved for pre-defined
// JSON-RPC errors.  Any code within this range, but not defined explicitly
// below is reserved for future use.  The remainder of the space is available
// for application defined errors.
//
// See also: https://www.jsonrpc.org/specification#error_object
type Code int32

func (c Code) String() string {
	if s, ok := stdError[c]; ok {
		return s
	}
	return fmt.Sprintf("error code %d", c)
}

// A Coder is a value that can report an error code value.
type Coder interface {
	Code() Code
}

// Err converts c to an error value, which is nil for code.NoError and
// otherwise an error value constructed by fmt.Errorf.
func (c Code) Err() error {
	if c == NoError {
		return nil
	} else if s, ok := stdError[c]; ok {
		return fmt.Errorf("[%d] %s", c, s)
	}
	return errors.New(c.String())
}

// Pre-defined error codes, including the standard ones from the JSON-RPC
// specification and some specific to this implementation.
const (
	ParseError     Code = -32700 // Invalid JSON received by the server
	InvalidRequest Code = -32600 // The JSON sent is not a valid request object
	MethodNotFound Code = -32601 // The method does not exist or is unavailable
	InvalidParams  Code = -32602 // Invalid method parameters
	InternalError  Code = -32603 // Internal JSON-RPC error
)

// The JSON-RPC 2.0 specification reserves the range -32000 to -32099 for
// implementation-defined server errors. These are used by the jrpc2 package.
const (
	NoError          Code = -32099 // Denotes a nil error (used by FromError)
	SystemError      Code = -32098 // Errors from the operating environment
	Cancelled        Code = -32097 // Request cancelled (context.Canceled)
	DeadlineExceeded Code = -32096 // Request deadline exceeded (context.DeadlineExceeded)
)

var stdError = map[Code]string{
	ParseError:     "parse error",
	InvalidRequest: "invalid request",
	MethodNotFound: "method not found",
	InvalidParams:  "invalid parameters",
	InternalError:  "internal error",

	NoError:          "no error (success)",
	SystemError:      "system error",
	Cancelled:        "request cancelled",
	DeadlineExceeded: "deadline exceeded",
}

// Register adds a new Code value with the specified message string.  This
// function will panic if the proposed value is already registered.
func Register(value int32, message string) Code {
	code := Code(value)
	if s, ok := stdError[code]; ok {
		panic(fmt.Sprintf("code %d is already registered for %q", code, s))
	}
	stdError[code] = message
	return code
}

// FromError returns a Code to categorize the specified error.
// If err == nil, it returns code.NoError.
// If err is a Coder, it returns the reported code value.
// If err is context.Canceled, it returns code.Cancelled.
// If err is context.DeadlineExceeded, it returns code.DeadlineExceeded.
// Otherwise it returns code.SystemError.
func FromError(err error) Code {
	switch t := err.(type) {
	case nil:
		return NoError
	case Coder:
		return t.Code()
	}
	switch err {
	case context.Canceled:
		return Cancelled
	case context.DeadlineExceeded:
		return DeadlineExceeded
	default:
		return SystemError
	}
}
