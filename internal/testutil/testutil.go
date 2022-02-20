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
func ParseRequest(s string) (req *jrpc2.Request, err error) {
	cch, sch := channel.Direct()

	srv := jrpc2.NewServer(requestStub{req: &req}, nil).Start(sch)
	defer func() {
		cch.Close()
		serr := srv.Wait()
		if err == nil {
			err = serr
		}
	}()

	if err := cch.Send([]byte(s)); err != nil {
		srv.Stop()
		return nil, err
	} else if _, err := cch.Recv(); err != nil {
		return nil, err
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

type requestStub struct{ req **jrpc2.Request }

func (r requestStub) Assign(context.Context, string) jrpc2.Handler { return r }

func (r requestStub) Handle(_ context.Context, req *jrpc2.Request) (interface{}, error) {
	*r.req = req
	return nil, nil
}
