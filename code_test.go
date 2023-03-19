// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/creachadair/jrpc2"
)

type testCoder jrpc2.Code

func (t testCoder) ErrCode() jrpc2.Code { return jrpc2.Code(t) }
func (testCoder) Error() string         { return "bogus" }

func TestErrorCode(t *testing.T) {
	tests := []struct {
		input error
		want  jrpc2.Code
	}{
		{nil, jrpc2.NoError},
		{testCoder(jrpc2.ParseError), jrpc2.ParseError},
		{testCoder(jrpc2.InvalidRequest), jrpc2.InvalidRequest},
		{fmt.Errorf("wrapped parse error: %w", jrpc2.ParseError.Err()), jrpc2.ParseError},
		{context.Canceled, jrpc2.Cancelled},
		{fmt.Errorf("wrapped cancellation: %w", context.Canceled), jrpc2.Cancelled},
		{context.DeadlineExceeded, jrpc2.DeadlineExceeded},
		{fmt.Errorf("wrapped deadline: %w", context.DeadlineExceeded), jrpc2.DeadlineExceeded},
		{errors.New("other"), jrpc2.SystemError},
		{io.EOF, jrpc2.SystemError},
	}
	for _, test := range tests {
		if got := jrpc2.ErrorCode(test.input); got != test.want {
			t.Errorf("ErrorCode(%v): got %v, want %v", test.input, got, test.want)
		}
	}
}

func TestCodeIs(t *testing.T) {
	tests := []struct {
		code jrpc2.Code
		err  error
		want bool
	}{
		{jrpc2.NoError, nil, true},
		{0, nil, false},
		{1, jrpc2.Code(1).Err(), true},
		{2, jrpc2.Code(3).Err(), false},
		{4, fmt.Errorf("blah: %w", jrpc2.Code(4).Err()), true},
		{5, fmt.Errorf("nope: %w", jrpc2.Code(6).Err()), false},
	}
	for _, test := range tests {
		cerr := test.code.Err()
		got := errors.Is(test.err, cerr)
		if got != test.want {
			t.Errorf("Is(%v, %v): got %v, want %v", test.err, cerr, got, test.want)
		}
	}
}

func TestErr(t *testing.T) {
	eqv := func(e1, e2 error) bool {
		return e1 == e2 || (e1 != nil && e2 != nil && e1.Error() == e2.Error())
	}
	type test struct {
		code jrpc2.Code
		want error
	}
	tests := []test{
		{jrpc2.NoError, nil},
		{0, errors.New("error code 0")},
		{1, errors.New("error code 1")},
		{-17, errors.New("error code -17")},
	}

	// Make sure all the pre-defined errors get their messages hit.
	for _, v := range []int32{
		// Codes reserved by the JSON-RPC 2.0 spec.
		-32700, -32600, -32601, -32602, -32603,
		// Codes reserved by this implementation.
		-32098, -32097, -32096,
	} {
		c := jrpc2.Code(v)
		tests = append(tests, test{
			code: c,
			want: errors.New(c.String()),
		})
	}
	for _, test := range tests {
		got := test.code.Err()
		if !eqv(got, test.want) {
			t.Errorf("Code(%d).Err(): got %#v, want %#v", test.code, got, test.want)
		}
		if c := jrpc2.ErrorCode(got); c != test.code {
			t.Errorf("Code(%d).Err(): got code %v, want %v", test.code, c, test.code)
		}
	}
}
