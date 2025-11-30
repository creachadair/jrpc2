// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/creachadair/mds/mnet"
)

var testService = handler.Map{
	"Test1": handler.New(func(ctx context.Context, ss []string) int {
		return len(ss)
	}),
	"Test2": handler.New(func(ctx context.Context, req json.RawMessage) int {
		return len(req)
	}),
}

func newHTTPServer(t *testing.T, h http.Handler) (*httptest.Server, *http.Client) {
	n := mnet.New(t.Name() + " network")
	lst := n.MustListen("tcp", "test:1234")

	tsp := http.DefaultTransport.(*http.Transport).Clone()
	tsp.DialContext = n.DialContext
	cli := &http.Client{Transport: tsp}

	hsrv := httptest.NewUnstartedServer(h)
	hsrv.Listener = lst
	hsrv.Start()
	t.Cleanup(hsrv.Close)

	return hsrv, cli
}

func TestBridge(t *testing.T) {
	// Verify that a valid POST request succeeds.
	t.Run("PostOK", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			for _, charset := range []string{"", "utf8", "utf-8"} {
				got := mustPost(t, hcli, hsrv.URL, charset, `{
		  "jsonrpc": "2.0",
		  "id": 1,
		  "method": "Test1",
		  "params": ["a", "foolish", "consistency", "is", "the", "hobgoblin"]
		}`, http.StatusOK)

				const want = `{"jsonrpc":"2.0","id":1,"result":6}`
				if got != want {
					t.Errorf("POST body: got %#q, want %#q", got, want)
				}
			}
		})
	})

	// Verify that the bridge will accept a batch.
	t.Run("PostBatchOK", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "", `[
		  {"jsonrpc":"2.0", "id": 3, "method": "Test1", "params": ["first"]},
		  {"jsonrpc":"2.0", "id": 7, "method": "Test1", "params": ["among", "equals"]}
		]`, http.StatusOK)

			const want = `[{"jsonrpc":"2.0","id":3,"result":1},` +
				`{"jsonrpc":"2.0","id":7,"result":2}]`
			if got != want {
				t.Errorf("POST body: got %#q, want %#q", got, want)
			}
		})
	})

	// Verify that a single-request batch comes back as a batch too.
	t.Run("PostBatchSingle", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "", `[
        {"jsonrpc":"2.0", "id": 11, "method": "Test1", "params": ["a", "solo", "request"]}
      ]`, http.StatusOK)

			const want = `[{"jsonrpc":"2.0","id":11,"result":3}]`
			if got != want {
				t.Errorf("POST body: got %#q, want %#q", got, want)
			}
		})
	})

	// Verify that a GET request reports an error.
	t.Run("GetFail", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			rsp, err := hcli.Get(hsrv.URL)
			if err != nil {
				t.Fatalf("GET request failed: %v", err)
			}
			io.Copy(io.Discard, rsp.Body)
			rsp.Body.Close()
			if got, want := rsp.StatusCode, http.StatusMethodNotAllowed; got != want {
				t.Errorf("GET status: got %v, want %v", got, want)
			}
		})
	})

	// Verify that a POST with the wrong content type fails.
	t.Run("PostInvalidType", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			rsp, err := hcli.Post(hsrv.URL, "text/plain", strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("POST request failed: %v", err)
			}
			io.Copy(io.Discard, rsp.Body)
			rsp.Body.Close()
			if got, want := rsp.StatusCode, http.StatusUnsupportedMediaType; got != want {
				t.Errorf("POST response code: got %v, want %v", got, want)
			}
		})
	})

	// Verify that the charset, if provided, is utf-8.
	t.Run("PostInvalidCharset", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "iso-8859-1", "{}", http.StatusUnsupportedMediaType)
			const want = "invalid content-type charset\n"
			if got != want {
				t.Errorf("POST response body: got %q, want %q", got, want)
			}
		})
	})

	// Verify that a POST that generates a JSON-RPC error succeeds.
	t.Run("PostErrorReply", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "utf-8", `{
		  "id": 1,
		  "jsonrpc": "2.0"
		}`, http.StatusOK)

			const exp = `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"empty method name"}}`
			if got != exp {
				t.Errorf("POST body: got %#q, want %#q", got, exp)
			}
		})
	})

	// Verify that an invalid ID is not swallowed by the remapping process (see #80).
	t.Run("PostInvalidID", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "", `{
        "jsonrpc": "2.0",
        "id": ["this is totally bogus"],
        "method": "Test1"
      }`, http.StatusOK)

			const exp = `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"invalid request ID"}}`
			if got != exp {
				t.Errorf("POST body: got %#q, want %#q", got, exp)
			}
		})
	})

	// Verify that a notification returns an empty success.
	t.Run("PostNotification", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			b := jhttp.NewBridge(testService, nil)
			defer checkClose(t, b)
			hsrv, hcli := newHTTPServer(t, b)

			got := mustPost(t, hcli, hsrv.URL, "", `{
		  "jsonrpc": "2.0",
		  "method": "TakeNotice",
		  "params": []
		}`, http.StatusNoContent)
			if got != "" {
				t.Errorf("POST body: got %q, want empty", got)
			}
		})
	})
}

