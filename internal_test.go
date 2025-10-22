// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

// This file contains tests that need to inspect the internal details of the
// implementation to verify that the results are correct.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2/channel"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var errInvalidVersion = &Error{Code: InvalidRequest, Message: "invalid version marker"}

func TestParseRequests(t *testing.T) {
	tests := []struct {
		input string
		want  []*ParsedRequest
		err   error
	}{
		// An empty batch is valid and produces no results.
		{`[]`, nil, nil},

		// An empty single request is invalid but returned anyway.
		{`{}`, []*ParsedRequest{{Error: errInvalidVersion}}, nil},

		// Structurally invalid JSON reports an error.
		{`[`, nil, errInvalidRequest},
		{`{}}`, nil, errInvalidRequest},
		{`[{}, ttt]`, nil, errInvalidRequest},

		// A valid notification.
		{`{"jsonrpc":"2.0", "method": "foo", "params":[1, 2, 3]}`, []*ParsedRequest{{
			Method: "foo",
			Params: json.RawMessage(`[1, 2, 3]`),
		}}, nil},

		// A valid request, with nil parameters.
		{`{"jsonrpc":"2.0", "method": "foo", "id":10332, "params":null}`, []*ParsedRequest{{
			ID: "10332", Method: "foo",
		}}, nil},

		// A valid mixed batch.
		{`[ {"jsonrpc": "2.0", "id": 1, "method": "A", "params": {}},
          {"jsonrpc": "2.0", "params": [5], "method": "B"} ]`, []*ParsedRequest{
			{Method: "A", ID: "1", Params: json.RawMessage(`{}`), Batch: true},
			{Method: "B", Params: json.RawMessage(`[5]`), Batch: true},
		}, nil},

		// An invalid batch.
		{`[{"id": 37, "method": "complain", "params":[]}]`, []*ParsedRequest{
			{Method: "complain", ID: "37", Params: json.RawMessage(`[]`), Batch: true, Error: errInvalidVersion},
		}, nil},

		// A broken request.
		{`{`, nil, Errorf(ParseError, "invalid request value")},

		// A broken batch.
		{`["bad"{]`, nil, Errorf(ParseError, "invalid request value")},
	}
	for _, test := range tests {
		got, err := ParseRequests([]byte(test.input))
		if !errEQ(err, test.err) {
			t.Errorf("ParseRequests(%#q): got error %v, want %v", test.input, err, test.err)
			continue
		}

		diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty())
		if diff != "" {
			t.Errorf("ParseRequests(%#q): wrong result (-want, +got):\n%s", test.input, diff)
		}
	}
}

func errEQ(x, y error) bool {
	if x == nil {
		return y == nil
	} else if y == nil {
		return false
	}
	return ErrorCode(x) == ErrorCode(y) && x.Error() == y.Error()
}

func TestRequest_UnmarshalParams(t *testing.T) {
	type xy struct {
		X int  `json:"x"`
		Y bool `json:"y"`
	}

	tests := []struct {
		input   string
		want    any
		pstring string
		code    Code
	}{
		// If parameters are set, the target should be updated.
		{`{"jsonrpc":"2.0", "id":1, "method":"X", "params":[1,2]}`, []int{1, 2}, "[1,2]", NoError},

		// If parameters are null, the target should not be modified.
		{`{"jsonrpc":"2.0", "id":2, "method":"Y", "params":null}`, "", "", NoError},

		// If parameters are not set, the target should not be modified.
		{`{"jsonrpc":"2.0", "id":2, "method":"Y"}`, 0, "", NoError},

		// Unmarshaling should work into a struct as long as the fields match.
		{`{"jsonrpc":"2.0", "id":3, "method":"Z", "params":{}}`, xy{}, "{}", NoError},
		{`{"jsonrpc":"2.0", "id":4, "method":"Z", "params":{"x":17}}`, xy{X: 17}, `{"x":17}`, NoError},
		{`{"jsonrpc":"2.0", "id":5, "method":"Z", "params":{"x":23, "y":true}}`,
			xy{X: 23, Y: true}, `{"x":23, "y":true}`, NoError},
		{`{"jsonrpc":"2.0", "id":6, "method":"Z", "params":{"x":23, "z":"wat"}}`,
			xy{X: 23}, `{"x":23, "z":"wat"}`, NoError},
	}
	for _, test := range tests {
		var reqs jmessages
		if err := reqs.parseJSON([]byte(test.input)); err != nil {
			t.Errorf("Parsing request %#q failed: %v", test.input, err)
		} else if len(reqs) != 1 {
			t.Fatalf("Wrong number of requests: got %d, want 1", len(reqs))
		}
		req := &Request{id: reqs[0].ID, method: reqs[0].M, params: reqs[0].P}

		// Allocate a zero of the expected type to unmarshal into.
		target := reflect.New(reflect.TypeOf(test.want)).Interface()
		{
			err := req.UnmarshalParams(target)
			if got := ErrorCode(err); got != test.code {
				t.Errorf("UnmarshalParams error: got code %d, want %d [%v]", got, test.code, err)
			}
			if err != nil {
				continue
			}
		}

		// Dereference the target to get the value to compare.
		got := reflect.ValueOf(target).Elem().Interface()
		if diff := cmp.Diff(test.want, got); diff != "" {
			t.Errorf("Parameters(%#q): wrong result (-want, +got):\n%s", test.input, diff)
		}

		// Check that the parameter string matches.
		if got := req.ParamString(); got != test.pstring {
			t.Errorf("ParamString(%#q): got %q, want %q", test.input, got, test.pstring)
		}
	}
}

type hmap map[string]Handler

func (h hmap) Assign(_ context.Context, method string) Handler { return h[method] }
func (h hmap) Names() []string                                 { return nil }

