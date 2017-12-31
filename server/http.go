package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"bitbucket.org/creachadair/jrpc2"
)

// HTTP adapts a *jrpc2.Client to an http.Handler. The body of each HTTP
// request is transmitted as a JSON-RPC request through the client, and its
// response is written back as the body of the HTTP reply. Each HTTP request is
// handled as a synchronous RPC, but arbitrarily-many HTTP requests may be in
// flight concurrently.
//
// If the HTTP request body is empty or malformed, the handler reports status
// 400 (Bad Request). Any other structural errors in sending or receiving the
// RPC are reported as status 500 (Internal Server Error). A complete RPC reply
// reports status 200 (OK) even if the reply contains an error.
func HTTP(cli *jrpc2.Client) http.Handler {
	caller := RawCaller(cli)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "unable to read request", http.StatusBadRequest)
			return
		}
		rsp, err := caller.CallWait(data)
		if err != nil {
			http.Error(w, "call failed: "+err.Error(), http.StatusInternalServerError)
			return
		} else if len(rsp) != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Write(rsp)
			w.Write([]byte("\n"))
		}
	})
}

// RawCaller returns a wrapper around c that accepts requests as undecoded
// (raw) JSON and returns replies in the same format.
func RawCaller(c *jrpc2.Client) Caller { return Caller{cli: c} }

type Caller struct {
	cli *jrpc2.Client
}

// CallWait sends a raw JSON-RPC request message through the client and returns
// the response as plain JSON. The call blocks until complete.  If the request
// is a notification, CallWait returns nil, nil on success.  Otherwise any
// successful call, even if it contains an error from the server, reports a
// complete JSON response message.
func (c Caller) CallWait(req []byte) ([]byte, error) {
	// Decode the request sufficiently to find the ID, method, and params
	// so we can forward the request through the client.
	var parsed struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(req, &parsed); err != nil {
		return nil, err
	} else if len(parsed.ID) == 0 {
		// Send a notification, and reply with an empty success.
		return nil, c.cli.Notify(parsed.Method, parsed.Params)
	}
	rsp, err := c.cli.CallWait(parsed.Method, parsed.Params)
	if rsp == nil {
		// We only need to directly report the error in case it prevented us
		// from getting a response at all; errors within the protocol are
		// encoded in the response directly.
		return nil, err
	}
	return jrpc2.MarshalResponse(rsp, parsed.ID)
}