// Verify that the request-parsing hook works.
func TestBridge_parseRequest(t *testing.T) {
	const reqMessage = `{"jsonrpc":"2.0", "method": "Test2", "id": 100, "params":null}`
	const wantReply = `{"jsonrpc":"2.0","id":100,"result":0}`

	setup := func(t *testing.T) (*http.Client, string) {
		b := jhttp.NewBridge(testService, &jhttp.BridgeOptions{
			ParseRequest: func(req *http.Request) ([]*jrpc2.ParsedRequest, error) {
				action := req.Header.Get("x-test-header")
				if action == "fail" {
					return nil, errors.New("parse hook reporting failure")
				}
				return jrpc2.ParseRequests([]byte(reqMessage))
			},
		})
		t.Cleanup(func() { checkClose(t, b) })
		hsrv, hcli := newHTTPServer(t, b)
		t.Cleanup(hsrv.Close)
		return hcli, hsrv.URL
	}
	t.Run("Succeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			// Since a parse hook is set, the method and content-type checks should not occur.
			// We send an empty body to be sure the request comes from the hook.
			req, err := http.NewRequest("GET", url, strings.NewReader(""))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}

			rsp, err := hcli.Do(req)
			if err != nil {
				t.Fatalf("GET request failed: %v", err)
			}
			body, _ := io.ReadAll(rsp.Body)
			rsp.Body.Close()
			if got, want := rsp.StatusCode, http.StatusOK; got != want {
				t.Errorf("GET response code: got %v, want %v", got, want)
			}
			if got := string(body); got != wantReply {
				t.Errorf("Response: got %#q, want %#q", got, wantReply)
			}
		})
	})
	t.Run("Fail", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			req, err := http.NewRequest("POST", url, strings.NewReader(""))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("X-Test-Header", "fail")

			rsp, err := hcli.Do(req)
			if err != nil {
				t.Fatalf("POST request failed: %v", err)
			}
			io.Copy(io.Discard, rsp.Body)
			rsp.Body.Close()
			if got, want := rsp.StatusCode, http.StatusInternalServerError; got != want {
				t.Errorf("POST response code: got %v, want %v", got, want)
			}
		})
	})
}

func TestBridge_parseGETRequest(t *testing.T) {
	mux := handler.Map{
		"str/eq": handler.NewPos(func(ctx context.Context, a, b string) bool {
			return a == b
		}, "lhs", "rhs"),
	}

	setup := func(t *testing.T) (*http.Client, func(string) string) {
		b := jhttp.NewBridge(mux, &jhttp.BridgeOptions{
			ParseGETRequest: func(req *http.Request) (string, any, error) {
				if err := req.ParseForm(); err != nil {
					return "", nil, err
				}
				method := strings.Trim(req.URL.Path, "/")
				params := make(map[string]string)
				for key := range req.Form {
					params[key] = req.Form.Get(key)
				}
				return method, params, nil
			},
		})
		t.Cleanup(func() { checkClose(t, b) })

		hsrv, hcli := newHTTPServer(t, b)
		t.Cleanup(hsrv.Close)
		return hcli, func(pathQuery string) string {
			return hsrv.URL + "/" + pathQuery
		}
	}
	t.Run("GET", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			got := mustGet(t, hcli, url("str/eq?rhs=fizz&lhs=buzz"), http.StatusOK)
			const want = `false`
			if got != want {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("POST", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			const req = `{"jsonrpc":"2.0", "id":1, "method":"str/eq", "params":["foo","foo"]}`
			got := mustPost(t, hcli, url(""), "", req, http.StatusOK)

			const want = `{"jsonrpc":"2.0","id":1,"result":true}`
			if got != want {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
}

func TestChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := jhttp.NewBridge(testService, nil)
		defer checkClose(t, b)
		hsrv, hcli := newHTTPServer(t, b)

		tests := []struct {
			params any
			want   int
		}{
			{nil, 0},
			{[]string{"foo"}, 7},         // ["foo"]
			{map[string]int{"hi": 3}, 8}, // {"hi":3}
		}

		var callCount int
		ch := jhttp.NewChannel(hsrv.URL, &jhttp.ChannelOptions{
			Client: counter{&callCount, hcli},
		})
		cli := jrpc2.NewClient(ch, nil)
		for _, test := range tests {
			var got int
			if err := cli.CallResult(t.Context(), "Test2", test.params, &got); err != nil {
				t.Errorf("Call Test(%v): unexpected error: %v", test.params, err)
			} else if got != test.want {
				t.Errorf("Call Test(%v): got %d, want %d", test.params, got, test.want)
			}
		}

		cli.Close() // also closes the channel

		// Verify that a closed channel reports errors for Send and Recv.
		if err := ch.Send([]byte("whatever")); err == nil {
			t.Error("Send on a closed channel unexpectedly worked")
		}
		if got, err := ch.Recv(); err != io.EOF {
			t.Errorf("Recv = (%#q, %v), want (nil, %v)", string(got), err, io.EOF)
		}

		if callCount != len(tests) {
			t.Errorf("Channel client call count: got %d, want %d", callCount, len(tests))
		}
	})
}

// counter implements the HTTPClient interface via a real HTTP client.  As a
// side effect it counts the number of invocations of its signature method.
type counter struct {
	z *int
	c *http.Client
}

func (c counter) Do(req *http.Request) (*http.Response, error) {
	defer func() { *c.z++ }()
	return c.c.Do(req)
}

func checkClose(t *testing.T, c io.Closer) {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Errorf("Error in Close: %v", err)
	}
}

func mustPost(t *testing.T, cli *http.Client, url, charset, req string, code int) string {
	t.Helper()
	ctype := "application/json"
	if charset != "" {
		ctype += "; charset=" + charset
	}
	rsp, err := cli.Post(url, ctype, strings.NewReader(req))
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	body, err := io.ReadAll(rsp.Body)
	rsp.Body.Close()
	if err != nil {
		t.Errorf("Reading POST body: %v", err)
	}
	if got := rsp.StatusCode; got != code {
		t.Errorf("POST response code: got %v, want %v", got, code)
	}
	return string(body)
}
