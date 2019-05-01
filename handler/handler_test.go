package handler

import (
	"context"
	"strings"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"github.com/google/go-cmp/cmp"
)

// Verify that the New function correctly handles the various type signatures
// it's advertised to support, and not others.
func TestNew(t *testing.T) {
	tests := []struct {
		v   interface{}
		bad bool
	}{
		{v: nil, bad: true},              // nil value
		{v: "not a function", bad: true}, // not a function

		// All the legal kinds...
		{v: func(context.Context, *jrpc2.Request) (interface{}, error) { return nil, nil }},
		{v: func(context.Context) (int, error) { return 0, nil }},
		{v: func(context.Context, []int) error { return nil }},
		{v: func(context.Context, []bool) (float64, error) { return 0, nil }},
		{v: func(context.Context, ...string) (bool, error) { return false, nil }},
		{v: func(context.Context, *jrpc2.Request) (byte, error) { return '0', nil }},

		// Things that aren't supposed to work.
		{v: func() error { return nil }, bad: true},                           // wrong # of params
		{v: func(a, b, c int) bool { return false }, bad: true},               // ...
		{v: func(byte) {}, bad: true},                                         // wrong # of results
		{v: func(byte) (int, bool, error) { return 0, true, nil }, bad: true}, // ...
		{v: func(string) error { return nil }, bad: true},                     // missing context
		{v: func(context.Context) error { return nil }, bad: true},            // no params, no result
		{v: func(a, b string) error { return nil }, bad: true},                // P1 is not context
		{v: func(context.Context, int) bool { return false }, bad: true},      // R1 is not error
		{v: func(context.Context) (int, bool) { return 1, true }, bad: true},  // R2 is not error
	}
	for _, test := range tests {
		got, err := newHandler(test.v)
		if !test.bad && err != nil {
			t.Errorf("newHandler(%T): unexpected error: %v", test.v, err)
		} else if test.bad && err == nil {
			t.Errorf("newHandler(%T): got %+v, want error", test.v, got)
		}
	}
}

type dummy struct{}

func (dummy) Y1(context.Context) (int, error) { return 0, nil }

func (dummy) N1(string) {}

func (dummy) Y2(_ context.Context, vs ...int) (int, error) { return len(vs), nil }

func (dummy) N2() bool { return false }

//lint:ignore U1000 verify unexported methods are not assigned
func (dummy) n3(context.Context, []string) error { return nil }

// Verify that the NewService function obtains the correct functions.
func TestNewService(t *testing.T) {
	var stub dummy
	m := NewService(stub)
	for _, test := range []string{"Y1", "Y2", "N1", "N2", "n3", "foo"} {
		got := m.Assign(test) != nil
		want := strings.HasPrefix(test, "Y")
		if got != want {
			t.Errorf("Assign %q: got %v, want %v", test, got, want)
		}
	}
}

// Verify that a stub with no usable methods panics.
func TestEmptyService(t *testing.T) {
	type empty struct{}

	defer func() {
		if x := recover(); x != nil {
			t.Logf("Received expected panic: %v", x)
		}
	}()
	m := NewService(empty{})
	t.Fatalf("NewService(empty): got %v, want panic", m)
}

// Verify that a ServiceMap assigns names correctly.
func TestServiceMap(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"nothing", false}, // not a known service
		{"Test", false},    // no method in the service
		{"Test.", false},   // empty method name in service
		{"Test.Y1", true},  // OK
		{"Test.Y2", true},
		{"Test.Y3", false},
		{"Test.N1", false},
	}
	m := ServiceMap{"Test": NewService(dummy{})}
	for _, test := range tests {
		got := m.Assign(test.name) != nil
		if got != test.want {
			t.Errorf("Assign(%q): got %v, want %v", test.name, got, test.want)
		}
	}

	got, want := m.Names(), []string{"Test.Y1", "Test.Y2"} // sorted
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong method names: (-want, +got)\n%s", diff)
	}
}
