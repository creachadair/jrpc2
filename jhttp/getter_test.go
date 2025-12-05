// Copyright (C) 2021 Michael J. Fromberger. All Rights Reserved.

package jhttp_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/creachadair/mds/mtest"
	"github.com/google/go-cmp/cmp"
)

func TestGetter(t *testing.T) {
	mux := handler.Map{
		"concat": handler.NewPos(func(ctx context.Context, a, b string) string {
			return a + b
		}, "first", "second"),
	}
	setup := func(t *testing.T) (*http.Client, func(string) string) {
		g := jhttp.NewGetter(mux, nil)
		t.Cleanup(func() { checkClose(t, g) })

		hsrv, hcli := mtest.NewHTTPServer(t, g)
		return hcli, func(pathQuery string) string {
			return hsrv.URL + "/" + pathQuery
		}
	}
	t.Run("OK", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			got := mustGet(t, hcli, url("concat?second=world&first=hello"), http.StatusOK)
			const want = `"helloworld"`
			if got != want {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("NotFound", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			got := mustGet(t, hcli, url("nonesuch"), http.StatusNotFound)
			const want = `"code":-32601` // MethodNotFound
			if !strings.Contains(got, want) {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("BadRequest", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			// N.B. invalid query string
			got := mustGet(t, hcli, url("concat?x%2"), http.StatusBadRequest)
			const want = `"code":-32700` // ParseError
			if !strings.Contains(got, want) {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("InternalError", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			got := mustGet(t, hcli, url("concat?third=c"), http.StatusInternalServerError)
			const want = `"code":-32602` // InvalidParams
			if !strings.Contains(got, want) {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
}

func TestParseQuery(t *testing.T) {
	tests := []struct {
		url     string
		body    string
		method  string
		want    any
		errText string
	}{
		// Error: Missing method name.
		{"http://localhost:2112/", "", "", nil, "empty URL path"},

		// No parameters.
		{"http://localhost/foo", "", "foo", nil, ""},

		// Unbalanced double-quoted string.
		{"https://fuzz.ball/foo?bad=%22xyz", "", "foo", nil, "missing string quote"},
		{"https://fuzz.ball/bar?bad=xyz%22", "", "bar", nil, "missing string quote"},

		// Unbalanced single-quoted string.
		{`http://stripe/sister?bad='invalid`, "", "sister", nil, "missing bytes quote"},
		{`http://stripe/sister?bad=invalid'`, "", "sister", nil, "missing bytes quote"},

		// Invalid byte string.
		{`http://green.as/balls?bad='NOT%20VALID'`, "", "balls", nil, "decoding bytes"},

		// Invalid double-quoted string.
		{`http://black.as/sin?bad=%22a%5Cx25%22`, "", "sin", nil, "invalid character"},

		// Valid: Single-quoted byte string (base64).
		{`http://fast.as.hell/and?twice='YXMgcHJldHR5IGFzIHlvdQ=='`,
			"", "and", map[string]any{
				"twice": []byte("as pretty as you"),
			}, ""},

		// Valid: Unquoted strings and null.
		{`http://head.like/a-hole?black=as&your=null&soul`,
			"", "a-hole", map[string]any{
				"black": "as",
				"your":  nil,
				"soul":  "",
			}, ""},

		// Valid: Quoted strings, numbers, Booleans.
		{`http://foo.com:1999/go/west/?alpha=%22xyz%22&bravo=3&charlie=true&delta=false&echo=3.2`,
			"", "go/west", map[string]any{
				"alpha":   "xyz",
				"bravo":   int64(3),
				"charlie": true,
				"delta":   false,
				"echo":    3.2,
			}, ""},

		// Valid: Form-encoded query in the request body.
		{`http://buz.org:2013/bodyblow`,
			"alpha=%22pdq%22&bravo=-19.4&charlie=false", "bodyblow", map[string]any{
				"alpha":   "pdq",
				"bravo":   float64(-19.4),
				"charlie": false,
			}, ""},
	}
	for _, test := range tests {
		t.Run("ParseQuery", func(t *testing.T) {
			req, err := http.NewRequest("PUT", test.url, strings.NewReader(test.body))
			if err != nil {
				t.Fatalf("New request for %q failed: %v", test.url, err)
			}
			if test.body != "" {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}

			method, params, err := jhttp.ParseQuery(req)
			if err != nil {
				if test.errText == "" {
					t.Fatalf("ParseQuery failed: %v", err)
				} else if !strings.Contains(err.Error(), test.errText) {
					t.Fatalf("ParseQuery: got error %v, want %q", err, test.errText)
				}
			} else if test.errText != "" {
				t.Fatalf("ParseQuery: got method %q, params %+v, wanted error %q",
					method, params, test.errText)
			} else {
				if method != test.method {
					t.Errorf("ParseQuery method: got %q, want %q", method, test.method)
				}
				if diff := cmp.Diff(test.want, params); diff != "" {
					t.Errorf("Wrong params: (-want, +got)\n%s", diff)
				}
			}
		})
	}
}

func TestGetter_parseRequest(t *testing.T) {
	mux := handler.Map{
		"format": handler.NewPos(func(ctx context.Context, a string, b int) string {
			return fmt.Sprintf("%s-%d", a, b)
		}, "tag", "value"),
	}

	setup := func(t *testing.T) (*http.Client, func(string) string) {
		g := jhttp.NewGetter(mux, &jhttp.GetterOptions{
			ParseRequest: func(req *http.Request) (string, any, error) {
				if err := req.ParseForm(); err != nil {
					return "", nil, err
				}
				tag := req.Form.Get("tag")
				val, err := strconv.ParseInt(req.Form.Get("value"), 10, 64)
				if err != nil && req.Form.Get("value") != "" {
					return "", nil, fmt.Errorf("invalid number: %w", err)
				}
				return strings.TrimPrefix(req.URL.Path, "/x/"), map[string]any{
					"tag":   tag,
					"value": val,
				}, nil
			},
		})
		t.Cleanup(func() { checkClose(t, g) })

		hsrv, hcli := mtest.NewHTTPServer(t, g)
		return hcli, func(pathQuery string) string {
			return hsrv.URL + "/" + pathQuery
		}
	}
	t.Run("OK", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)
			got := mustGet(t, hcli, url("x/format?tag=foo&value=25"), http.StatusOK)
			const want = `"foo-25"`
			if got != want {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("NotFound", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)

			// N.B. Missing path prefix.
			got := mustGet(t, hcli, url("format"), http.StatusNotFound)
			const want = `"code":-32601` // MethodNotFound
			if !strings.Contains(got, want) {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
	t.Run("InternalError", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hcli, url := setup(t)

			// N.B. Parameter type does not match on the server side.
			got := mustGet(t, hcli, url("x/format?tag=foo&value=bar"), http.StatusBadRequest)
			const want = `"code":-32700` // ParseError
			if !strings.Contains(got, want) {
				t.Errorf("Response body: got %#q, want %#q", got, want)
			}
		})
	})
}

func mustGet(t *testing.T, cli *http.Client, url string, code int) string {
	t.Helper()
	rsp, err := cli.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	body, err := io.ReadAll(rsp.Body)
	rsp.Body.Close()
	if err != nil {
		t.Errorf("Reading GET body: %v", err)
	}
	if got := rsp.StatusCode; got != code {
		t.Errorf("GET response code: got %v, want %v", got, code)
	}
	return string(body)
}
