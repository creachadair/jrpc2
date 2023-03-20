// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error is the concrete type of errors returned from RPC calls.
// It also represents the JSON encoding of the JSON-RPC error object.
type Error struct {
	Code    Code            `json:"code"`              // the machine-readable error code
	Message string          `json:"message,omitempty"` // the human-readable error message
	Data    json.RawMessage `json:"data,omitempty"`    // optional ancillary error data
}

// Error returns a human-readable description of e.
func (e Error) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// ErrCode trivially satisfies the ErrCoder interface for an *Error.
func (e Error) ErrCode() Code { return e.Code }

// WithData marshals v as JSON and constructs a copy of e whose Data field
// includes the result. If v == nil or if marshaling v fails, e is returned
// without modification.
func (e *Error) WithData(v any) *Error {
	if v == nil {
		return e
	} else if data, err := json.Marshal(v); err == nil {
		return &Error{Code: e.Code, Message: e.Message, Data: data}
	}
	return e
}

// errServerStopped is returned by Server.Wait when the server was shut down by
// an explicit call to its Stop method or orderly termination of its channel.
var errServerStopped = errors.New("the server has been stopped")

// errClientStopped is the error reported when a client is shut down by an
// explicit call to its Close method.
var errClientStopped = errors.New("the client has been stopped")

// errEmptyMethod is the error reported for an empty request method name.
var errEmptyMethod = &Error{Code: InvalidRequest, Message: "empty method name"}

// errNoSuchMethod is the error reported for an unknown method name.
var errNoSuchMethod = &Error{Code: MethodNotFound, Message: MethodNotFound.String()}

// errDuplicateID is the error reported for a duplicated request ID.
var errDuplicateID = &Error{Code: InvalidRequest, Message: "duplicate request ID"}

// errInvalidRequest is the error reported for an invalid request object or batch.
var errInvalidRequest = &Error{Code: ParseError, Message: "invalid request value"}

// errEmptyBatch is the error reported for an empty request batch.
var errEmptyBatch = &Error{Code: InvalidRequest, Message: "empty request batch"}

// errInvalidParams is the error reported for invalid request parameters.
var errInvalidParams = &Error{Code: InvalidParams, Message: InvalidParams.String()}

// errTaskNotExecuted is the internal sentinel error for an unassigned task.
var errTaskNotExecuted = new(Error)

// ErrConnClosed is returned by a server's push-to-client methods if they are
// called after the client connection is closed.
var ErrConnClosed = errors.New("client connection is closed")

// Errorf returns an error value of concrete type *Error having the specified
// code and formatted message string.
func Errorf(code Code, msg string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(msg, args...)}
}
