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
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "unable to read request", http.StatusBadRequest)
			return
		}

		// Decode the request sufficiently to find the ID, method, and params
		// so we can forward the request through the client.
		var parsed struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method,omitempty"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(parsed.ID) == 0 {
			// Send a notification, and reply with an empty success.
			if err := cli.Notify(parsed.Method, parsed.Params); err != nil {
				http.Error(w, "sending notification: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}

		// Unpack the response and restore the original request ID.
		rsp, err := cli.CallWait(parsed.Method, parsed.Params)
		if rsp == nil {
			http.Error(w, "call failed: "+err.Error(), http.StatusInternalServerError)
			return
		} else if data, err := jrpc2.MarshalResponse(rsp, parsed.ID); err != nil {
			http.Error(w, "encoding response failed: "+err.Error(), http.StatusInternalServerError)
			return
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			w.Write([]byte("\n"))
		}
	})
}
