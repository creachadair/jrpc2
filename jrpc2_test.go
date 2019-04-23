package jrpc2_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/code"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/jctx"
	"bitbucket.org/creachadair/jrpc2/server"
)

var notAuthorized = code.Register(-32095, "request not authorized")

type dummy struct{}

// Add is a request-based method.
func (dummy) Add(_ context.Context, req *jrpc2.Request) (interface{}, error) {
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
		return 0, jrpc2.Errorf(code.InvalidParams, "cannot compute max of no elements")
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
func (dummy) Ctx(ctx context.Context, req *jrpc2.Request) (int, error) {
	if creq := jrpc2.InboundRequest(ctx); creq != req {
		return 0, fmt.Errorf("wrong req in context %p ≠ %p", creq, req)
	}
	return 1, nil
}

// Unrelated should not be picked up by the server.
func (dummy) Unrelated() string { return "ceci n'est pas une méthode" }

var callTests = []struct {
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
	{"Test.Nil", json.RawMessage("null"), 42},
}

func TestMethodNames(t *testing.T) {
	loc := server.NewLocal(handler.ServiceMap{
		"Test": handler.NewService(dummy{}),
	}, nil)
	defer loc.Close()
	s := loc.Server

	// Verify that the assigner got the names it was supposed to.
	got, want := s.ServerInfo().Methods, []string{"Test.Add", "Test.Ctx", "Test.Max", "Test.Mul", "Test.Nil"}
	if diff := pretty.Compare(got, want); diff != "" {
		t.Errorf("Wrong method names: (-got, +want)\n%s", diff)
	}
}

func TestCall(t *testing.T) {
	loc := server.NewLocal(handler.ServiceMap{
		"Test": handler.NewService(dummy{}),
	}, &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{
			AllowV1:     true,
			Concurrency: 16,
		},
	})
	defer loc.Close()
	c := loc.Client
	ctx := context.Background()

	// Verify that individual sequential requests work.
	for _, test := range callTests {
		rsp, err := c.Call(ctx, test.method, test.params)
		if err != nil {
			t.Errorf("Call %q %v: unexpected error: %v", test.method, test.params, err)
			continue
		}
		var got int
		if err := rsp.UnmarshalResult(&got); err != nil {
			t.Errorf("Unmarshaling result: %v", err)
			continue
		}
		if got != test.want {
			t.Errorf("Call %q %v: got %v, want %v", test.method, test.params, got, test.want)
		}
		if err := c.Notify(ctx, test.method, test.params); err != nil {
			t.Errorf("Notify %q %v: unexpected error: %v", test.method, test.params, err)
		}
	}
}

func TestCallResult(t *testing.T) {
	loc := server.NewLocal(handler.ServiceMap{
		"Test": handler.NewService(dummy{}),
	}, &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{Concurrency: 16},
	})
	defer loc.Close()
	c := loc.Client
	ctx := context.Background()

	// Verify also that the CallResult wrapper works.
	for _, test := range callTests {
		var got int
		if err := c.CallResult(ctx, test.method, test.params, &got); err != nil {
			t.Errorf("CallResult %q %v: unexpected error: %v", test.method, test.params, err)
			continue
		}
		if got != test.want {
			t.Errorf("CallResult %q %v: got %v, want %v", test.method, test.params, got, test.want)
		}
	}
}

func TestBatch(t *testing.T) {
	loc := server.NewLocal(handler.ServiceMap{
		"Test": handler.NewService(dummy{}),
	}, &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{
			AllowV1:     true,
			Concurrency: 16,
		},
	})
	defer loc.Close()
	c := loc.Client
	ctx := context.Background()

	// Verify that a batch request works.
	specs := make([]jrpc2.Spec, len(callTests))
	for i, test := range callTests {
		specs[i] = jrpc2.Spec{
			Method: test.method,
			Params: test.params,
			Notify: false,
		}
	}
	batch, err := c.Batch(ctx, specs)
	if err != nil {
		t.Fatalf("Batch failed: %v", err)
	}
	for i, rsp := range batch {
		if err := rsp.Error(); err != nil {
			t.Errorf("Response %d failed: %v", i+1, err)
			continue
		}
		var got int
		if err := rsp.UnmarshalResult(&got); err != nil {
			t.Errorf("Umarshaling result %d: %v", i+1, err)
			continue
		}
		if got != callTests[i].want {
			t.Errorf("Response %d (%q): got %v, want %v", i+1, rsp.ID(), got, callTests[i].want)
		}
	}
}

