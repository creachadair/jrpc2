// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func y1(context.Context) (int, error) { return 0, nil }

func y2(_ context.Context, vs []int) (int, error) { return len(vs), nil }

func y3(context.Context) error { return errors.New("blah") }

type argStruct struct {
	A string `json:"alpha"`
	B int    `json:"bravo"`
}

// Verify that the Check function correctly handles the various type signatures
// it's advertised to support, and not others.
func TestCheck(t *testing.T) {
	tests := []struct {
		v   any
		bad bool
	}{
		{v: nil, bad: true},              // nil value
		{v: "not a function", bad: true}, // not a function

		// All the legal kinds...
		{v: func(context.Context) error { return nil }},
		{v: func(context.Context, *jrpc2.Request) (any, error) { return nil, nil }},
		{v: func(context.Context) (int, error) { return 0, nil }},
		{v: func(context.Context, []int) error { return nil }},
		{v: func(context.Context, []bool) (float64, error) { return 0, nil }},
		{v: func(context.Context, *argStruct) int { return 0 }},
		{v: func(context.Context, *jrpc2.Request) error { return nil }},
		{v: func(context.Context, *jrpc2.Request) float64 { return 0 }},
		{v: func(context.Context, *jrpc2.Request) (byte, error) { return '0', nil }},
		{v: func(context.Context) bool { return true }},
		{v: func(context.Context, int) bool { return true }},
		{v: func(_ context.Context, s [1]string) string { return s[0] }},

		// Things that aren't supposed to work.
		{v: func() error { return nil }, bad: true},                           // wrong # of params
		{v: func(a, b, c int) bool { return false }, bad: true},               // ...
		{v: func(byte) {}, bad: true},                                         // wrong # of results
		{v: func(byte) (int, bool, error) { return 0, true, nil }, bad: true}, // ...
		{v: func(string) error { return nil }, bad: true},                     // missing context
		{v: func(a, b string) error { return nil }, bad: true},                // P1 is not context
		{v: func(context.Context) (int, bool) { return 1, true }, bad: true},  // R2 is not error

		//lint:ignore ST1008 verify permuted error position does not match
		{v: func(context.Context) (error, float64) { return nil, 0 }, bad: true}, // ...
	}
	for _, test := range tests {
		got, err := handler.Check(test.v)
		if !test.bad && err != nil {
			t.Errorf("Check(%T): unexpected error: %v", test.v, err)
		} else if test.bad && err == nil {
			t.Errorf("Check(%T): got %+v, want error", test.v, got)
		}
	}
}

