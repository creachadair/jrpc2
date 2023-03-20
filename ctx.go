// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package jrpc2

import (
	"context"
)

// InboundRequest returns the inbound request associated with the context
// passed to a Handler, or nil if ctx does not have an inbound request.
// A *jrpc2.Server populates this value for handler contexts.
//
// This is mainly useful to wrapped server methods that do not have the request
// as an explicit parameter; for direct implementations of the Handler type the
// request value returned by InboundRequest will be the same value as was
// passed explicitly.
func InboundRequest(ctx context.Context) *Request {
	if v := ctx.Value(inboundRequestKey{}); v != nil {
		return v.(*Request)
	}
	return nil
}

type inboundRequestKey struct{}

// ServerFromContext returns the server associated with the context passed to a
// Handler by a *jrpc2.Server.  It will panic for a non-handler context.
//
// It is safe to retain the server and invoke its methods beyond the lifetime
// of the context from which it was extracted; however, a handler must not
// block on the Wait or WaitStatus methods of the server, as the server will
// deadlock waiting for the handler to return.
func ServerFromContext(ctx context.Context) *Server { return ctx.Value(serverKey{}).(*Server) }

type serverKey struct{}

// ClientFromContext returns the client associated with the given context.
// This will be populated on the context passed by a *jrpc2.Client to a
// client-side callback handler.
//
// A callback handler MUST NOT close the client, as the close will deadlock
// waiting for the callback to return.
func ClientFromContext(ctx context.Context) *Client { return ctx.Value(clientKey{}).(*Client) }

type clientKey struct{}