// Verify that a method that returns only an error (no result payload) is set
// up and handled correctly.
func TestErrorOnly(t *testing.T) {
	const errMessage = "not enough strings"
	loc := server.NewLocal(handler.Map{
		"ErrorOnly": handler.New(func(_ context.Context, ss []string) error {
			if len(ss) == 0 {
				return jrpc2.Errorf(1, errMessage)
			}
			t.Logf("ErrorOnly succeeds on input %q", ss)
			return nil
		}),
	}, nil)
	defer loc.Close()
	c := loc.Client
	ctx := context.Background()

	t.Run("CallExpectingError", func(t *testing.T) {
		rsp, err := c.Call(ctx, "ErrorOnly", []string{})
		if err == nil {
			t.Errorf("ErrorOnly: got %+v, want error", rsp)
		} else if e, ok := err.(*jrpc2.Error); !ok {
			t.Errorf("ErrorOnly: got %v, want *Error", err)
		} else if e.Code() != 1 || e.Message() != errMessage {
			t.Errorf("ErrorOnly: got (%s, %s), want (1, %s)", e.Code(), e.Message(), errMessage)
		} else {
			var data json.RawMessage
			if err, want := e.UnmarshalData(&data), jrpc2.ErrNoData; err != want {
				t.Errorf("UnmarshalData: got %#q, %v, want %v", string(data), err, want)
			}
		}
	})
	t.Run("CallExpectingOK", func(t *testing.T) {
		rsp, err := c.Call(ctx, "ErrorOnly", []string{"aiutami!"})
		if err != nil {
			t.Errorf("ErrorOnly: unexpected error: %v", err)
		}
		// Per https://www.jsonrpc.org/specification#response_object, a "result"
		// field is required on success, so verify that it is set null.
		var got json.RawMessage
		if err := rsp.UnmarshalResult(&got); err != nil {
			t.Fatalf("Failed to unmarshal result data: %v", err)
		} else if r := string(got); r != "null" {
			t.Errorf("ErrorOnly response: got %q, want null", r)
		}
	})
}

