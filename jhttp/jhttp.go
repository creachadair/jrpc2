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
	if err := b.serveInternal(w, req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, err.Error())
	}
}

func (b *Bridge) serveInternal(w http.ResponseWriter, req *http.Request) error {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return err
	}
	jreq, err := jrpc2.ParseRequests(body)
	if err != nil {
		return err
	}

	// Because the bridge shares the JSON-RPC client between potentially many
	// HTTP clients, we must virtualize the ID space for requests to preserve
	// the HTTP client's assignment of IDs.
	//
	// To do this, we keep track of the inbound ID for each request so that we
	// can map the responses back. This takes advantage of the fact that the
	// *jrpc2.Client detangles batch order so that responses come back in the
	// same order (modulo notifications) even if the server response did not
	// preserve order.

	// Generate request specifications for the client.
	var inboundID []string                // for requests
	spec := make([]jrpc2.Spec, len(jreq)) // requests & notifications
	for i, req := range jreq {
		spec[i] = jrpc2.Spec{
			Method: req.Method(),
			Notify: req.IsNotification(),
		}
		if req.HasParams() {
			var p json.RawMessage
			req.UnmarshalParams(&p)
			spec[i].Params = p
		}
		if !spec[i].Notify {
			inboundID = append(inboundID, req.ID())
		}
	}

	rsps, err := b.cli.Batch(req.Context(), spec)
	if err != nil {
		return err
	}

	// If all the requests were notifications, report success without responses.
	if len(rsps) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	// Otherwise, map the responses back to their original IDs, and marshal the
	// response back into the body.
	for i, rsp := range rsps {
		rsp.SetID(inboundID[i])
	}

	// If the original request was a single message, make sure we encode the
	// response the same way.
	var reply []byte
	if len(rsps) == 1 && !bytes.HasPrefix(body, []byte("[")) {
		reply, err = json.Marshal(rsps[0])
	} else {
		reply, err = json.Marshal(rsps)
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(reply)))
	w.Write(reply)
	return nil
}

// Close shuts down the client associated with b and reports the result from
// its Close method.
func (b *Bridge) Close() error { return b.cli.Close() }

// New constructs a new Bridge that dispatches requests through a client
// constructed from the specified channel and options.
func New(ch channel.Channel, opts *jrpc2.ClientOptions) *Bridge {
	return &Bridge{cli: jrpc2.NewClient(ch, opts)}
}
