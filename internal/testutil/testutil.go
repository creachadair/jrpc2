// Copyright (C) 2022 Michael J. Fromberger. All Rights Reserved.

// Package testutil defines internal support code for writing tests.
package testutil

import (
	"context"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
)

// ParseRequest parses a single JSON request object.
func ParseRequest(s string) (_ *jrpc2.Request, err error) {
	// Check syntax.
	if _, err := jrpc2.ParseRequests([]byte(s)); err != nil {
		return nil, err
	}

	cch, sch := channel.Direct()
	rs := newRequestStub()
	srv := jrpc2.NewServer(rs, nil).Start(sch)
	defer func() {
		cch.Close()
		serr := srv.Wait()
		if err == nil {
			err = serr
		}
	}()
	if err := cch.Send([]byte(s)); err != nil {
		return nil, err
	}
	req := <-rs.reqc
	if !rs.isNote {
		cch.Recv()
	}
	return req, nil
}

// MustParseRequest calls ParseRequest and fails t if it reports an error.
func MustParseRequest(t *testing.T, s string) *jrpc2.Request {
	t.Helper()

	req, err := ParseRequest(s)
	if err != nil {
		t.Fatalf("Parsing %#q failed: %v", s, err)
	}
	return req
}

func newRequestStub() *requestStub {
	return &requestStub{reqc: make(chan *jrpc2.Request, 1)}
}

type requestStub struct {
	reqc   chan *jrpc2.Request
	isNote bool
}

func (r *requestStub) Assign(context.Context, string) jrpc2.Handler { return r }

func (r *requestStub) Handle(_ context.Context, req *jrpc2.Request) (interface{}, error) {
	defer close(r.reqc)
	r.isNote = req.IsNotification()
	r.reqc <- req
	return nil, nil
}
