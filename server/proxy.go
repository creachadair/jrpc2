package server

import (
	"encoding/json"

	"bitbucket.org/creachadair/jrpc2"
)

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
	if err != nil {
		return nil, err
	}
	return jrpc2.MarshalResponse(rsp, parsed.ID)
}
