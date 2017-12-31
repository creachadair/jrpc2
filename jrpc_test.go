package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

type pipeConn struct {
	*io.PipeReader
	*io.PipeWriter
}

func (p pipeConn) Close() error {
	rerr := p.PipeReader.Close()
	werr := p.PipeWriter.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

func pipePair() (client, server pipeConn) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	return pipeConn{PipeReader: cr, PipeWriter: cw}, pipeConn{PipeReader: sr, PipeWriter: sw}
}

func newServer(t *testing.T, assigner Assigner, opts *ServerOptions) (*Server, *Client, func()) {
	t.Helper()
	if opts == nil {
		opts = &ServerOptions{LogWriter: os.Stderr}
	}

	cpipe, spipe := pipePair()
	srv, err := NewServer(assigner, opts).Start(spipe)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Logf("Server running on pipe %+v", spipe)

	cli := NewClient(cpipe, &ClientOptions{LogWriter: os.Stderr})
	t.Logf("Client running on pipe %v", cpipe)

	return srv, cli, func() {
		t.Logf("Client close: err=%v", cli.Close())
		srv.Stop()
		t.Logf("Server wait: err=%v", srv.Wait())
	}
}

type dummy struct{}

// Add is a request-based method.
func (dummy) Add(_ context.Context, req *Request) (interface{}, error) {
	if req.IsNotification() {
		return nil, errors.New("ignoring notification")
	}
	var vals []int
	if err := req.UnmarshalParams(&vals); err != nil {
		return nil, err
	}
	var sum int
	for _, v := range vals {
		sum += v
	}
	return sum, nil
}

// Mul uses its own explicit parameter type.
func (dummy) Mul(_ context.Context, req struct{ X, Y int }) (int, error) {
	return req.X * req.Y, nil
}

// Max has a variadic signature.
func (dummy) Max(_ context.Context, vs ...int) (int, error) {
	if len(vs) == 0 {
		return 0, Errorf(E_InvalidParams, "cannot compute max of no elements")
	}
	max := vs[0]
	for _, v := range vs[1:] {
		if v > max {
			max = v
		}
	}
	return max, nil
}

// Nil does not require any parameters.
func (dummy) Nil(_ context.Context) (int, error) { return 42, nil }

// Ctx validates that its context includes the request.
func (dummy) Ctx(ctx context.Context, req *Request) (int, error) {
	if creq := InboundRequest(ctx); creq != req {
		return 0, fmt.Errorf("wrong req in context %p ≠ %p", creq, req)
	}
	return 1, nil
}

// Unrelated should not be picked up by the server.
func (dummy) Unrelated() string { return "ceci n'est pas une méthode" }

func TestClientServer(t *testing.T) {
	_, c, cleanup := newServer(t, ServiceMapper{
		"Test": NewService(dummy{}),
	}, &ServerOptions{
		LogWriter:   os.Stderr,
		AllowV1:     true,
		Concurrency: 16,
	})
	defer cleanup()

	tests := []struct {
		method string
		params interface{}
		want   int
	}{
		{"Test.Add", []int{}, 0},
		{"Test.Add", []int{1, 2, 3}, 6},
		{"Test.Mul", struct{ X, Y int }{7, 9}, 63},
		{"Test.Mul", struct{ X, Y int }{}, 0},
		{"Test.Max", []int{3, 1, 8, 4, 2, 0, -5}, 8},
		{"Test.Ctx", nil, 1},
		{"Test.Nil", nil, 42},
	}

	// Verify that individual sequential requests work.
	for _, test := range tests {
		rsp, err := c.CallWait(test.method, test.params)
		if err != nil {
			t.Errorf("Call %q %v: unexpected error: %v", test.method, test.params, err)
			continue
		}
		var got int
		if err := rsp.UnmarshalResult(&got); err != nil {
			t.Errorf("Unmarshaling result: %v", err)
			continue
		}
		t.Logf("Call %q %v returned %d", test.method, test.params, got)
		if got != test.want {
			t.Errorf("Call %q: got %v, want %v", test.method, got, test.want)
		}

		if err := c.Notify(test.method, test.params); err != nil {
			t.Errorf("Notify %q %v: unexpected error: %v", test.method, test.params, err)
		}
	}

	// Verify that a batch request works.
	specs := make([]Spec, len(tests))
	for i, test := range tests {
		specs[i] = Spec{test.method, test.params}
	}
	batch, err := c.Batch(specs)
	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}
	for i, rsp := range batch.Wait() {
		if err := rsp.Error(); err != nil {
			t.Errorf("Response %d failed: %v", i+1, err)
			continue
		}
		var got int
		if err := rsp.UnmarshalResult(&got); err != nil {
			t.Errorf("Umarshaling result %d: %v", i+1, err)
			continue
		}
		t.Logf("Response %d (%q) contains %d", i+1, rsp.ID(), got)
		if got != tests[i].want {
			t.Errorf("Response %d (%q): got %v, want %v", i+1, rsp.ID(), got, tests[i].want)
		}
	}
}

