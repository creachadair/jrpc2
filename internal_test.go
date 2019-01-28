package jrpc2

// This file contains tests that need to inspect the internal details of the
// implementation to verify that the results are correct.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/code"
	"github.com/kylelemons/godebug/pretty"
)

func TestParseRequests(t *testing.T) {
	tests := []struct {
		input string
		want  []*Request
		err   error
	}{
		// An empty batch is valid and produces no results.
		{`[]`, nil, nil},

		// An empty single request is invalid but returned anyway.
		{`{}`, []*Request{{}}, ErrInvalidVersion},

		// A valid notification.
		{`{"jsonrpc":"2.0", "method": "foo", "params":[1, 2, 3]}`, []*Request{{
			method: "foo",
			params: json.RawMessage(`[1, 2, 3]`),
		}}, nil},

		// A valid request, with nil parameters.
		{`{"jsonrpc":"2.0", "method": "foo", "id":10332, "params":null}`, []*Request{{
			id: json.RawMessage("10332"), method: "foo",
		}}, nil},

		// A valid mixed batch.
		{`[ {"jsonrpc": "2.0", "id": 1, "method": "A", "params": {}},
          {"jsonrpc": "2.0", "params": [5], "method": "B"} ]`, []*Request{
			{method: "A", id: json.RawMessage(`1`), params: json.RawMessage(`{}`)},
			{method: "B", params: json.RawMessage(`[5]`)},
		}, nil},

		// An invalid batch.
		{`[{"id": 37, "method": "complain", "params":[]}]`, []*Request{
			{method: "complain", id: json.RawMessage(`37`), params: json.RawMessage(`[]`)},
		}, ErrInvalidVersion},
	}
	for _, test := range tests {
		got, err := ParseRequests([]byte(test.input))
		if err != test.err {
			t.Errorf("ParseRequests(%#q): got error %v, want%v", test.input, err, test.err)
			continue
		}

		if diff := pretty.Compare(got, test.want); diff != "" {
			t.Errorf("ParseRequests(%#q): wrong result (-got +want):\n%s", test.input, diff)
		}
	}
}

type hmap map[string]Handler

func (h hmap) Assign(method string) Handler { return h[method] }
func (h hmap) Names() []string              { return nil }

// Verify that if the client context terminates during a request, the client
// will terminate and report failure.
func TestClientCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan bool, 1)
	cpipe, spipe := channel.Pipe(channel.RawJSON)
	srv := NewServer(hmap{
		"Hang": methodFunc(func(ctx context.Context, _ *Request) (interface{}, error) {
			close(started) // signal that the method handler is running
			defer close(stopped)

			t.Log("Waiting for context completion...")
			select {
			case <-ctx.Done():
				t.Logf("Server context cancelled: err=%v", ctx.Err())
				stopped <- true
				return true, ctx.Err()
			case <-time.After(10 * time.Second):
				return false, nil
			}
		}),
	}, nil).Start(spipe)
	c := NewClient(cpipe, nil)
	defer func() {
		c.Close()
		srv.Wait()
	}()

	// Start a call that will hang around until a timer expires or an explicit
	// cancellation is received.
	ctx, cancel := context.WithCancel(context.Background())
	req, err := c.req(ctx, "Hang", nil)
	if err != nil {
		t.Fatalf("c.req(Hang) failed: %v", err)
	}
	rsps, err := c.send(ctx, jrequests{req})
	if err != nil {
		t.Fatalf("c.send(Hang) failed: %v", err)
	}

	// Wait for the handler to start so that we don't race with calling the
	// handler on the server side, then cancel the context client-side.
	<-started
	cancel()

	// The call should fail client side, in the usual way for a cancellation.
	rsp := rsps[0]
	rsp.wait()
	if err := rsp.Error(); err != nil {
		if err.code != code.Cancelled {
			t.Errorf("Response error for %q: got %v, want %v", rsp.ID(), err, code.Cancelled)
		}
	} else {
		t.Errorf("Response for %q: unexpectedly succeeded", rsp.ID())
	}

	// The server handler should have reported a cancellation.
	if ok := <-stopped; !ok {
		t.Error("Server context was not cancelled")
	}
}

func TestSpecialMethods(t *testing.T) {
	s := NewServer(hmap{
		"rpc.nonesuch": methodFunc(func(context.Context, *Request) (interface{}, error) {
			return "OK", nil
		}),
		"donkeybait": methodFunc(func(context.Context, *Request) (interface{}, error) {
			return true, nil
		}),
	}, nil)
	for _, name := range []string{"rpc.serverInfo", "rpc.cancel", "donkeybait"} {
		if got := s.assign(name); got == nil {
			t.Errorf("s.assign(%s): no method assigned", name)
		}
	}
	if got := s.assign("rpc.nonesuch"); got != nil {
		t.Errorf("s.assign(rpc.nonesuch): got %v, want nil", got)
	}
}

// Verify that the option to remove the special behaviour of rpc.* methods can
// be correctly disabled by the server options.
func TestDisableBuiltin(t *testing.T) {
	s := NewServer(hmap{
		"rpc.nonesuch": methodFunc(func(context.Context, *Request) (interface{}, error) {
			return "OK", nil
		}),
	}, &ServerOptions{DisableBuiltin: true})

	// With builtins disabled, the default rpc.* methods should not get assigned.
	for _, name := range []string{"rpc.serverInfo", "rpc.cancel"} {
		if got := s.assign(name); got != nil {
			t.Errorf("s.assign(%s): got %+v, wanted nil", name, got)
		}
	}

	// However, user-assigned methods with this prefix should now work.
	if got := s.assign("rpc.nonesuch"); got == nil {
		t.Error("s.assign(rpc.nonesuch): missing assignment")
	}
}