// Verify that if the client context terminates during a request, the client
// will terminate and report failure.
func TestClient_contextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clientDone := make(chan struct{})
		cpipe, spipe := channel.Direct()
		srv := NewServer(hmap{
			"Hang": Handler(func(ctx context.Context, _ *Request) (any, error) {
				<-clientDone
				t.Log("Server unblocked by client completing")
				return true, nil
			}),
		}, nil).Start(spipe)
		c := NewClient(cpipe, nil)
		defer func() {
			c.Close()
			srv.Wait()
		}()

		// Start a call that will hang around until a timer expires or an explicit
		// cancellation is received.
		ctx, cancel := context.WithCancel(t.Context())
		req, err := c.req(ctx, "Hang", nil)
		if err != nil {
			t.Fatalf("c.req(Hang) failed: %v", err)
		}
		rsps, err := c.send(ctx, jmessages{req})
		if err != nil {
			t.Fatalf("c.send(Hang) failed: %v", err)
		}

		// Wait for the handler to be blocked, then cancel the request context
		// for the client and verify that we eventually get free.
		synctest.Wait()
		cancel()

		// The call should fail client side, in the usual way for a cancellation.
		rsp := rsps[0]
		rsp.wait()
		close(clientDone)
		if err := rsp.Error(); err != nil {
			if err.Code != Cancelled {
				t.Errorf("Response error for %q: got %v, want %v", rsp.ID(), err, Cancelled)
			}
		} else {
			t.Errorf("Response for %q: unexpectedly succeeded", rsp.ID())
		}
	})
}

func TestServer_specialMethods(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewServer(hmap{
			"rpc.nonesuch": Handler(func(context.Context, *Request) (any, error) {
				return "OK", nil
			}),
			"donkeybait": Handler(func(context.Context, *Request) (any, error) {
				return true, nil
			}),
		}, nil)
		for _, name := range []string{rpcServerInfo, "donkeybait"} {
			if got := s.assignLocked(t.Context(), name); got == nil {
				t.Errorf("s.assignLocked(%s): no method assigned", name)
			}
		}
		if got := s.assignLocked(t.Context(), "rpc.nonesuch"); got != nil {
			t.Errorf("s.assignLocked(rpc.nonesuch): got %p, want nil", got)
		}
	})
}

// Verify that the option to remove the special behaviour of rpc.* methods can
// be correctly disabled by the server options.
func TestServer_disableBuiltinHook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewServer(hmap{
			"rpc.nonesuch": Handler(func(context.Context, *Request) (any, error) {
				return "OK", nil
			}),
		}, &ServerOptions{DisableBuiltin: true})

		// With builtins disabled, the default rpc.* methods should not get assigned.
		for _, name := range []string{rpcServerInfo} {
			if got := s.assignLocked(t.Context(), name); got != nil {
				t.Errorf("s.assignLocked(%s): got %p, wanted nil", name, got)
			}
		}

		// However, user-assigned methods with this prefix should now work.
		if got := s.assignLocked(t.Context(), "rpc.nonesuch"); got == nil {
			t.Error("s.assignLocked(rpc.nonesuch): missing assignment")
		}
	})
}

// Verify that a batch request gets a batch reply, even if it is only a single
// request. The Client never sends requests like that, but the server needs to
// cope with it correctly.
func TestBatchReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cpipe, spipe := channel.Direct()
		srv := NewServer(hmap{
			"test": Handler(func(_ context.Context, req *Request) (any, error) {
				return req.Method() + " OK", nil
			}),
		}, nil).Start(spipe)
		defer func() { cpipe.Close(); srv.Wait() }()

		tests := []struct {
			input, want string
		}{
			// A single-element batch gets returned as a batch.
			{`[{"jsonrpc":"2.0", "id":1, "method":"test"}]`,
				`[{"jsonrpc":"2.0","id":1,"result":"test OK"}]`},

			// A single-element non-batch gets returned as a single reply.
			{`{"jsonrpc":"2.0", "id":2, "method":"test"}`,
				`{"jsonrpc":"2.0","id":2,"result":"test OK"}`},
		}
		for _, test := range tests {
			if err := cpipe.Send([]byte(test.input)); err != nil {
				t.Errorf("Send failed: %v", err)
			}
			rsp, err := cpipe.Recv()
			if err != nil {
				t.Errorf("Recv failed: %v", err)
			}
			if got := string(rsp); got != test.want {
				t.Errorf("Batch reply:\n got %#q\nwant %#q", got, test.want)
			}
		}
	})
}

func TestMarshalResponse(t *testing.T) {
	tests := []struct {
		id     string
		err    *Error
		result string
		want   string
	}{
		{"", nil, "", `{"jsonrpc":"2.0"}`},
		{"null", nil, "", `{"jsonrpc":"2.0","id":null}`},
		{"123", Errorf(ParseError, "failed"), "",
			`{"jsonrpc":"2.0","id":123,"error":{"code":-32700,"message":"failed"}}`},
		{"456", nil, `{"ok":true,"values":[4,5,6]}`,
			`{"jsonrpc":"2.0","id":456,"result":{"ok":true,"values":[4,5,6]}}`},
	}
	for _, test := range tests {
		rsp := &Response{id: test.id, err: test.err}
		if test.err == nil {
			rsp.result = json.RawMessage(test.result)
		}

		got, err := json.Marshal(rsp)
		if err != nil {
			t.Errorf("Marshaling %+v: unexpected error: %v", rsp, err)
		} else if s := string(got); s != test.want {
			t.Errorf("Marshaling %+v: got %#q, want %#q", rsp, s, test.want)
		}
	}
}