func TestNewCaller(t *testing.T) {
	// A dummy method that returns the length of its argument slice.
	ass := MapAssigner{
		"F": NewMethod(func(_ context.Context, req []string) (int, error) {
			t.Logf("Call to F with arguments %#v", req)

			// Check for this special form, and generate an error if it matches.
			if len(req) > 0 && req[0] == "fail" {
				return 0, errors.New(strings.Join(req[1:], " "))
			}
			return len(req), nil
		}),
		"OK": NewMethod(func(context.Context) (string, error) {
			t.Log("Call to OK")
			return "OK, hello", nil
		}),
	}

	_, c, cleanup := newServer(t, ass, nil)
	defer cleanup()

	caller := NewCaller("F", []string(nil), int(0))
	F, ok := caller.(func(*Client, []string) (int, error))
	if !ok {
		t.Fatalf("NewCaller (plain): wrong type: %T", caller)
	}
	vcaller := NewCaller("F", string(""), int(0), Variadic())
	V, ok := vcaller.(func(*Client, ...string) (int, error))
	if !ok {
		t.Fatalf("NewCaller (variadic): wrong type: %T", vcaller)
	}
	okcaller := NewCaller("OK", nil, "")
	OK, ok := okcaller.(func(*Client) (string, error))
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
		if got, err := F(c, test.in); err != nil {
			t.Errorf("F(c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("F(c, %q): got %d, want %d", test.in, got, test.want)
		}
		if got, err := V(c, test.in...); err != nil {
			t.Errorf("V(c, %q): unexpected error: %v", test.in, err)
		} else if got != test.want {
			t.Errorf("V(c, %q): got %d, want %d", test.in, got, test.want)
		}
	}

	// Verify that errors get propagated sensibly.
	if got, err := F(c, []string{"fail", "propagate error"}); err == nil {
		t.Errorf("F(c, _): should have failed, returned %d", got)
	} else {
		t.Logf("F(c, _): correctly failed: %v", err)
	}
	if got, err := V(c, "fail", "propagate error"); err == nil {
		t.Errorf("V(c, _): should have failed, returned %d", got)
	} else {
		t.Logf("V(c, _): correctly failed: %v", err)
	}

	// Verify that we can call through a stub without request parameters.
	if m, err := OK(c); err != nil {
		t.Errorf("OK(c): unexpected error: %v", err)
	} else {
		t.Logf("OK(c): returned message %q", m)
	}
}