// Verify that the wrappers constructed by FuncInfo.Wrap can properly decode
// their arguments of different types and structure.
func TestFuncInfo_wrapDecode(t *testing.T) {
	tests := []struct {
		fn   jrpc2.Handler
		p    string
		want any
	}{
		// A positional handler should decode its argument from an array or an object.
		{handler.NewPos(func(_ context.Context, z int) int { return z }, "arg"),
			`[25]`, 25},
		{handler.NewPos(func(_ context.Context, z int) int { return z }, "arg"),
			`{"arg":109}`, 109},

		// A type with custom marshaling should be properly handled.
		{handler.NewPos(func(_ context.Context, b stringByte) byte { return byte(b) }, "arg"),
			`["00111010"]`, byte(0x3a)},
		{handler.NewPos(func(_ context.Context, b stringByte) byte { return byte(b) }, "arg"),
			`{"arg":"10011100"}`, byte(0x9c)},
		{handler.New(func(_ context.Context, v fauxStruct) int { return int(v) }),
			`{"type":"thing","value":99}`, 99},

		// Plain JSON should get its argument unmodified.
		{handler.New(func(_ context.Context, v json.RawMessage) string { return string(v) }),
			`{"x": true, "y": null}`, `{"x": true, "y": null}`},

		// Npn-positional slice argument.
		{handler.New(func(_ context.Context, ss []string) int { return len(ss) }),
			`["a", "b", "c"]`, 3},
	}
	ctx := context.Background()
	for _, test := range tests {
		req := testutil.MustParseRequest(t,
			fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"x","params":%s}`, test.p))
		got, err := test.fn(ctx, req)
		if err != nil {
			t.Errorf("Call %p failed: %v", test.fn, err)
		} else if diff := cmp.Diff(test.want, got); diff != "" {
			t.Errorf("Call %p: wrong result (-want, +got)\n%s", test.fn, diff)
		}
	}
}

// Verify that the Positional function correctly handles its cases.
func TestPositional(t *testing.T) {
	tests := []struct {
		v   any
		n   []string
		bad bool
	}{
		{v: nil, bad: true},              // nil value
		{v: "not a function", bad: true}, // not a function

		// Things that should work.
		{v: func(context.Context) error { return nil }},
		{v: func(context.Context) int { return 1 }},
		{v: func(context.Context, bool) bool { return false },
			n: []string{"isTrue"}},
		{v: func(context.Context, int, int) int { return 0 },
			n: []string{"a", "b"}},
		{v: func(context.Context, string, int, []float64) int { return 0 },
			n: []string{"a", "b", "c"}},

		// Things that should not work.
		{v: func() error { return nil }, bad: true}, // no parameters
		{v: func(int) int { return 0 }, bad: true},  // first argument not context
		{v: func(context.Context, string) error { return nil },
			n: nil, bad: true}, // not enough names
		{v: func(context.Context, string, string, string) error { return nil },
			n: []string{"x", "y"}, bad: true}, // too many names
		{v: func(context.Context, string, ...float64) int { return 0 },
			n: []string{"goHome", "youAreDrunk"}, bad: true}, // variadic

		// N.B. Other cases are covered by TestCheck. The cases here are only
		// those that Positional checks for explicitly.
	}
	for _, test := range tests {
		got, err := handler.Positional(test.v, test.n...)
		if !test.bad && err != nil {
			t.Errorf("Positional(%T, %q): unexpected error: %v", test.v, test.n, err)
		} else if test.bad && err == nil {
			t.Errorf("Positional(%T, %q): got %+v, want error", test.v, test.n, got)
		}
	}
}

// Verify that the Check function correctly handles struct names.
func TestCheck_structArg(t *testing.T) {
	type args struct {
		A    string `json:"apple"`
		B    int    `json:"-"`
		C    bool   `json:",omitempty"`
		D    byte   // unspecified, use default name
		Evil int    `json:"eee"`
	}

	const base = `{"jsonrpc":"2.0","id":1,"method":"M","params":%s}`
	const inputObj = `{"apple":"1","c":true,"d":25,"eee":666}`
	const inputArray = `["1", true, 25, 666]`
	fail := errors.New("fail")

	// Each of these cases has a valid struct argument type.  Call each wrapper
	// with the same arguments in object an array format, and verify that the
	// expected result or error are reported.
	tests := []struct {
		name string
		v    any
		want any
		err  error
	}{
		// Things that should work.
		{name: "non-pointer returns string",
			v: func(_ context.Context, x args) string { return x.A }, want: "1"},
		{name: "pointer returns bool",
			v: func(_ context.Context, x *args) bool { return x.C }, want: true},
		{name: "non-pointer returns int",
			v: func(_ context.Context, x args) int { return x.Evil }, want: 666},
		{name: "pointer returns bool",
			v: func(_ context.Context, x *args) (bool, error) { return true, nil }, want: true},
		{name: "non-pointer reports error",
			v: func(context.Context, args) (int, error) { return 0, fail }, err: fail},
		{name: "pointer reports error",
			v: func(context.Context, *args) error { return fail }, err: fail},

		// N.B. Other cases are covered by TestCheck. The cases here are only
		// those that Struct checks for explicitly.
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fi, err := handler.Check(test.v)
			if err != nil {
				t.Fatalf("Check failed for %T: %v", test.v, err)
			}
			fn := fi.Wrap()

			for _, sub := range []struct {
				name string
				req  *jrpc2.Request
			}{
				{"Object", testutil.MustParseRequest(t, fmt.Sprintf(base, inputObj))},
				{"Array", testutil.MustParseRequest(t, fmt.Sprintf(base, inputArray))},
			} {
				t.Run(sub.name, func(t *testing.T) {

					rsp, err := fn(context.Background(), sub.req)
					if err != test.err {
						t.Errorf("Got error %v, want %v", err, test.err)
					}
					if rsp != test.want {
						t.Errorf("Got value %v, want %v", rsp, test.want)
					}
					if t.Failed() {
						t.Logf("Parameters: %#q", sub.req.ParamString())
					}
				})
			}
		})
	}
}

func TestFuncInfo_SetStrict(t *testing.T) {
	type arg struct {
		A, B string
	}
	fi, err := handler.Check(func(ctx context.Context, arg *arg) error { return nil })
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	fn := fi.SetStrict(true).Wrap()

	req := testutil.MustParseRequest(t, `{
   "jsonrpc": "2.0",
   "id":      100,
   "method":  "f",
   "params": {
      "A": "foo",
      "Z": 25
   }}`)
	rsp, err := fn(context.Background(), req)
	if got := jrpc2.ErrorCode(err); got != jrpc2.InvalidParams {
		t.Errorf("Handler returned (%+v, %v), want InvalidParms", rsp, err)
	}
}

func TestFuncInfo_AllowArray(t *testing.T) {
	type arg struct {
		A, B string
	}
	fi, err := handler.Check(func(ctx context.Context, arg *arg) error {
		if arg.A != "x" || arg.B != "y" {
			return fmt.Errorf("a=%q, b=%q", arg.A, arg.B)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	req := testutil.MustParseRequest(t, `{
   "jsonrpc": "2.0",
   "id":      101,
   "method":  "f",
   "params":  ["x", "y"]
   }`)

	t.Run("true", func(t *testing.T) {
		fn := fi.AllowArray(true).Wrap()
		if _, err := fn(context.Background(), req); err != nil {
			t.Fatalf("Handler unexpectedly failed: %v", err)
		}
	})
	t.Run("false", func(t *testing.T) {
		fn := fi.AllowArray(false).Wrap()
		rsp, err := fn(context.Background(), req)
		if got := jrpc2.ErrorCode(err); got != jrpc2.InvalidParams {
			t.Errorf("Handler returned (%+v, %v), want InvalidParams", rsp, err)
		}
	})
}

// Verify that the handling of pointer-typed arguments does not incorrectly
// introduce another pointer indirection.
func TestNew_pointerRegression(t *testing.T) {
	var got argStruct
	method := handler.New(func(_ context.Context, arg *argStruct) error {
		got = *arg
		t.Logf("Got argument struct: %+v", got)
		return nil
	})
	req := testutil.MustParseRequest(t, `{
   "jsonrpc": "2.0",
   "id":      "foo",
   "method":  "bar",
   "params":{
      "alpha": "xyzzy",
      "bravo": 23
   }}`)
	if _, err := method(context.Background(), req); err != nil {
		t.Errorf("Handler failed: %v", err)
	}
	want := argStruct{A: "xyzzy", B: 23}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong argStruct value: (-want, +got)\n%s", diff)
	}
}

// Verify that positional arguments are decoded properly.
func TestPositional_decode(t *testing.T) {
	fi, err := handler.Positional(func(ctx context.Context, a, b int) int {
		return a + b
	}, "first", "second")
	if err != nil {
		t.Fatalf("Positional: unexpected error: %v", err)
	}
	call := fi.Wrap()
	tests := []struct {
		input string
		want  int
		bad   bool
	}{
		{`{"jsonrpc":"2.0","id":1,"method":"add","params":{"first":5,"second":3}}`, 8, false},
		{`{"jsonrpc":"2.0","id":2,"method":"add","params":[5,3]}`, 8, false},
		{`{"jsonrpc":"2.0","id":3,"method":"add","params":{"first":5}}`, 5, false},
		{`{"jsonrpc":"2.0","id":4,"method":"add","params":{"second":3}}`, 3, false},
		{`{"jsonrpc":"2.0","id":5,"method":"add","params":{}}`, 0, false},
		{`{"jsonrpc":"2.0","id":6,"method":"add","params":null}`, 0, false},
		{`{"jsonrpc":"2.0","id":7,"method":"add"}`, 0, false},

		{`{"jsonrpc":"2.0","id":10,"method":"add","params":["wrong", "type"]}`, 0, true},
		{`{"jsonrpc":"2.0","id":12,"method":"add","params":[15, "wrong-type"]}`, 0, true},
		{`{"jsonrpc":"2.0","id":13,"method":"add","params":{"unknown":"field"}}`, 0, true},
		{`{"jsonrpc":"2.0","id":14,"method":"add","params":[1]}`, 0, true},     // too few
		{`{"jsonrpc":"2.0","id":15,"method":"add","params":[1,2,3]}`, 0, true}, // too many
	}
	for _, test := range tests {
		req := testutil.MustParseRequest(t, test.input)
		got, err := call(context.Background(), req)
		if !test.bad {
			if err != nil {
				t.Errorf("Call %#q: unexpected error: %v", test.input, err)
			} else if z := got.(int); z != test.want {
				t.Errorf("Call %#q: got %d, want %d", test.input, z, test.want)
			}
		} else if test.bad && err == nil {
			t.Errorf("Call %#q: got %v, want error", test.input, got)
		}
	}
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
		{"Test.Y3", true},
		{"Test.Y4", false},
		{"Test.N1", false},
		{"Test.N2", false},
	}
	ctx := context.Background()
	m := handler.ServiceMap{"Test": handler.Map{
		"Y1": handler.New(y1),
		"Y2": handler.New(y2),
		"Y3": handler.New(y3),
	}}
	for _, test := range tests {
		got := m.Assign(ctx, test.name) != nil
		if got != test.want {
			t.Errorf("Assign(%q): got %v, want %v", test.name, got, test.want)
		}
	}

	got, want := m.Names(), []string{"Test.Y1", "Test.Y2", "Test.Y3"} // sorted
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong method names: (-want, +got)\n%s", diff)
	}
}

// Verify that argument decoding works.
func TestArgs(t *testing.T) {
	type stuff struct {
		S string
		Z int
		F float64
		B bool
	}
	var tmp stuff
	tests := []struct {
		json string
		args handler.Args
		want stuff
		ok   bool
	}{
		{``, nil, stuff{}, false},     // incomplete
		{`{}`, nil, stuff{}, false},   // wrong type (object)
		{`true`, nil, stuff{}, false}, // wrong type (bool)

		{`[]`, nil, stuff{}, true},
		{`[ ]`, nil, stuff{}, true},
		{`null`, nil, stuff{}, true},

		// Respect order of arguments and values.
		{`["foo", 25]`, handler.Args{&tmp.S, &tmp.Z}, stuff{S: "foo", Z: 25}, true},
		{`[25, "foo"]`, handler.Args{&tmp.Z, &tmp.S}, stuff{S: "foo", Z: 25}, true},

		{`[true, 3.5, "blah"]`, handler.Args{&tmp.B, &tmp.F, &tmp.S},
			stuff{S: "blah", B: true, F: 3.5}, true},

		// Skip values with a nil corresponding argument.
		{`[true, 101, "ignored"]`, handler.Args{&tmp.B, &tmp.Z, nil},
			stuff{B: true, Z: 101}, true},
		{`[true, 101, "observed"]`, handler.Args{&tmp.B, nil, &tmp.S},
			stuff{B: true, S: "observed"}, true},

		// Mismatched argument/value count.
		{`["wrong"]`, handler.Args{&tmp.S, &tmp.Z}, stuff{}, false},   // too few values
		{`["really", "wrong"]`, handler.Args{&tmp.S}, stuff{}, false}, // too many values

		// Mismatched argument/value types.
		{`["nope"]`, handler.Args{&tmp.B}, stuff{}, false}, // wrong value type
		{`[{}]`, handler.Args{&tmp.F}, stuff{}, false},     // "
	}
	for _, test := range tests {
		tmp = stuff{} // reset
		if err := json.Unmarshal([]byte(test.json), &test.args); err != nil {
			if test.ok {
				t.Errorf("Unmarshal %#q: unexpected error: %v", test.json, err)
			}
			continue
		}

		if diff := cmp.Diff(test.want, tmp); diff != "" {
			t.Errorf("Unmarshal %#q: (-want, +got)\n%s", test.json, diff)
		}
	}
}

func TestArgsMarshal(t *testing.T) {
	tests := []struct {
		input []any
		want  string
	}{
		{nil, "[]"},
		{[]any{}, "[]"},
		{[]any{12345}, "[12345]"},
		{[]any{"hey you"}, `["hey you"]`},
		{[]any{true, false}, "[true,false]"},
		{[]any{nil, 3.5}, "[null,3.5]"},
		{[]any{[]string{"a", "b"}, 33}, `[["a","b"],33]`},
		{[]any{1, map[string]string{
			"ok": "yes",
		}, 3}, `[1,{"ok":"yes"},3]`},
	}
	for _, test := range tests {
		got, err := json.Marshal(handler.Args(test.input))
		if err != nil {
			t.Errorf("Marshal %+v: unexpected error: %v", test.input, err)
		} else if s := string(got); s != test.want {
			t.Errorf("Marshal %+v: got %#q, want %#q", test.input, s, test.want)
		}
	}
}

func TestObjUnmarshal(t *testing.T) {
	// N.B. Exported field names here to satisfy cmp.Diff.
	type sub struct {
		Foo string `json:"foo"`
	}
	type values struct {
		Z int
		S string
		T sub
		L []int
	}
	var v values

	tests := []struct {
		input string
		obj   handler.Obj
		want  *values
	}{
		{"", nil, nil},     // error: empty text
		{"true", nil, nil}, // error: not an object
		{"[]", nil, nil},   // error: not an object
		{`{"x":true}`, handler.Obj{"x": &v.S}, nil}, // error: wrong type

		// Nothing to unpack, no place to put it.
		{"{}", nil, &values{}},

		// Ignore non-matching keys but keep matching ones.
		{`{"apple":true, "laser":"sauce"}`, handler.Obj{"laser": &v.S}, &values{S: "sauce"}},

		// Assign to matching fields including compound types.
		{`{"x": 25, "q": "snark", "sub": {"foo":"bark"}, "yawp": false, "#":[5,3,2,4,7]}`, handler.Obj{
			"x":   &v.Z,
			"q":   &v.S,
			"sub": &v.T,
			"#":   &v.L,
		}, &values{
			Z: 25,
			S: "snark",
			T: sub{Foo: "bark"},
			L: []int{5, 3, 2, 4, 7},
		}},
	}
	for _, test := range tests {
		v = values{} // reset

		if err := json.Unmarshal([]byte(test.input), &test.obj); err != nil {
			if test.want == nil {
				t.Logf("Unmarshal: got expected error: %v", err)
			} else {
				t.Errorf("Unmarshal %q: %v", test.input, err)
			}
			continue
		}
		if diff := cmp.Diff(*test.want, v); diff != "" {
			t.Errorf("Wrong values: (-want, +got)\n%s", diff)
		}
	}
}

// stringByte is a byte with a custom JSON encoding. It expects a string of
// decimal digits 1 and 0, e.g., "10011000" == 0x98.
type stringByte byte

func (s *stringByte) UnmarshalText(text []byte) error {
	v, err := strconv.ParseUint(string(text), 2, 8)
	if err != nil {
		return err
	}
	*s = stringByte(v)
	return nil
}

// fauxStruct is an integer with a custom JSON encoding. It expects an object:
//
//	{"type":"thing","value":<integer>}
type fauxStruct int

func (s *fauxStruct) UnmarshalJSON(data []byte) error {
	var tmp struct {
		T string `json:"type"`
		V *int   `json:"value"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	} else if tmp.T != "thing" {
		return fmt.Errorf("unknown type %q", tmp.T)
	} else if tmp.V == nil {
		return errors.New("missing value")
	}
	*s = fauxStruct(*tmp.V)
	return nil
}
