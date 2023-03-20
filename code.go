// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"context"
	"errors"
	"fmt"
)

// A Code is an error code included in the JSON-RPC error object.
//
// Code values from and including -32768 to -32000 are reserved for predefined
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

// An ErrCoder is a value that can report an error code value.
type ErrCoder interface {
	ErrCode() Code
}

// A codeError wraps a Code to satisfy the standard error interface.  This
// indirection prevents a Code from accidentally being used as an error value.
// It also satisfies the ErrCoder interface, allowing the code to be recovered.
type codeError Code

// Error satisfies the error interface using the built-in string for the code,
// if one is defined, or else a placeholder that describes the value.
func (c codeError) Error() string { return Code(c).String() }

// ErrCode trivially satisfies the ErrCoder interface.
func (c codeError) ErrCode() Code { return Code(c) }

// Is reports whether err is c or has a code equal to c.
func (c codeError) Is(err error) bool {
	v, ok := err.(ErrCoder) // including codeError
	return ok && v.ErrCode() == Code(c)
}

// Err converts c to an error value, which is nil for NoError and otherwise an
// error value whose code is c and whose text is based on the built-in string
// for c if one exists.
func (c Code) Err() error {
	if c == NoError {
		return nil
	}
	return codeError(c)
}

// Error codes from and including -32768 to -32000 are reserved for pre-defined
// errors by the JSON-RPC specification. These constants cover the standard
// codes and implementation-specific codes used by the jrpc2 module.
const (
	ParseError     Code = -32700 // [std] Invalid JSON received by the server
	InvalidRequest Code = -32600 // [std] The JSON sent is not a valid request object
	MethodNotFound Code = -32601 // [std] The method does not exist or is unavailable
	InvalidParams  Code = -32602 // [std] Invalid method parameters
	InternalError  Code = -32603 // [std] Internal JSON-RPC error

	NoError          Code = -32099 // Denotes a nil error (used by ErrorCode)
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

// ErrorCode returns a Code to categorize the specified error.
//
//   - If err == nil, it returns jrpc2.NoError.
//   - If err is (or wraps) an ErrCoder, it returns the reported code value.
//   - If err is context.Canceled, it returns jrpc2.Cancelled.
//   - If err is context.DeadlineExceeded, it returns jrpc2.DeadlineExceeded.
//   - Otherwise it returns jrpc2.SystemError.
func ErrorCode(err error) Code {
	if err == nil {
		return NoError
	}
	var c ErrCoder
	if errors.As(err, &c) {
		return c.ErrCode()
	} else if errors.Is(err, context.Canceled) {
		return Cancelled
	} else if errors.Is(err, context.DeadlineExceeded) {
		return DeadlineExceeded
	}
	return SystemError
}
