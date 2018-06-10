package jrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/code"
	"bitbucket.org/creachadair/jrpc2/jctx"
)

type testOptions struct {
	server *ServerOptions
	client *ClientOptions
}

func newServer(t *testing.T, assigner Assigner, opts *testOptions) (*Server, *Client, func()) {
	t.Helper()
	if opts == nil {
		opts = new(testOptions)
	}
	cpipe, spipe := channel.Pipe(channel.JSON)
	srv := NewServer(assigner, opts.server).Start(spipe)
	t.Logf("Server running on pipe %+v", spipe)

	cli := NewClient(cpipe, opts.client)
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
		return 0, Errorf(code.InvalidParams, "cannot compute max of no elements")
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
	}, &testOptions{
		server: &ServerOptions{
			AllowV1:     true,
			Concurrency: 16,
		},
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

func TestTimeout(t *testing.T) {
	_, c, cleanup := newServer(t, MapAssigner{
		"Stall": NewMethod(func(ctx context.Context) (bool, error) {
			t.Log("Stalling...")
			select {
			case <-ctx.Done():
				t.Logf("Stall context done: err=%v", ctx.Err())
				return true, nil
			case <-time.After(5 * time.Second):
				return false, errors.New("stall timed out")
			}
		}),
	}, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	got, err := c.CallWait(ctx, "Stall", nil)
	if err == nil {
		t.Errorf("Stall: got %+v, wanted error", got)
	} else if err != context.DeadlineExceeded {
		t.Errorf("Stall: got error %v, want %v", err, context.DeadlineExceeded)
	} else {
		t.Logf("Successfully cancelled after %v", time.Since(start))
	}
}

func TestClientCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan bool, 1)
	_, c, cleanup := newServer(t, MapAssigner{
		"Hang": NewMethod(func(ctx context.Context) (bool, error) {
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
	}, nil)
	defer cleanup()

	// Start a call that will hang around until a timer expires or an explicit
	// cancellation is received.
	ctx, cancel := context.WithCancel(context.Background())
	p, err := c.Call(ctx, "Hang", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Wait for the handler to start so that we don't race with calling the
	// handler on the server side, then cancel the context client-side.
	<-started
	cancel()

	// The call should fail client side, in the usual way for a cancellation.
	rsp := p.Wait()
	if err := rsp.Error(); err != nil {
		if err.Code != code.Cancelled {
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
	if err == nil {
		t.Errorf("CallWait: got %#v, wanted error", got)
	} else if e, ok := err.(*Error); ok {
		t.Logf("Response error is %+v", e)
		if e.Code != errCode {
			t.Errorf("Error code: got %d, want %d", e.Code, errCode)
		}
		if e.Message != errMessage {
			t.Errorf("Error message: got %q, want %q", e.Message, errMessage)
		}
		if s := string(e.data); s != errData {
			t.Errorf("Error data: got %q, want %q", s, errData)
		}
	} else {
		t.Fatalf("CallWait(Err): unexpected error: %v", err)
	}
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want code.Code
	}{
		{nil, code.NoError},                               // no error (success)
		{errors.New("bad"), code.SystemError},             // an unrelated error
		{Errorf(code.ParseError, "bad"), code.ParseError}, // a package error
		{code.InvalidParams, code.InvalidParams},          // a naked code
	}
	for _, test := range tests {
		if got := ErrorCode(test.err); got != test.want {
			t.Errorf("ErrorCode(%v): got %v, want %v", test.err, got, test.want)
		}
	}
}

func TestServerInfo(t *testing.T) {
	s, c, cleanup := newServer(t, MapAssigner{
		"Metricize": NewMethod(func(ctx context.Context) (bool, error) {
			m := ServerMetrics(ctx)
			if m == nil {
				t.Error("Request context does not contain a metrics writer")
				return false, nil
			}
			m.Count("counters-written", 1)
			m.Count("counters-written", 2)

			// Max value trackers are not accumulative.
			m.SetMaxValue("max-metric-value", 1)
			m.SetMaxValue("max-metric-value", 5)
			m.SetMaxValue("max-metric-value", 3)
			m.SetMaxValue("max-metric-value", -30337)

			// Counters are accumulative, and negative deltas subtract.
			m.Count("zero-sum", 0)
			m.Count("zero-sum", 15)
			m.Count("zero-sum", -16)
			m.Count("zero-sum", 1)
			return true, nil
		}),
	}, nil)
	defer cleanup()

	ctx := context.Background()
	if _, err := c.CallWait(ctx, "Metricize", nil); err != nil {
		t.Fatalf("CallWait(Metricize) failed: %v", err)
	}

	info := s.ServerInfo()
	tests := []struct {
		input map[string]int64
		name  string
		want  int64 // use < 0 to test for existence only
	}{
		{info.Counter, "rpc.requests", 1},
		{info.Counter, "counters-written", 3},
		{info.Counter, "zero-sum", 0},
		{info.Counter, "rpc.bytesRead", -1},
		{info.Counter, "rpc.bytesWritten", -1},
		{info.MaxValue, "max-metric-value", 5},
		{info.MaxValue, "rpc.bytesRead", -1},
		{info.MaxValue, "rpc.bytesWritten", -1},
	}
	for _, test := range tests {
		got, ok := test.input[test.name]
		if !ok {
			t.Errorf("Metric %q is not defined, but was expected", test.name)
			continue
		}
		t.Logf("Metric %q is defined with value %d", test.name, got)
		if test.want >= 0 && got != test.want {
			t.Errorf("Wrong value for %q: want %d", test.name, test.want)
		}
	}
}

func TestOtherClient(t *testing.T) {
	// Ensure that a correct request not sent via the *Client type will still
	// elicit a correct response.
	srv, cli := channel.Pipe(channel.Line)
	s := NewServer(MapAssigner{
		"X": NewMethod(func(ctx context.Context) (string, error) {
			return "OK", nil
		}),
	}, nil).Start(srv)
	defer func() {
		cli.Close()
		if err := s.Wait(); err != io.EOF {
			t.Errorf("Server wait: unexpected error %v", err)
		}
	}()

	tests := []struct {
		input, want string
	}{
		// Missing version marker (and therefore wrong).
		{`{"id":0}`,
			`{"jsonrpc":"2.0","id":0,"error":{"code":-32600,"message":"incorrect version marker"}}`},

		// Version marker is present, but wrong.
		{`{"jsonrpc":"1.5","id":1}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"incorrect version marker"}}`},

		// No method was specified.
		{`{"jsonrpc":"2.0","id":2}`,
			`{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"empty method name"}}`},

		// The method specified doesn't exist.
		{`{"jsonrpc":"2.0", "id": 3, "method": "NoneSuch"}`,
			`{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"no such method \"NoneSuch\""}}`},

		// The parameters are of the wrong form.
		{`{"jsonrpc":"2.0", "id": 4, "method": "X", "params": "bogus"}`,
			`{"jsonrpc":"2.0","id":4,"error":{"code":-32600,"message":"parameters must be list or object"}}`},

		// A correct request.
		{`{"jsonrpc":"2.0","id": 5, "method": "X"}`,
			`{"jsonrpc":"2.0","id":5,"result":"OK"}`},
	}
	for _, test := range tests {
		if err := cli.Send([]byte(test.input)); err != nil {
			t.Fatalf("Send %#q failed: %v", test.input, err)
		}
		raw, err := cli.Recv()
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		if got := string(raw); got != test.want {
			t.Errorf("Call %#q: got %#q, want %#q", test.input, got, test.want)
		}
	}
}

func TestServerNotify(t *testing.T) {
	// Set up a server and client with server-side notification support.  Here
	// we're just capturing the name of the notification method, as a sign we
	// got the right thing.
	var notes []string
	s, c, cleanup := newServer(t, MapAssigner{
		"NoteMe": NewMethod(func(ctx context.Context) (bool, error) {
			// When this method is called, it posts a notification back to the
			// client before returning.
			if err := ServerNotify(ctx, "method", nil); err != nil {
				t.Errorf("ServerNotify unexpectedly failed: %v", err)
				return false, err
			}
			return true, nil
		}),
	}, &testOptions{
		server: &ServerOptions{
			AllowNotify: true,
		},
		client: &ClientOptions{
			OnNotify: func(req *Request) {
				notes = append(notes, req.method)
				t.Logf("OnNotify handler saw method %q", req.method)
			},
		},
	})

	ctx := context.Background()

	// Post an explicit notification.
	if err := s.Notify(ctx, "explicit", nil); err != nil {
		t.Errorf("Notify explicit: unexpected error: %v", err)
	}

	// Call the method that posts a notification.
	if _, err := c.CallWait(ctx, "NoteMe", nil); err != nil {
		t.Errorf("Call NoteMe: unexpected error: %v", err)
	}

	// Shut everything down to be sure the callbacks have settled.
	cleanup()

	want := []string{"explicit", "method"}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("Server notifications: got %+q, want %+q", notes, want)
	}
}

func TestContextPlumbing(t *testing.T) {
	want := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()

	_, c, cleanup := newServer(t, MapAssigner{
		"X": NewMethod(func(ctx context.Context) (bool, error) {
			got, ok := ctx.Deadline()
			if !ok {
				return false, errors.New("no deadline was set")
			} else if !got.Equal(want) {
				return false, fmt.Errorf("deadline: got %v, want %v", got, want)
			}
			t.Logf("Got expected deadline: %v", got)
			return true, nil
		}),
	}, &testOptions{
		server: &ServerOptions{DecodeContext: jctx.Decode},
		client: &ClientOptions{EncodeContext: jctx.Encode},
	})
	defer cleanup()

	if _, err := c.CallWait(ctx, "X", nil); err != nil {
		t.Errorf("Call X failed: %v", err)
	}
}

func TestSpecialMethods(t *testing.T) {
	s := NewServer(MapAssigner{
		"rpc.nonesuch": NewMethod(func(context.Context) (string, error) { return "OK", nil }),
		"donkeybait":   NewMethod(func(context.Context) (bool, error) { return true, nil }),
	}, nil)
	for _, name := range []string{"rpc.serverInfo", "rpc.cancel", "donkeybait"} {
		if got := s.assign(name); got == nil {
			t.Errorf("s.assign(%s): no method assigned", name)
		} else {
			t.Logf("s.assign(%s): got %v OK", name, got)
		}
	}
	if got := s.assign("rpc.nonesuch"); got != nil {
		t.Errorf("s.assign(rpc.nonesuch): got %v, want nil", got)
	}
}
