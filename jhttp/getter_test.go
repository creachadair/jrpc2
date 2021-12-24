package jhttp_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/fortytw2/leaktest"
)

func TestGetter(t *testing.T) {
	defer leaktest.Check(t)()

	mux := handler.Map{
		"concat": handler.NewPos(func(ctx context.Context, a, b string) string {
			return a + b
		}, "first", "second"),
	}

	g := jhttp.NewGetter(mux, &jhttp.GetterOptions{
		Client: &jrpc2.ClientOptions{EncodeContext: checkContext},
	})
	defer checkClose(t, g)

	hsrv := httptest.NewServer(g)
	defer hsrv.Close()
	url := func(pathQuery string) string {
		return hsrv.URL + "/" + pathQuery
	}

	t.Run("OK", func(t *testing.T) {
		got := mustGet(t, url("concat?second=world&first=hello"), http.StatusOK)
		const want = `"helloworld"`
		if got != want {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
	t.Run("NotFound", func(t *testing.T) {
		got := mustGet(t, url("nonesuch"), http.StatusNotFound)
		const want = `"code":-32601` // MethodNotFound
		if !strings.Contains(got, want) {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
	t.Run("BadRequest", func(t *testing.T) {
		// N.B. invalid query string
		got := mustGet(t, url("concat?x%2"), http.StatusBadRequest)
		const want = `"code":-32700` // ParseError
		if !strings.Contains(got, want) {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
	t.Run("InternalError", func(t *testing.T) {
		got := mustGet(t, url("concat?third=c"), http.StatusInternalServerError)
		const want = `"code":-32602` // InvalidParams
		if !strings.Contains(got, want) {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
}

func TestGetter_parseRequest(t *testing.T) {
	defer leaktest.Check(t)()

	mux := handler.Map{
		"format": handler.NewPos(func(ctx context.Context, a string, b int) string {
			return fmt.Sprintf("%s-%d", a, b)
		}, "tag", "value"),
	}

	g := jhttp.NewGetter(mux, &jhttp.GetterOptions{
		ParseRequest: func(req *http.Request) (string, interface{}, error) {
			if err := req.ParseForm(); err != nil {
				return "", nil, err
			}
			params := make(map[string]interface{})
			for key := range req.Form {
				val := req.Form.Get(key)
				v, err := strconv.Atoi(val)
				if err == nil {
					params[key] = v
					continue
				}
				b, err := hex.DecodeString(val)
				if err == nil {
					params[key] = b
					continue
				}
				params[key] = val
			}
			return strings.TrimPrefix(req.URL.Path, "/x/"), params, nil
		},
	})
	defer checkClose(t, g)

	hsrv := httptest.NewServer(g)
	defer hsrv.Close()
	url := func(pathQuery string) string {
		return hsrv.URL + "/" + pathQuery
	}

	t.Run("OK", func(t *testing.T) {
		got := mustGet(t, url("x/format?tag=foo&value=25"), http.StatusOK)
		const want = `"foo-25"`
		if got != want {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
	t.Run("NotFound", func(t *testing.T) {
		// N.B. Missing path prefix.
		got := mustGet(t, url("format"), http.StatusNotFound)
		const want = `"code":-32601` // MethodNotFound
		if !strings.Contains(got, want) {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
	t.Run("InternalError", func(t *testing.T) {
		// N.B. Parameter type does not match on the server side.
		got := mustGet(t, url("x/format?tag=foo&value=bar"), http.StatusInternalServerError)
		const want = `"code":-32602` // InvalidParams
		if !strings.Contains(got, want) {
			t.Errorf("Response body: got %#q, want %#q", got, want)
		}
	})
}

func mustGet(t *testing.T, url string, code int) string {
	t.Helper()
	rsp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	} else if got := rsp.StatusCode; got != code {
		t.Errorf("GET response code: got %v, want %v", got, code)
	}
	body, err := io.ReadAll(rsp.Body)
	if err != nil {
		t.Errorf("Reading GET body: %v", err)
	}
	return string(body)
}