// Verify that a timeout set on the context is respected by the server and
// propagates back to the client as an error.
func TestTimeout(t *testing.T) {
	loc := server.NewLocal(handler.Map{
		"Stall": handler.New(func(ctx context.Context) (bool, error) {
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
	defer loc.Close()
	c := loc.Client

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	got, err := c.Call(ctx, "Stall", nil)
	if err == nil {
		t.Errorf("Stall: got %+v, wanted error", got)
	} else if err != context.DeadlineExceeded {
		t.Errorf("Stall: got error %v, want %v", err, context.DeadlineExceeded)
	} else {
		t.Logf("Successfully cancelled after %v", time.Since(start))
	}
}

// Verify that stopping the server terminates in-flight requests.
func TestServerStopCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan error, 1)
	loc := server.NewLocal(handler.Map{
		"Hang": handler.New(func(ctx context.Context) (bool, error) {
			close(started) // signal that the method handler is running
			<-ctx.Done()
			return true, ctx.Err()
		}),
	}, nil)
	defer loc.Close()
	s, c := loc.Server, loc.Client

	// Call the server. The method will hang until its context is cancelled,
	// which should happen when the server stops.
	go func() {
		defer close(stopped)
		_, err := c.Call(context.Background(), "Hang", nil)
		stopped <- err
	}()

	// Wait until the client method is running so we know we are testing at the
	// right time, i.e., with a request in flight.
	<-started
	s.Stop()
	select {
	case <-time.After(30 * time.Second):
		t.Error("Timed out waiting for service handler to fail")
	case err := <-stopped:
		if ec := code.FromError(err); ec != code.Cancelled {
			t.Errorf("Client error: got %v (%v), wanted code %v", err, ec, code.Cancelled)
		}
	}
}

// Test that an error with data attached to it is correctly propagated back
// from the server to the client, in a value of concrete type *Error.
func TestErrors(t *testing.T) {
	const errCode = -32000
	const errData = `{"caroline":452}`
	const errMessage = "error thingy"
	loc := server.NewLocal(handler.Map{
		"Err": handler.New(func(_ context.Context) (int, error) {
			return 17, jrpc2.DataErrorf(errCode, json.RawMessage(errData), errMessage)
		}),
		"Push": handler.New(func(ctx context.Context) (bool, error) {
			return false, jrpc2.ServerPush(ctx, "PushBack", nil)
		}),
	}, &server.LocalOptions{
		ClientOptions: &jrpc2.ClientOptions{
			OnNotify: func(req *jrpc2.Request) {
				t.Errorf("Client received unexpected push: %#v", req)
			},
		},
	})
	defer loc.Close()
	c := loc.Client

	if got, err := c.Call(context.Background(), "Err", nil); err == nil {
		t.Errorf("Call(Push): got %#v, wanted error", got)
	} else if e, ok := err.(*jrpc2.Error); ok {
		if e.Code() != errCode {
			t.Errorf("Error code: got %d, want %d", e.Code(), errCode)
		}
		if e.Message() != errMessage {
			t.Errorf("Error message: got %q, want %q", e.Message(), errMessage)
		}
		var data json.RawMessage
		if err := e.UnmarshalData(&data); err != nil {
			t.Errorf("Unmarshaling error data: %v", err)
		} else if s := string(data); s != errData {
			t.Errorf("Error data: got %q, want %q", s, errData)
		}
	} else {
		t.Fatalf("Call(Err): unexpected error: %v", err)
	}

	if got, err := c.Call(context.Background(), "Push", nil); err == nil {
		t.Errorf("Call(Push): got %#v, wanted error", got)
	} else {
		t.Logf("Call(Push): got expected error: %v", err)
	}
}

// Test that a client correctly reports bad parameters.
func TestBadCallParams(t *testing.T) {
	loc := server.NewLocal(handler.Map{
		"Test": handler.New(func(_ context.Context, v interface{}) error {
			return jrpc2.Errorf(129, "this should not be reached")
		}),
	}, nil)
	defer loc.Close()

	rsp, err := loc.Client.Call(context.Background(), "Test", "bogus")
	if err == nil {
		t.Errorf("Call(Test): got %+v, wanted error", rsp)
	} else if got, want := code.FromError(err), code.InvalidRequest; got != want {
		t.Errorf("Call(Test): got code %v, want %v", got, want)
	} else {
		t.Logf("Call(Test): got expected error: %v", err)
	}
}

// Verify that metrics are correctly propagated to server info.
func TestServerInfo(t *testing.T) {
	loc := server.NewLocal(handler.Map{
		"Metricize": handler.New(func(ctx context.Context) (bool, error) {
			m := jrpc2.ServerMetrics(ctx)
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
	defer loc.Close()
	s, c := loc.Server, loc.Client

	ctx := context.Background()
	if _, err := c.Call(ctx, "Metricize", nil); err != nil {
		t.Fatalf("Call(Metricize) failed: %v", err)
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
		if test.want >= 0 && got != test.want {
			t.Errorf("Wrong value for metric %q: got %d, want %d", test.name, got, test.want)
		}
	}
}

// Ensure that a correct request not sent via the *Client type will still
// elicit a correct response from the server. Here we simulate a "different"
// client by writing requests directly into the channel.
func TestOtherClient(t *testing.T) {
	srv, cli := channel.Direct()
	s := jrpc2.NewServer(handler.Map{
		"X": handler.New(func(ctx context.Context) (string, error) {
			return "OK", nil
		}),
	}, nil).Start(srv)
	defer func() {
		cli.Close()
		if err := s.Wait(); err != nil {
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
			`{"jsonrpc":"2.0","id":4,"error":{"code":-32600,"message":"parameters must be array or object"}}`},

		// The parameters are absent, but as null.
		{`{"jsonrpc": "2.0", "id": 6, "method": "X", "params": null}`,
			`{"jsonrpc":"2.0","id":6,"result":"OK"}`},

		// A correct request.
		{`{"jsonrpc":"2.0","id": 5, "method": "X"}`,
			`{"jsonrpc":"2.0","id":5,"result":"OK"}`},

		// A batch of correct requests.
		{`[{"jsonrpc":"2.0", "id":"a1", "method":"X"}, {"jsonrpc":"2.0", "id":"a2", "method": "X"}]`,
			`[{"jsonrpc":"2.0","id":"a1","result":"OK"},{"jsonrpc":"2.0","id":"a2","result":"OK"}]`},

		// Extra fields on an otherwise-correct request.
		{`{"jsonrpc":"2.0","id": 7, "method": "Z", "params":[], "bogus":true}`,
			`{"jsonrpc":"2.0","id":7,"error":{"code":-32600,"message":"extra fields in request"}}`},

		// An empty batch request should report a single error object.
		{`[]`, `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"empty request batch"}}`},

		// An invalid batch request should report a single error object.
		{`[1]`, `[{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"request is not a JSON object"}}]`},

		// A batch of invalid requests returns a batch of errors.
		{`[{"jsonrpc": "2.0", "id": 6, "method":"bogus"}]`,
			`[{"jsonrpc":"2.0","id":6,"error":{"code":-32601,"message":"no such method \"bogus\""}}]`},

		// Batch requests return batch responses, even for a singleton.
		{`[{"jsonrpc": "2.0", "id": 7, "method": "X"}]`, `[{"jsonrpc":"2.0","id":7,"result":"OK"}]`},

		// Notifications are not reflected in a batch response.
		{`[{"jsonrpc": "2.0", "method": "note"}, {"jsonrpc": "2.0", "id": 8, "method": "X"}]`,
			`[{"jsonrpc":"2.0","id":8,"result":"OK"}]`},

		// Invalid structure for a version is reported, with and without ID.
		{`{"jsonrpc": false}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"invalid version key"}}`},
		{`{"jsonrpc": false, "id": 747}`,
			`{"jsonrpc":"2.0","id":747,"error":{"code":-32700,"message":"invalid version key"}}`},

		// Invalid structure for a method name is reported, with and without ID.
		{`{"jsonrpc":"2.0", "method": [false]}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"invalid method name"}}`},
		{`{"jsonrpc":"2.0", "method": [false], "id": 252}`,
			`{"jsonrpc":"2.0","id":252,"error":{"code":-32700,"message":"invalid method name"}}`},

		// A broken batch request should report a single top-level error.
		{`[{"jsonrpc":"2.0", "method":"A", "id": 1}, {"jsonrpc":"2.0"]`, // N.B. syntax error
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"invalid request batch"}}`},

		// A broken single request should report a top-level error.
		{`{"bogus"][++`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"invalid request message"}}`},
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
			t.Errorf("Simulated call %#q: got %#q, want %#q", test.input, got, test.want)
		}
	}
}

// Verify that server-side push notifications work.
func TestServerNotify(t *testing.T) {
	// Set up a server and client with server-side notification support.  Here
	// we're just capturing the name of the notification method, as a sign we
	// got the right thing.
	var notes []string
	loc := server.NewLocal(handler.Map{
		"NoteMe": handler.New(func(ctx context.Context) (bool, error) {
			// When this method is called, it posts a notification back to the
			// client before returning.
			if err := jrpc2.ServerPush(ctx, "method", nil); err != nil {
				t.Errorf("ServerPush unexpectedly failed: %v", err)
				return false, err
			}
			return true, nil
		}),
	}, &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{
			AllowPush: true,
		},
		ClientOptions: &jrpc2.ClientOptions{
			OnNotify: func(req *jrpc2.Request) {
				notes = append(notes, req.Method())
				t.Logf("OnNotify handler saw method %q", req.Method())
			},
		},
	})
	s, c := loc.Server, loc.Client
	ctx := context.Background()

	// Post an explicit notification.
	if err := s.Push(ctx, "explicit", nil); err != nil {
		t.Errorf("Notify explicit: unexpected error: %v", err)
	}

	// Call the method that posts a notification.
	if _, err := c.Call(ctx, "NoteMe", nil); err != nil {
		t.Errorf("Call NoteMe: unexpected error: %v", err)
	}

	// Shut everything down to be sure the callbacks have settled.
	loc.Close()

	want := []string{"explicit", "method"}
	if diff := pretty.Compare(notes, want); diff != "" {
		t.Errorf("Server notifications: (-got, +want)\n%s", diff)
	}
}

// Verify that a server push after the client closes does not trigger a panic.
func TestDeadServerPush(t *testing.T) {
	loc := server.NewLocal(make(handler.Map), &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{AllowPush: true},
	})
	loc.Client.Close()
	if err := loc.Server.Push(context.Background(), "whatever", nil); err != jrpc2.ErrConnClosed {
		t.Errorf("Push(whatever): got %v, want %v", err, jrpc2.ErrConnClosed)
	}
}

// Verify that the context encoding/decoding hooks work.
func TestContextPlumbing(t *testing.T) {
	want := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()

	loc := server.NewLocal(handler.Map{
		"X": handler.New(func(ctx context.Context) (bool, error) {
			got, ok := ctx.Deadline()
			if !ok {
				return false, errors.New("no deadline was set")
			} else if !got.Equal(want) {
				return false, fmt.Errorf("deadline: got %v, want %v", got, want)
			}
			t.Logf("Got expected deadline: %v", got)
			return true, nil
		}),
	}, &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{DecodeContext: jctx.Decode},
		ClientOptions: &jrpc2.ClientOptions{EncodeContext: jctx.Encode},
	})
	defer loc.Close()

	if _, err := loc.Client.Call(ctx, "X", nil); err != nil {
		t.Errorf("Call X failed: %v", err)
	}
}

// Verify that the request-checking hook works.
func TestRequestHook(t *testing.T) {
	const wantResponse = "Hey girl"
	const wantToken = "OK"

	loc := server.NewLocal(handler.Map{
		"Test": handler.New(func(ctx context.Context) (string, error) {
			return wantResponse, nil
		}),
	}, &server.LocalOptions{
		// Enable auth checking and context decoding for the server.
		ServerOptions: &jrpc2.ServerOptions{
			DecodeContext: jctx.Decode,
			CheckRequest: func(ctx context.Context, req *jrpc2.Request) error {
				var token []byte
				switch err := jctx.UnmarshalMetadata(ctx, &token); err {
				case nil:
					t.Logf("Metadata present: value=%q", string(token))
				case jctx.ErrNoMetadata:
					t.Log("Metadata not set")
				default:
					return err
				}
				if s := string(token); s != wantToken {
					return jrpc2.Errorf(notAuthorized, "not authorized")
				}
				return nil
			},
		},

		// Enable context encoding for the client.
		ClientOptions: &jrpc2.ClientOptions{
			EncodeContext: jctx.Encode,
		},
	})
	defer loc.Close()
	c := loc.Client

	// Call without a token and verify that we get an error.
	t.Run("NoToken", func(t *testing.T) {
		var rsp string
		err := c.CallResult(context.Background(), "Test", nil, &rsp)
		if err == nil {
			t.Errorf("Call(Test): got %q, wanted error", rsp)
		} else if ec := code.FromError(err); ec != notAuthorized {
			t.Errorf("Call(Test): got code %v, want %v", ec, notAuthorized)
		}
	})

	// Call with a valid token and verify that we get a response.
	t.Run("GoodToken", func(t *testing.T) {
		ctx, err := jctx.WithMetadata(context.Background(), []byte(wantToken))
		if err != nil {
			t.Fatalf("Call(Test): attaching metadata: %v", err)
		}
		var rsp string
		if err := c.CallResult(ctx, "Test", nil, &rsp); err != nil {
			t.Errorf("Call(Test): unexpected error: %v", err)
		}
		if rsp != wantResponse {
			t.Errorf("Call(Test): got %q, want %q", rsp, wantResponse)
		}
	})

	// Call with an invalid token and verify that we get an error.
	t.Run("BadToken", func(t *testing.T) {
		ctx, err := jctx.WithMetadata(context.Background(), []byte("BAD"))
		if err != nil {
			t.Fatalf("Call(Test): attaching metadata: %v", err)
		}
		var rsp string
		if err := c.CallResult(ctx, "Test", nil, &rsp); err == nil {
			t.Errorf("Call(Test): got %q, wanted error", rsp)
		} else if ec := code.FromError(err); ec != notAuthorized {
			t.Errorf("Call(Test): got code %v, want %v", ec, notAuthorized)
		}
	})
}

// Verify that calling a wrapped method which takes no parameters, but in which
// the caller provided parameters, will correctly report an error.
func TestNoParams(t *testing.T) {
	loc := server.NewLocal(handler.Map{
		"Test": handler.New(func(ctx context.Context) (string, error) {
			return "OK", nil // this should not be reached
		}),
	}, nil)
	defer loc.Close()

	var rsp string
	if err := loc.Client.CallResult(context.Background(), "Test", []int{1, 2, 3}, &rsp); err == nil {
		t.Errorf("Call(Test): got %q, wanted error", rsp)
	} else if ec := code.FromError(err); ec != code.InvalidParams {
		t.Errorf("Call(Test): got code %v, wanted %v", ec, code.InvalidParams)
	}
}

// Verify that the rpc.serverInfo handler and client wrapper work together.
func TestRPCServerInfo(t *testing.T) {
	loc := server.NewLocal(handler.Map{
		"Test": handler.New(func(ctx context.Context) (string, error) {
			return "OK", nil // this should not be reached
		}),
	}, nil)
	defer loc.Close()

	si, err := jrpc2.RPCServerInfo(context.Background(), loc.Client)
	if err != nil {
		t.Errorf("RPCServerInfo failed: %v", err)
	}
	{
		got, want := si.Methods, []string{"Test"}
		if diff := pretty.Compare(got, want); diff != "" {
			t.Errorf("Wrong method names: (-got, +want)\n%s", diff)
		}
	}
}