func TestErrors(t *testing.T) {
	// Test that an error with data attached to it is correctly propagated back
	// from the server to the client, in a value of concrete type *Error.
	const errCode = -32000
	const errData = `{"caroline":452}`
	const errMessage = "error thingy"
	_, c, cleanup := newServer(t, MapAssigner{
		"Err": NewMethod(func(_ context.Context) (int, error) {
			return 17, DataErrorf(errCode, json.RawMessage(errData), errMessage)
		}),
	}, nil)
	defer cleanup()

	Err := NewCaller("Err", nil, int(0)).(func(*Client) (int, error))

	got, err := Err(c)
	if err == nil {
		t.Fatalf("CallWait(Err): expected error, got %d", got)
	} else if e, ok := err.(*Error); !ok {
		t.Fatalf("CallWait(Err): wrong error type %T: %v", err, err)
	} else {
		t.Logf("Response error is %#v", e)
		if e.Code != errCode {
			t.Errorf("Err code: got %d, want %d", e.Code, errCode)
		}
		if e.Message != errMessage {
			t.Errorf("Err message: got %q, want %q", e.Message, errMessage)
		}
		if s := string(e.data); s != errData {
			t.Errorf("Err data: got %q, want %q", s, errData)
		}
	}
}

func TestRegistration(t *testing.T) {
	const message = "fun for the whole family"
	c := RegisterCode(-100, message)
	if got := c.Error(); got != message {
		t.Errorf("RegisterCode(-100): got %q, want %q", got, message)
	} else if c != -100 {
		t.Errorf("RegisterCode(-100): got %d instead", c)
	}
}

func TestRegistrationError(t *testing.T) {
	defer func() {
		if v := recover(); v != nil {
			t.Logf("RegisterCode correctly panicked: %v", v)
		} else {
			t.Fatalf("RegisterCode should have panicked on input %d, but did not", E_ParseError)
		}
	}()
	RegisterCode(int32(E_ParseError), "bogus")
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want Code
	}{
		{nil, E_NoError},                            // no error (success)
		{errors.New("bad"), E_SystemError},          // an unrelated error
		{Errorf(E_ParseError, "bad"), E_ParseError}, // a package error
		{E_InvalidParams, E_InvalidParams},          // a naked code
	}
	for _, test := range tests {
		if got := ErrorCode(test.err); got != test.want {
			t.Errorf("ErrorCode(%v): got %v, want %v", test.err, got, test.want)
		}
	}
}

func TestParseRequest(t *testing.T) {
	req, err := ParseRequest([]byte(`{
  "jsonrpc": "2.0",
  "id":      123,
  "method":  "foo",
  "params":  ["a", "b", "c"]
}`))
	if err != nil {
		t.Errorf("ParseRequest failed: %v", err)
	} else if s := string(req.id); s != "123" {
		t.Errorf("ParseRequest ID: got %q, want 123", s)
	} else if req.method != "foo" {
		t.Errorf("ParseRequest method: got %q, want foo", req.method)
	} else if s, want := string(req.params), `["a", "b", "c"]`; s != want {
		t.Errorf("ParseRequest params: got %q, want %q", s, want)
	}
}

func TestMarshalResponse(t *testing.T) {
	tests := []struct {
		rsp  *Response
		want string
	}{
		{&Response{id: json.RawMessage(`"abc"`), err: E_InvalidParams.ToError()},
			`{"jsonrpc":"2.0","id":"abc","error":{"code":-32602,"message":"invalid parameters"}}`},
		{&Response{id: json.RawMessage("123"), result: json.RawMessage("456")},
			`{"jsonrpc":"2.0","id":123,"result":456}`},
		{&Response{id: json.RawMessage("null"), err: &Error{Code: 11, Message: "bad", data: json.RawMessage(`"horse"`)}},
			`{"jsonrpc":"2.0","id":null,"error":{"code":11,"message":"bad","data":"horse"}}`},
	}
	for _, test := range tests {
		got, err := MarshalResponse(test.rsp)
		if err != nil {
			t.Errorf("MarshalResponse %+v: unexpected error: %v", test.rsp, err)
		} else if s := string(got); s != test.want {
			t.Errorf("MarshalResponse %+v: got %#q, want %#q", test.rsp, s, test.want)
		}
	}
}
