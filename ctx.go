package jrpc2

import (
	"context"

	"bitbucket.org/creachadair/jrpc2/metrics"
)

// serverContextKey is the concrete type of the context keys used to dispatch
// server-side context values in to handlers.
type serverContextKey string

// ServerMetrics returns the server metrics collector associated with the given
// context, or nil if ctx doees not have a collector attached.  The context
// passed to a handler by *jrpc2.Server will include this value.
func ServerMetrics(ctx context.Context) *metrics.M {
	if v := ctx.Value(serverMetricsKey); v != nil {
		return v.(*metrics.M)
	}
	return nil
}

const serverMetricsKey = serverContextKey("server-metrics")

// InboundRequest returns the inbound request associated with the given
// context, or nil if ctx does not have an inbound request. The context passed
// to the handler by *jrpc2.Server will include this value.
//
// This is mainly useful to wrapped server methods that do not have the request
// as an explicit parameter; for direct implementations of Method.Call the
// request value returned by InboundRequest will be the same value as was
// passed explicitly.
func InboundRequest(ctx context.Context) *Request {
	if v := ctx.Value(inboundRequestKey); v != nil {
		return v.(*Request)
	}
	return nil
}

const inboundRequestKey = serverContextKey("inbound-request")

// ServerNotify returns the server notifier associated with the given context,
// or nil if ctx does not have a server notifier. The context passed to the
// handler by *jrpc2.Server will include this value if the server was
// constructed with the AllowNotify option set true.
func ServerNotify(ctx context.Context) func(context.Context, string, interface{}) error {
	if v := ctx.Value(serverNotifyKey); v != nil {
		return v.(func(context.Context, string, interface{}) error)
	}
	return nil
}

const serverNotifyKey = serverContextKey("server-notify")
