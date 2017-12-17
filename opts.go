package jrpc2

import (
	"context"
	"io"
	"log"

	"golang.org/x/sync/semaphore"
)

// An ServerOption controls an optional behaviour of a Server.
type ServerOption func(*Server)

// LogTo enables debug logging to the specified writer.
func LogTo(w io.Writer) ServerOption {
	return func(s *Server) { s.lw = log.New(w, "[jrpc2] ", log.LstdFlags|log.Lshortfile) }
}

// AllowV1 instructs the server whether to tolerate requests that do not
// include the required "jsonrpc" version marker.
func AllowV1(ok bool) ServerOption { return func(s *Server) { s.allow1 = ok } }

// Concurrency allows up to the specified number of concurrent goroutines to
// execute when processing requests. A value less than 1 is treated as 1.
func Concurrency(n int) ServerOption {
	if n <= 0 {
		n = 1
	}
	return func(s *Server) { s.sem = semaphore.NewWeighted(int64(n)) }
}

// ReqContext provides a function that the server will call to obtain a context
// value to use for each inbound request. By default the server uses background
// context.
func ReqContext(f func(*Request) context.Context) ServerOption {
	return func(s *Server) { s.reqctx = f }
}
