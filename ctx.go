package jrpc2

import (
	"context"
	"errors"

	"github.com/creachadair/jrpc2/metrics"
)

// ServerMetrics returns the server metrics collector associated with the given
// context, or nil if ctx does not have a collector attached.  The context
// passed to a handler by *jrpc2.Server will include this value.
func ServerMetrics(ctx context.Context) *metrics.M { return ServerFromContext(ctx).metrics }

// InboundRequest returns the inbound request associated with the given
// context, or nil if ctx does not have an inbound request. The context passed
// to the handler by *jrpc2.Server will include this value.
//
// This is mainly useful to wrapped server methods that do not have the request
// as an explicit parameter; for direct implementations of Handler.Handle the
// request value returned by InboundRequest will be the same value as was
// passed explicitly.
func InboundRequest(ctx context.Context) *Request {
	if v := ctx.Value(inboundRequestKey{}); v != nil {
		return v.(*Request)
	}
	return nil
}

type inboundRequestKey struct{}

// PushNotify posts a server notification to the client. If the server does not
// have push enabled (via the AllowPush option), it reports ErrPushUnsupported.
// This function is for use by handlers, and will panic for a non-handler context.
func PushNotify(ctx context.Context, method string, params interface{}) error {
	return ServerFromContext(ctx).Notify(ctx, method, params)
}

// PushCall posts a server call to the client. If the server does not have push
// enabled (via the AllowPush option), it reports ErrPushUnsupported.
// This function is for use by handlers, and will panic for a non-handler context.
//
// A successful callback reports a nil error and a non-nil response. Errors
// reported by the client have concrete type *jrpc2.Error.
func PushCall(ctx context.Context, method string, params interface{}) (*Response, error) {
	return ServerFromContext(ctx).Callback(ctx, method, params)
}

// CancelRequest requests the server associated with ctx to cancel the pending
// or in-flight request with the specified ID.  If no request exists with that
// ID, this is a no-op without error.
// This function is for use by handlers, and will panic for a non-handler context.
func CancelRequest(ctx context.Context, id string) { ServerFromContext(ctx).CancelRequest(id) }

// ServerFromContext returns the server associated with the given context.
// This will be populated on the context passed to request handlers.
// This function is for use by handlers, and will panic for a non-handler context.
//
// It is safe to retain the server and invoke its methods beyond the lifetime
// of the context from which it was extracted; however, a handler must not
// block on the Wait or WaitStatus methods of the server, as the server will
// deadlock waiting for the handler to return.
func ServerFromContext(ctx context.Context) *Server { return ctx.Value(serverKey{}).(*Server) }

type serverKey struct{}

// ErrPushUnsupported is returned by PushNotify and PushCall if server pushes
// are not enabled in the specified context.
var ErrPushUnsupported = errors.New("server push is not enabled")
