// Package proxy implements a transparent JSON-RPC proxy that dispatches to a
// jrpc2.Client.
package proxy

import (
	"context"
	"encoding/json"

	"github.com/creachadair/jrpc2"
)

// New creates a proxy that dispatches inbound requests to the given client.
// The resulting value satisfies the jrpc2.Assigner interface, allowing it to
// be used as the assigner for a jrpc2.Server.
//
// For example:
//    cli := jrpc2.NewClient(ch, clientOpts)
//    ...
//    s := jrpc2.NewServer(proxy.New(cli), &jrpc2.ServerOptions{
//        DisableBuiltin: true,  // disable the proxy's rpc.* handlers
//    })
//
func New(c *jrpc2.Client) *Proxy {
	return &Proxy{h: proxHandler{c}}
}

// A Proxy is a JSON-RPC transparent proxy. It implements a jrpc2.Assigner that
// assigns each requested method to a handler that forwards the request to a
// server connected through a *jrpc2.Client.
type Proxy struct{ h proxHandler }

// Close closes the underlying client for p and reports its result.
func (p *Proxy) Close() error { return p.h.client.Close() }

// Assign implements part of the jrpc2.Assigner interface. All methods are
// assigned to the proxy's internal handler, which forwards them across the
// client.
func (p *Proxy) Assign(_ string) jrpc2.Handler { return p.h }

// Names implements part of the jrpc2.Assigner interface.  It always returns
// nil, since the resolution of method names is delegated to the remote server.
func (Proxy) Names() []string { return nil }

type proxHandler struct{ client *jrpc2.Client }

// Handle implements the jrpc2.Handler interface. It handles any call or
// notification method name given, by forwarding it transparently to the remote
// server. The only errors returned from the proxy itself are decoding errors,
// or errors from the internals of the client's Call and Notify methods.
func (h proxHandler) Handle(ctx context.Context, req *jrpc2.Request) (interface{}, error) {
	// If the request has parameters, unpack them so we can pass them to the call.
	var params interface{}
	if req.HasParams() {
		var msg json.RawMessage
		if err := req.UnmarshalParams(&msg); err != nil {
			return nil, err
		}
		params = msg
	}

	// If the request is a notification, do not block for a response.
	if req.IsNotification() {
		return nil, h.client.Notify(ctx, req.Method(), params)
	}

	// Invoke the requested method on the proxied server.
	rsp, err := h.client.Call(ctx, req.Method(), params)
	if err != nil {
		return nil, err
	}

	// Extract the response value or error.
	var result json.RawMessage
	if err := rsp.UnmarshalResult(&result); err != nil {
		return nil, err
	}
	return result, nil
}
