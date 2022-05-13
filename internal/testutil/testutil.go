// Copyright (C) 2022 Michael J. Fromberger. All Rights Reserved.

// Package testutil defines internal support code for writing tests.
package testutil

import (
	"fmt"
	"testing"

	"github.com/creachadair/jrpc2"
)

// ParseRequest parses a single JSON request object.
func ParseRequest(s string) (_ *jrpc2.Request, err error) {
	// Check syntax.
	reqs, err := jrpc2.ParseRequests([]byte(s))
	if err != nil {
		return nil, err
	} else if len(reqs) != 1 {
		return nil, fmt.Errorf("got %d requests, want 1", len(reqs))
	} else if reqs[0].Error != nil {
		return nil, reqs[0].Error
	}
	return reqs[0].ToRequest(), nil
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
