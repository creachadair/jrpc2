// Package jhttp implements a bridge from HTTP to JSON-RPC.  This permits
// requests to be submitted to a JSON-RPC server using HTTP as a transport.
package jhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

// A Bridge is a http.Handler that bridges requests to a JSON-RPC client.
//
// The body of the HTTP POST request must contain the complete JSON-RPC request
// message, encoded with Content-Type: application/json. Either a single
// request object or a list of request objects are supported.
//
// If the request completes, whether or not there is an error, the HTTP
// response is 200 (OK) for ordinary requests or 204 (No Response) for
// notifications, and the response body contains the JSON-RPC response.
//
// If the HTTP request method is not "POST", the bridge reports 405 (Method Not
// Allowed). If the Content-Type is not application/json, the bridge reports
// 415 (Unsupported Media Type).
type Bridge struct {
	cli *jrpc2.Client
}

// ServeHTTP implements the required method of http.Handler.
func (b *Bridge) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	} else if req.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	var body, reply []byte
	var rsp []*jrpc2.Response

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		goto failed
	}
	rsp, err = b.cli.CallRaw(req.Context(), body)
	if err != nil {
		goto failed
	}

	// If all the requests were notifications, report success without responses.
	if len(rsp) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Otherwise, marshal the response back into the body.
	if len(rsp) == 1 && !bytes.HasPrefix(body, []byte("[")) {
		reply, err = json.Marshal(rsp[0])
	} else {
		reply, err = json.Marshal(rsp)
	}
	if err != nil {
		goto failed
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(reply)))
	w.Write(reply)

	return
failed:
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintln(w, err.Error())
}

// Close shuts down the client associated with b and reports the result from
// its Close method.
func (b *Bridge) Close() error { return b.cli.Close() }

// New constructs a new Bridge that dispatches requests through a client
// constructed from the specified channel and options.
func New(ch channel.Channel, opts *jrpc2.ClientOptions) *Bridge {
	return &Bridge{cli: jrpc2.NewClient(ch, opts)}
}
