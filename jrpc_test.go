package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
)

type pipeChannel struct {
	dec   *json.Decoder
	enc   *json.Encoder
	close func() error
}

func (p pipeChannel) Send(msg []byte) error { return p.enc.Encode(json.RawMessage(msg)) }

func (p pipeChannel) Recv() ([]byte, error) {
	var msg json.RawMessage
	if err := p.dec.Decode(&msg); err != nil {
		return nil, err
	}
	return []byte(msg), nil
}

func (p pipeChannel) Close() error { return p.close() }

func pipePair() (client, server pipeChannel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client.dec = json.NewDecoder(cr)
	client.enc = json.NewEncoder(cw)
	client.close = func() error {
		cr.Close()
		return cw.Close()
	}
	server.dec = json.NewDecoder(sr)
	server.enc = json.NewEncoder(sw)
	server.close = func() error {
		sr.Close()
		return sw.Close()
	}
	return
}

func newServer(t *testing.T, assigner Assigner, opts *ServerOptions) (*Server, *Client, func()) {
	t.Helper()
	if opts == nil {
		opts = &ServerOptions{LogWriter: os.Stderr}
	}

	cpipe, spipe := pipePair()
	srv := NewServer(assigner, opts).Start(spipe)
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
	s, c, cleanup := newServer(t, ServiceMapper{
		"Test": NewService(dummy{}),
	}, &ServerOptions{
		LogWriter:   os.Stderr,
		AllowV1:     true,
		Concurrency: 16,
	})
	defer cleanup()
	ctx := context.Background()

	// Verify that the assigner got the names it was supposed to.
	if got, want := s.mux.Names(), []string{"Test.Add", "Test.Ctx", "Test.Max", "Test.Mul", "Test.Nil"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names:\ngot  %+q\nwant %+q", got, want)
	}

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
		rsp, err := c.CallWait(ctx, test.method, test.params)
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

		if err := c.Notify(ctx, test.method, test.params); err != nil {
			t.Errorf("Notify %q %v: unexpected error: %v", test.method, test.params, err)
		}
	}

	// Verify that a batch request works.
	specs := make([]Spec, len(tests))
	for i, test := range tests {
		specs[i] = Spec{test.method, test.params}
	}
	batch, err := c.Batch(ctx, specs)
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

	got, err := c.CallWait(context.Background(), "Err", nil)
	if err != nil {
		t.Fatalf("CallWait(Err): unexpected error: %v", err)
	} else if e := got.Error(); e == nil {
		t.Fatalf("CallWait(Err): expected error, got %v", got)
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
