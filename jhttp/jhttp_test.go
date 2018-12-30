package jhttp

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

func TestBridge(t *testing.T) {
	// Set up a JSON-RPC server to answer requests bridged from HTTP.
	cc, cs := channel.Pipe(channel.Varint)
	srv := jrpc2.NewServer(jrpc2.MapAssigner{
		"Test": jrpc2.NewHandler(func(ctx context.Context, ss ...string) (string, error) {
			return strings.Join(ss, " "), nil
		}),
	}, nil).Start(cs)
	defer srv.Stop()

	// Bridge HTTP to the JSON-RPC server.
	b := New(cc, nil)
	defer b.Close()

	// Create an HTTP test server to call into the bridge.
	hsrv := httptest.NewServer(b)
	defer hsrv.Close()

	// Verify that a valid POST request succeeds.
	t.Run("PostOK", func(t *testing.T) {
		rsp, err := http.Post(hsrv.URL, "application/json", strings.NewReader(`{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "Test",
  "params": ["a", "foolish", "consistency", "is", "the", "hobgoblin"]
}
`))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		} else if got, want := rsp.StatusCode, http.StatusOK; got != want {
			t.Errorf("POST response code: got %v, want %v", got, want)
		}
		body, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			t.Errorf("Reading POST body: %v", err)
		}

		const want = `{"jsonrpc":"2.0","id":1,"result":"a foolish consistency is the hobgoblin"}`
		if got := string(body); got != want {
			t.Errorf("POST body: got %#q, want %#q", got, want)
		}
	})

	// Verify that the bridge will accept a batch.
	t.Run("PostBatchOK", func(t *testing.T) {
		rsp, err := http.Post(hsrv.URL, "application/json", strings.NewReader(`[
  {"jsonrpc":"2.0", "id": 3, "method": "Test", "params": ["first"]},
  {"jsonrpc":"2.0", "id": 7, "method": "Test", "params": ["among", "equals"]}
]
`))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		} else if got, want := rsp.StatusCode, http.StatusOK; got != want {
			t.Errorf("POST response code: got %v, want %v", got, want)
		}
		body, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			t.Errorf("Reading POST body: %v", err)
		}

		const want = `[{"jsonrpc":"2.0","id":3,"result":"first"},` +
			`{"jsonrpc":"2.0","id":7,"result":"among equals"}]`
		if got := string(body); got != want {
			t.Errorf("POST body: got %#q, want %#q", got, want)
		}
	})

	// Verify that a GET request reports an error.
	t.Run("GetFail", func(t *testing.T) {
		rsp, err := http.Get(hsrv.URL)
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		if got, want := rsp.StatusCode, http.StatusMethodNotAllowed; got != want {
			t.Errorf("GET status: got %v, want %v", got, want)
		}
	})

	// Verify that a POST with the wrong content type fails.
	t.Run("PostInvalidType", func(t *testing.T) {
		rsp, err := http.Post(hsrv.URL, "text/plain", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		}
		if got, want := rsp.StatusCode, http.StatusUnsupportedMediaType; got != want {
			t.Errorf("POST status: got %v, want %v", got, want)
		}
	})

	// Verify that a POST that generates a JSON-RPC error succeeds.
	t.Run("PostErrorReply", func(t *testing.T) {
		rsp, err := http.Post(hsrv.URL, "application/json", strings.NewReader(`{
  "id": 1,
  "jsonrpc": "2.0"
}
`))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		} else if got, want := rsp.StatusCode, http.StatusOK; got != want {
			t.Errorf("POST status: got %v, want %v", got, want)
		}
		body, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			t.Errorf("Reading POST body: %v", err)
		}

		const exp = `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"empty method name"}}`
		if got := string(body); got != exp {
			t.Errorf("POST body: got %#q, want %#q", got, exp)
		}
	})

	// Verify that a notification returns an empty success.
	t.Run("PostNotification", func(t *testing.T) {
		rsp, err := http.Post(hsrv.URL, "application/json", strings.NewReader(`{
  "jsonrpc": "2.0",
  "method": "TakeNotice",
  "params": []
}`))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		} else if got, want := rsp.StatusCode, http.StatusNoContent; got != want {
			t.Errorf("POST status: got %v, want %v", got, want)
		}
		body, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			t.Errorf("Reading POST body: %v", err)
		}
		if got := string(body); got != "" {
			t.Errorf("POST body: got %q, want empty", got)
		}
	})
}
