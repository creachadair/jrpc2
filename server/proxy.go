package server

import (
	"encoding/json"

	"bitbucket.org/creachadair/jrpc2"
)

// NewProxy returns a wrapper around c that accepts requests as undecoded (raw)
// JSON and returns replies in the same format.
func NewProxy(c *jrpc2.Client) Proxy { return Proxy{cli: c} }

// A Proxy is an adapter around a jrpc2.Client that supports implementing a
// proxy from another transport mechanism into a JSON-RPC server.
type Proxy struct{ cli *jrpc2.Client }

// Send sends a raw JSON-RPC request message through the client and returns the
// response as plain JSON. The call blocks until complete.  If the request is a
// notification, Send returns nil, nil on success.  Otherwise any successful
// call, even if it contains an error from the server, reports a complete JSON
// response message.
func (p Proxy) Send(req []byte) ([]byte, error) {
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
		return nil, p.cli.Notify(parsed.Method, parsed.Params)
	}
	rsp, err := p.cli.CallWait(parsed.Method, parsed.Params)
	if err != nil {
		return nil, err
	}
	return jrpc2.MarshalResponse(rsp, parsed.ID)
}
