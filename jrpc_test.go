package jrpc2

import (
	"context"
	"errors"
	"io"
	"os"
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

// Unrelated should not be picked up by the server.
func (dummy) Unrelated() string { return "ceci n'est pas une méthode" }

func TestClientServer(t *testing.T) {
	cpipe, spipe := pipePair()

	ass := ServiceMapper{
		"Test": MapAssigner(NewMethods(dummy{})),
	}
	s, err := NewServer(ass, ServerLog(os.Stderr), AllowV1(true), Concurrency(16)).Start(spipe)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Logf("Server running on pipe %v", spipe)
	c := NewClient(cpipe, ClientLog(os.Stderr))
	t.Logf("Client running on pipe %v", cpipe)

	tests := []struct {
		method string
		params interface{}
		want   int
	}{
		{"Test.Add", []int{}, 0},
		{"Test.Add", []int{1, 2, 3}, 6},
		{"Test.Mul", struct{ X, Y int }{7, 9}, 63},
		{"Test.Mul", struct{ X, Y int }{}, 0},
	}

	// Verify that individual sequential requests work.
	for _, test := range tests {
		rsp, err := c.Call(test.method, test.params)
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
	var reqs []*Request
	for _, test := range tests {
		req, err := c.Req(test.method, test.params)
		if err != nil {
			t.Fatalf("Req %q %v: unexpected error: %v", test.method, test.params, err)
		}
		reqs = append(reqs, req)
	}
	ps, err := c.Send(reqs...)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	} else if len(ps) != len(tests) {
		t.Errorf("Wrong number of penders: got %d, want %d", len(ps), len(tests))
	}
	for i, p := range ps {
		rsp, err := p.Wait()
		if err != nil {
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

	t.Logf("Client close: err=%v", c.Close())
	s.Stop()
	t.Logf("Server wait: err=%v", s.Wait())
}
