// Package jhttp implements a bridge from HTTP to JSON-RPC.  This permits
// requests to be submitted to a JSON-RPC server using HTTP as a transport.
package jhttp

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
)

// A Bridge is a http.Handler that bridges requests to a JSON-RPC server.
//
// The body of the HTTP POST request must contain the complete JSON-RPC request
// message, encoded with Content-Type: application/json. Either a single
// request object or a list of request objects is supported.
//
// If the request completes, whether or not there is an error, the HTTP
// response is 200 (OK) for ordinary requests or 204 (No Response) for
// notifications, and the response body contains the JSON-RPC response.
//
// If the HTTP request method is not "POST", the bridge reports 405 (Method Not
// Allowed). If the Content-Type is not application/json, the bridge reports
// 415 (Unsupported Media Type).
type Bridge struct {
	ch        channel.Channel
	srv       *jrpc2.Server
	checkType func(string) bool
}

// ServeHTTP implements the required method of http.Handler.
func (b Bridge) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !b.checkType(req.Header.Get("Content-Type")) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	if err := b.serveInternal(w, req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err.Error())
	}
}

func (b Bridge) serveInternal(w http.ResponseWriter, req *http.Request) error {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return err
	}

	// The HTTP request requires a response, but the server will not reply if
	// all the requests are notifications. Check whether we have any calls
	// needing a response, and choose whether to wait for a reply based on that.
	jreq, err := jrpc2.ParseRequests(body)
	if err != nil {
		return err
	}
	var hasCall bool
	for _, req := range jreq {
		if !req.IsNotification() {
			hasCall = true
			break
		}
	}
	if err := b.ch.Send(body); err != nil {
		return err
	}

	// If there are only notifications, report success without responses.
	if !hasCall {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	// Wait for the server to reply.
	reply, err := b.ch.Recv()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(reply)))
	w.Write(reply)
	return nil
}

// Close closes the channel to the server, waits for the server to exit, and
// reports the exit status of the server.
func (b Bridge) Close() error { b.ch.Close(); return b.srv.Wait() }

// NewBridge constructs a new Bridge that starts srv and dispatches HTTP
// requests to it.  The server must be unstarted or NewBridge will panic.
// The server will run until the bridge is closed.
func NewBridge(srv *jrpc2.Server, opts *BridgeOptions) Bridge {
	cch, sch := channel.Direct()
	return Bridge{
		ch:        cch,
		srv:       srv.Start(sch),
		checkType: opts.checkContentType(),
	}
}

// BridgeOptions are optional settings for a Bridge. A nil pointer is ready for
// use and provides default values as described.
type BridgeOptions struct {
	// If non-nil, this function is called to check whether the HTTP request's
	// declared content-type is valid. If this function returns false, the
	// request is rejected. If nil, the default check requires a content type of
	// "application/json".
	CheckContentType func(contentType string) bool
}

func (o *BridgeOptions) checkContentType() func(string) bool {
	if o == nil || o.CheckContentType == nil {
		return func(ctype string) bool { return ctype == "application/json" }
	}
	return o.CheckContentType
}
