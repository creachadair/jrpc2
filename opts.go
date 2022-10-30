// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/creachadair/jrpc2/code"
)

// ServerOptions control the behaviour of a server created by NewServer.
// A nil *ServerOptions provides sensible defaults.
// It is safe to share server options among multiple server instances.
type ServerOptions struct {
	// If not nil, send debug text logs here.
	Logger Logger

	// If not nil, the methods of this value are called to log each request
	// received and each response or error returned.
	RPCLog RPCLogger

	// Instructs the server to allow server callbacks and notifications, a
	// non-standard extension to the JSON-RPC protocol. If AllowPush is false,
	// the Notify and Callback methods of the server report errors if called.
	AllowPush bool

	// Instructs the server to disable the built-in rpc.* handler methods.
	//
	// By default, a server reserves all rpc.* methods, even if the given
	// assigner maps them. When this option is true, rpc.* methods are passed
	// along to the given assigner.
	DisableBuiltin bool

	// Allows up to the specified number of goroutines to execute in parallel in
	// request handlers. A value less than 1 uses runtime.NumCPU().  Note that
	// this setting does not constrain order of issue.
	Concurrency int

	// If set, this function is called to create a new base request context.
	// If unset, the server uses a background context.
	NewContext func() context.Context

	// If nonzero this value as the server start time; otherwise, use the
	// current time when Start is called. All servers created from the same
	// options will share the same start time if one is set.
	StartTime time.Time
}

func (s *ServerOptions) logFunc() func(string, ...any) {
	if s == nil || s.Logger == nil {
		return func(string, ...any) {}
	}
	return s.Logger.Printf
}

func (s *ServerOptions) allowPush() bool    { return s != nil && s.AllowPush }
func (s *ServerOptions) allowBuiltin() bool { return s == nil || !s.DisableBuiltin }

func (s *ServerOptions) concurrency() int64 {
	if s == nil || s.Concurrency < 1 {
		return int64(runtime.NumCPU())
	}
	return int64(s.Concurrency)
}

func (s *ServerOptions) startTime() time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.StartTime
}

func (o *ServerOptions) newContext() func() context.Context {
	if o == nil || o.NewContext == nil {
		return context.Background
	}
	return o.NewContext
}

func (s *ServerOptions) rpcLog() RPCLogger {
	if s == nil || s.RPCLog == nil {
		return nullRPCLogger{}
	}
	return s.RPCLog
}

// ClientOptions control the behaviour of a client created by NewClient.
// A nil *ClientOptions provides sensible defaults.
type ClientOptions struct {
	// If not nil, send debug text logs here.
	Logger Logger

	// If set, this function is called if a notification is received from the
	// server. If unset, server notifications are logged and discarded.  At
	// most one invocation of the callback will be active at a time.
	// Server notifications are a non-standard extension of JSON-RPC.
	OnNotify func(*Request)

	// If set, this function is called if a request is received from the server.
	// If unset, server requests are logged and discarded. Multiple invocations
	// of the callback handler may be active concurrently.
	//
	// The callback handler can retrieve the client from its context using the
	// jrpc2.ClientFromContext function. The context terminates when the client
	// is closed.
	//
	// If a callback handler panics, the client will recover the panic and
	// report a system error back to the server describing the error.
	//
	// Server callbacks are a non-standard extension of JSON-RPC.
	OnCallback func(context.Context, *Request) (any, error)

	// If set, this function is called when the context for a request terminates.
	// The function receives the client and the response that was cancelled.
	// The hook can obtain the ID and error value from rsp.
	//
	// Note that the hook does not receive the request context, which has
	// already ended by the time the hook is called.
	OnCancel func(cli *Client, rsp *Response)
}

func (c *ClientOptions) logFunc() func(string, ...any) {
	if c == nil || c.Logger == nil {
		return func(string, ...any) {}
	}
	return c.Logger.Printf
}

func (c *ClientOptions) handleNotification() func(*jmessage) {
	if c == nil || c.OnNotify == nil {
		return nil
	}
	h := c.OnNotify
	return func(req *jmessage) { h(&Request{method: req.M, params: req.P}) }
}

func (c *ClientOptions) handleCancel() func(*Client, *Response) {
	if c == nil {
		return nil
	}
	return c.OnCancel
}

func (c *ClientOptions) handleCallback() func(context.Context, *jmessage) []byte {
	if c == nil || c.OnCallback == nil {
		return nil
	}
	cb := c.OnCallback
	return func(ctx context.Context, req *jmessage) []byte {
		// Recover panics from the callback handler to ensure the server gets a
		// response even if the callback fails without a result.
		//
		// Otherwise, a client and a server (a) running in the same process, and
		// (b) where panics are recovered at a higher level, and (c) without
		// cleaning up the client, can cause the server to stall in a manner that
		// is difficult to debug.
		//
		// See https://github.com/creachadair/jrpc2/issues/41.
		rsp := &jmessage{ID: req.ID}
		v, err := panicToError(func() (any, error) {
			return cb(ctx, &Request{
				id:     req.ID,
				method: req.M,
				params: req.P,
			})
		})
		if err == nil {
			rsp.R, err = json.Marshal(v)
		}
		if err != nil {
			rsp.R = nil
			if e, ok := err.(*Error); ok {
				rsp.E = e
			} else {
				rsp.E = &Error{Code: code.FromError(err), Message: err.Error()}
			}
		}
		bits, _ := rsp.toJSON()
		return bits
	}
}

func panicToError(f func() (any, error)) (v any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic in callback handler: %v", p)
		}
	}()
	return f()
}

// A Logger records text logs from a server or a client. A nil logger discards
// text log input.
type Logger func(text string)

// Printf writes a formatted message to the logger. If lg == nil, the message
// is discarded.
func (lg Logger) Printf(msg string, args ...any) {
	if lg != nil {
		lg(fmt.Sprintf(msg, args...))
	}
}

// StdLogger adapts a *log.Logger to a Logger. If logger == nil, the returned
// function sends logs to the default logger.
func StdLogger(logger *log.Logger) Logger {
	if logger == nil {
		return func(text string) { log.Output(2, text) }
	}
	return func(text string) { logger.Output(2, text) }
}

// An RPCLogger receives callbacks from a server to record the receipt of
// requests and the delivery of responses. These callbacks are invoked
// synchronously with the processing of the request.
type RPCLogger interface {
	// Called for each request received prior to invoking its handler.
	LogRequest(ctx context.Context, req *Request)

	// Called for each response produced by a handler, immediately prior to
	// sending it back to the client. The inbound request can be recovered from
	// the context using jrpc2.InboundRequest.
	LogResponse(ctx context.Context, rsp *Response)
}

type nullRPCLogger struct{}

func (nullRPCLogger) LogRequest(context.Context, *Request)   {}
func (nullRPCLogger) LogResponse(context.Context, *Response) {}
