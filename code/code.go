// Package code defines error code values used by the jrpc2 package.
package code

import "fmt"

// A Code is an error response code, that satisfies the error interface.
type Code int32

func (c Code) Error() string {
	if s, ok := stdError[c]; ok {
		return s
	}
	return fmt.Sprintf("error code %d", c)
}

// Pre-defined error codes, including the standard ones from the JSON-RPC
// specification and some specific to this implementation.
const (
	ParseError     Code = -32700 // Invalid JSON received by the server
	InvalidRequest Code = -32600 // The JSON sent is not a valid request object
	MethodNotFound Code = -32601 // The method does not exist or is unavailable
	InvalidParams  Code = -32602 // Invalid method parameters
	InternalError  Code = -32603 // Internal JSON-RPC error

	// The JSON-RPC 2.0 specification reserves the range -32000 to -32099 for
	// implementation-defined server errors, such as the following:

	NoError          Code = -32099 // Denotes a nil error
	SystemError      Code = -32098 // Errors from the operating environment
	Cancelled        Code = -32097 // Request cancelled
	DeadlineExceeded Code = -32096 // Request deadline exceeded
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
