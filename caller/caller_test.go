package caller

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

func newServer(t *testing.T, assigner jrpc2.Assigner, opts *jrpc2.ServerOptions) (*jrpc2.Server, *jrpc2.Client, func()) {
	t.Helper()
	if opts == nil {
		opts = &jrpc2.ServerOptions{LogWriter: os.Stderr}
	}

	cpipe, spipe := channel.Pipe()
	srv := jrpc2.NewServer(assigner, opts).Start(spipe)
	t.Logf("Server running on pipe %+v", spipe)

	cli := jrpc2.NewClient(cpipe, &jrpc2.ClientOptions{LogWriter: os.Stderr})
	t.Logf("Client running on pipe %v", cpipe)

	return srv, cli, func() {
		t.Logf("Client close: err=%v", cli.Close())
		srv.Stop()
		t.Logf("Server wait: err=%v", srv.Wait())
	}
}

func TestNewCaller(t *testing.T) {
	// A dummy method that returns the length of its argument slice.
	ass := jrpc2.MapAssigner{
		"F": jrpc2.NewMethod(func(_ context.Context, req []string) (int, error) {
			t.Logf("Call to F with arguments %#v", req)

			// Check for this special form, and generate an error if it matches.
			if len(req) > 0 && req[0] == "fail" {
				return 0, errors.New(strings.Join(req[1:], " "))
			}
			return len(req), nil
		}),
		"OK": jrpc2.NewMethod(func(context.Context) (string, error) {
			t.Log("Call to OK")
			return "OK, hello", nil
		}),
	}

	_, c, cleanup := newServer(t, ass, nil)
	defer cleanup()
	ctx := context.Background()

	caller := New("F", []string(nil), int(0))
	F, ok := caller.(func(context.Context, *jrpc2.Client, []string) (int, error))
	if !ok {
		t.Fatalf("NewCaller (plain): wrong type: %T", caller)
	}
	vcaller := New("F", string(""), int(0), Variadic())
	V, ok := vcaller.(func(context.Context, *jrpc2.Client, ...string) (int, error))
	if !ok {
		t.Fatalf("NewCaller (variadic): wrong type: %T", vcaller)
	}
	okcaller := New("OK", nil, "")
	OK, ok := okcaller.(func(context.Context, *jrpc2.Client) (string, error))
	if !ok {
		t.Fatalf("NewCaller (niladic): wrong type: %T", okcaller)
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
		if got, err := F(ctx, c, test.in); err != nil {
			t.Errorf("F(_, c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("F(_, c, %q): got %d, want %d", test.in, got, test.want)
		}
		if got, err := V(ctx, c, test.in...); err != nil {
			t.Errorf("V(_, c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("V(_, c, %q): got %d, want %d", test.in, got, test.want)
		}
	}

	// Verify that errors get propagated sensibly.
	if got, err := F(ctx, c, []string{"fail", "propagate error"}); err == nil {
		t.Errorf("F(_, c, _): should have failed, returned %d", got)
	} else {
		t.Logf("F(_, c, _): correctly failed: %v", err)
	}
	if got, err := V(ctx, c, "fail", "propagate error"); err == nil {
		t.Errorf("V(_, c, _): should have failed, returned %d", got)
	} else {
		t.Logf("V(_, c, _): correctly failed: %v", err)
	}

	// Verify that we can call through a stub without request parameters.
	if m, err := OK(ctx, c); err != nil {
		t.Errorf("OK(_, c): unexpected error: %v", err)
	} else {
		t.Logf("OK(_, c): returned message %q", m)
	}

	// Verify that we can list the methods via the server hook.
	info, err := RPC_serverInfo(ctx, c)
	if err != nil {
		t.Errorf("rpc.serverInfo: unexpected error: %v", err)
	} else if want := []string{"F", "OK"}; !reflect.DeepEqual(info.Methods, want) {
		t.Errorf("rpc.serverInfo: got %+v, want %+q", info, want)
	}
}
