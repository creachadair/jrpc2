package jrpc2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime"
)

const logFlags = log.LstdFlags | log.Lshortfile

// ServerOptions control the behaviour of a server created by NewServer.
// A nil *ServerOptions provides sensible defaults.
type ServerOptions struct {
	// If not nil, send debug logs to this writer.
	LogWriter io.Writer

	// Instructs the server to tolerate requests that do not include the
	// required "jsonrpc" version marker.
	AllowV1 bool

	// Allows up to the specified number of concurrent goroutines to execute
	// when processing requests. A value less than 1 uses runtime.NumCPU().
	Concurrency int

	// If not nil, this function is called to obtain a context value to use for
	// each inbound request. By default, a server uses the background context.
	RequestContext func(*Request) (context.Context, error)

	// If not nil, this value is used to capture server statistics.
	ServerInfo *ServerInfo
}

func (s *ServerOptions) logger() func(string, ...interface{}) {
	if s == nil || s.LogWriter == nil {
		return func(string, ...interface{}) {}
	}
	logger := log.New(s.LogWriter, "[jrpc2.Server] ", logFlags)
	return func(msg string, args ...interface{}) { logger.Output(2, fmt.Sprintf(msg, args...)) }
}

func (s *ServerOptions) allowV1() bool { return s != nil && s.AllowV1 }

func (s *ServerOptions) concurrency() int64 {
	if s == nil || s.Concurrency < 1 {
		return int64(runtime.NumCPU())
	}
	return int64(s.Concurrency)
}

func (s *ServerOptions) reqContext() func(*Request) (context.Context, error) {
	if s == nil || s.RequestContext == nil {
		return func(*Request) (context.Context, error) { return context.Background(), nil }
	}
	return s.RequestContext
}

func (s *ServerOptions) serverInfo() *ServerInfo {
	if s == nil || s.ServerInfo == nil {
		return new(ServerInfo)
	}
	return s.ServerInfo
}

// ClientOptions control the behaviour of a client created by NewClient.
// A nil *ClientOptions provides sensible defaults.
type ClientOptions struct {
	// If not nil, send debug logs to this writer.
	LogWriter io.Writer

	// Instructs the client to tolerate responses that do not include the
	// required "jsonrpc" version marker.
	AllowV1 bool

	// If set, this function is called with the context and encoded request
	// parameters before the request is sent to the server. Its return value
	// replaces the request parameters. This allows the client to send context
	// metadata along with the request. If unset, the parameters are unchanged.
	EncodeContext func(context.Context, json.RawMessage) (json.RawMessage, error)
}

// ClientLog enables debug logging to the specified writer.
func (c *ClientOptions) logger() func(string, ...interface{}) {
	if c == nil || c.LogWriter == nil {
		return func(string, ...interface{}) {}
	}
	logger := log.New(c.LogWriter, "[jrpc2.Client] ", logFlags)
	return func(msg string, args ...interface{}) { logger.Output(2, fmt.Sprintf(msg, args...)) }
}

func (c *ClientOptions) allowV1() bool { return c != nil && c.AllowV1 }

func (c *ClientOptions) encodeContext() func(context.Context, json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.EncodeContext == nil {
		return func(_ context.Context, params json.RawMessage) (json.RawMessage, error) { return params, nil }
	}
	return c.EncodeContext
}
