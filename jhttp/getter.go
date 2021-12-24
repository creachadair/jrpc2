package jhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/code"
	"github.com/creachadair/jrpc2/server"
)

// A Getter is a http.Handler that bridges GET requests to a JSON-RPC server.
//
// The JSON-RPC method name and parameters are decoded from the request URL.
// The results from a successful call are encoded as JSON in the response body
// with status 200 (OK). In case of error, the response body is a JSON-RPC
// error object, and the HTTP status is one of the following:
//
//  Condition               HTTP Status
//  ----------------------- -----------------------------------
//  Parsing request         400 (Bad request)
//  Method not found        404 (Not found)
//  (other errors)          500 (Internal server error)
//
// By default, the URL path identifies the JSON-RPC method, and the URL query
// parameters are converted into a JSON object for the parameters. Leading and
// trailing slashes are stripped from the path, and query values are converted
// into JSON strings.
//
// For example, the URL "http://host:port/path/to/method?foo=true&bar=okay"
// decodes to the method name "path/to/method" and this parameter object:
//
//   {"foo": "true", "bar": "okay"}
//
// Set a ParseRequest hook in the GetterOptions to override this behaviour.
type Getter struct {
	local    server.Local
	parseReq func(*http.Request) (string, interface{}, error)
}

// NewGetter constructs a new Getter that starts a server on mux and dispatches
// HTTP requests to it. The server will run until the getter is closed.
//
// Note that a getter is not able to push calls or notifications from the
// server back to the remote client even if enabled.
func NewGetter(mux jrpc2.Assigner, opts *GetterOptions) Getter {
	return Getter{
		local: server.NewLocal(mux, &server.LocalOptions{
			Client: opts.clientOptions(),
			Server: opts.serverOptions(),
		}),
		parseReq: opts.parseRequest(),
	}
}

// ServeHTTP implements the required method of http.Handler.
func (g Getter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	method, params, err := g.parseHTTPRequest(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &jrpc2.Error{
			Code:    code.ParseError,
			Message: err.Error(),
		})
		return
	}

	ctx := context.WithValue(req.Context(), httpReqKey{}, req)
	var result json.RawMessage
	if err := g.local.Client.CallResult(ctx, method, params, &result); err != nil {
		var status int
		switch code.FromError(err) {
		case code.MethodNotFound:
			status = http.StatusNotFound
		default:
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Close closes the channel to the server, waits for the server to exit, and
// reports its exit status.
func (g Getter) Close() error { return g.local.Close() }

func (g Getter) parseHTTPRequest(req *http.Request) (string, interface{}, error) {
	if g.parseReq != nil {
		return g.parseReq(req)
	}
	if err := req.ParseForm(); err != nil {
		return "", nil, err
	}
	params := make(map[string]string)
	for key := range req.Form {
		params[key] = req.Form.Get(key)
	}
	return strings.Trim(req.URL.Path, "/"), params, nil
}

// GetterOptions are optional settings for a Getter. A nil pointer is ready for
// use and provides default values as described.
type GetterOptions struct {
	// Options for the getter client (default nil).
	Client *jrpc2.ClientOptions

	// Options for the getter server (default nil).
	Server *jrpc2.ServerOptions

	// If set, this function is called to parse a method name and request
	// parameters from an HTTP request. If this is not set, the default handler
	// uses the URL path as the method name and the URL query as the method
	// parameters.
	ParseRequest func(*http.Request) (string, interface{}, error)
}

func (o *GetterOptions) clientOptions() *jrpc2.ClientOptions {
	if o == nil {
		return nil
	}
	return o.Client
}

func (o *GetterOptions) serverOptions() *jrpc2.ServerOptions {
	if o == nil {
		return nil
	}
	return o.Server
}

func (o *GetterOptions) parseRequest() func(*http.Request) (string, interface{}, error) {
	if o == nil {
		return nil
	}
	return o.ParseRequest
}

func writeJSON(w http.ResponseWriter, code int, obj interface{}) {
	bits, err := json.Marshal(obj)
	if err != nil {
		// Fallback in case of marshaling error. This should not happen, but
		// ensures the client gets a loggable reply from a broken server.
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err.Error())
		return
	}
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bits)))
	w.Write(bits)
}
