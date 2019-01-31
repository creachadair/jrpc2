package caller

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/server"
)

func newAssigner(t *testing.T) jrpc2.Assigner {
	return handler.Map{
		// A dummy method that returns the length of its argument slice.
		"F": handler.New(func(_ context.Context, req []string) (int, error) {
			t.Logf("Call to F with arguments %#v", req)

			// Check for this special form, and generate an error if it matches.
			if len(req) > 0 && req[0] == "fail" {
				return 0, errors.New(strings.Join(req[1:], " "))
			}
			return len(req), nil
		}),
		// A method that returns a fixed string.
		"OK": handler.New(func(context.Context) (string, error) {
			t.Log("Call to OK")
			return "OK, hello", nil
		}),
		// A method that returns an error only, no data value.
		"ErrOnly": handler.New(func(_ context.Context, req []string) error {
			if len(req) != 0 {
				return jrpc2.Errorf(1, req[0])
			}
			return nil
		}),
		// A method that should only ever be called as a notification.  It
		// generates a test error if it is sent a call expecting a reply.
		"Note": handler.New(func(_ context.Context, req *jrpc2.Request) error {
			if !req.IsNotification() {
				t.Errorf("Note called expecting a reply: %+v", req)
				return errors.New("bad")
			}
			t.Logf("Note notified (OK): %+v", req)
			return nil
		}),
	}
}

func TestNew(t *testing.T) {
	loc := server.NewLocal(newAssigner(t), nil)
	defer loc.Close()
	ctx := context.Background()

	caller := New("F", Options{Params: []string(nil), Result: int(0)})
	F, ok := caller.(func(context.Context, *jrpc2.Client, []string) (int, error))
	if !ok {
		t.Fatalf("New (plain): wrong type: %T", caller)
	}
	vcaller := New("F", Options{
		Params:   "",
		Result:   0,
		Variadic: true,
	})
	V, ok := vcaller.(func(context.Context, *jrpc2.Client, ...string) (int, error))
	if !ok {
		t.Fatalf("New (variadic): wrong type: %T", vcaller)
	}

	// Verify that various success cases do indeed.
	tests := []struct {
		in   []string
		want int
	}{
		{nil, 0}, // nil should behave like an empty slice
		{[]string{}, 0},
		{[]string{"a"}, 1},
		{[]string{"a", "b", "c"}, 3},
		{[]string{"", "", "q"}, 3},
	}
	for _, test := range tests {
		if got, err := F(ctx, loc.Client, test.in); err != nil {
			t.Errorf("F(_, c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("F(_, c, %q): got %d, want %d", test.in, got, test.want)
		}
		if got, err := V(ctx, loc.Client, test.in...); err != nil {
			t.Errorf("V(_, c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("V(_, c, %q): got %d, want %d", test.in, got, test.want)
		}
	}

	// Verify that errors get propagated sensibly.
	t.Run("PropagateErrors", func(t *testing.T) {
		if got, err := F(ctx, loc.Client, []string{"fail", "propagate error"}); err == nil {
			t.Errorf("F(_, c, _): should have failed, returned %d", got)
		} else {
			t.Logf("F(_, c, _): correctly failed: %v", err)
		}
		if got, err := V(ctx, loc.Client, "fail", "propagate error"); err == nil {
			t.Errorf("V(_, c, _): should have failed, returned %d", got)
		} else {
			t.Logf("V(_, c, _): correctly failed: %v", err)
		}
	})

	type tester func(context.Context, *jrpc2.Client, *testing.T)

	for _, fn := range []tester{
		testOmitParams, testOmitResult, testNotification, testRPCServerInfo,
	} {
		fn(ctx, loc.Client, t)
	}
}

func testOmitParams(ctx context.Context, c *jrpc2.Client, t *testing.T) {
	// Verify that we can call through a stub without request parameters.
	t.Run("OmitParams", func(t *testing.T) {
		okcaller := New("OK", Options{Result: ""})
		OK, ok := okcaller.(func(context.Context, *jrpc2.Client) (string, error))
		if !ok {
			t.Fatalf("New (niladic): wrong type: %T", okcaller)
		}
		if m, err := OK(ctx, c); err != nil {
			t.Errorf("OK(_, c): unexpected error: %v", err)
		} else {
			t.Logf("OK(_, c): returned message %q", m)
		}
	})
}

func testOmitResult(ctx context.Context, c *jrpc2.Client, t *testing.T) {
	// Verify that we can call through a stub without a result value.
	t.Run("OmitResult", func(t *testing.T) {
		errcaller := New("ErrOnly", Options{Params: []string(nil)})
		E, ok := errcaller.(func(context.Context, *jrpc2.Client, []string) error)
		if !ok {
			t.Fatalf("New (no-result): wrong type: %T", errcaller)
		}

		const message = "cork bat"
		if err := E(ctx, c, []string{message}); err == nil {
			t.Errorf("E(_, c, %q): unexpected success", message)
		} else if e, ok := err.(*jrpc2.Error); !ok || e.Message() != message {
			t.Errorf("E(_, c, %q): got error (%T) %#v, wanted message %q", message, err, err, message)
		} else {
			t.Logf("E(_, c, %q): got expected error %#v", message, e)
		}
	})
}

func testNotification(ctx context.Context, c *jrpc2.Client, t *testing.T) {
	// Verify that a stub flagged for notification actually sends a
	// notification instead of a regular call.
	t.Run("Notification", func(t *testing.T) {
		notecaller := New("Note", Options{Params: []string(nil), Notify: true})
		N, ok := notecaller.(func(context.Context, *jrpc2.Client, []string) error)
		if !ok {
			t.Fatalf("New (notify): wrong type: %T", notecaller)
		}

		if err := N(ctx, c, []string{"hello"}); err != nil {
			t.Errorf("N(_, c, hello): unexpected error: %v", err)
		}
	})
}

func testRPCServerInfo(ctx context.Context, c *jrpc2.Client, t *testing.T) {
	// Verify that we can list the methods via the server hook.
	t.Run("RPCServerInfo", func(t *testing.T) {
		info, err := RPCServerInfo(ctx, c)
		if err != nil {
			t.Fatalf("rpc.serverInfo: unexpected error: %v", err)
		}
		want := []string{"ErrOnly", "F", "Note", "OK"}
		if !reflect.DeepEqual(info.Methods, want) {
			t.Errorf("rpc.serverInfo: got %+v, want %+q", info, want)
		}
	})
}
