package jrpc2

import (
	"context"
	"encoding/json"

	"bitbucket.org/creachadair/jrpc2/code"
)

// Handle the special rpc.cancel notification, that requests cancellation of a
// set of pending methods. This only works if issued as a notification.
func (s *Server) handleRPCCancel(ctx context.Context, req *Request) (interface{}, error) {
	if !InboundRequest(ctx).IsNotification() {
		return nil, code.MethodNotFound.Err()
	}
	var ids []json.RawMessage
	if err := req.UnmarshalParams(&ids); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, raw := range ids {
		id := string(raw)
		if s.cancel(id) {
			s.log("Cancelled request %s by client order", id)
		}
	}
	return nil, nil
}

// methodFunc is a replication of handler.Func redeclared to avert a cycle.
type methodFunc func(context.Context, *Request) (interface{}, error)

func (m methodFunc) Handle(ctx context.Context, req *Request) (interface{}, error) {
	return m(ctx, req)
}

// Handle the special rpc.serverInfo method, that requests server vitals.
func (s *Server) handleRPCServerInfo(context.Context, *Request) (interface{}, error) {
	return s.serverInfo(), nil
}

func (s *Server) installBuiltins() {
	s.rpcHandlers = map[string]Handler{
		"rpc.cancel":     methodFunc(s.handleRPCCancel),
		"rpc.serverInfo": methodFunc(s.handleRPCServerInfo),
	}
}

// RPCServerInfo calls the built-in rpc.serverInfo method exported by servers.
// It is a convenience wrapper for an invocation of cli.CallResult.
func RPCServerInfo(ctx context.Context, cli *Client) (result *ServerInfo, err error) {
	err = cli.CallResult(ctx, "rpc.serverInfo", nil, &result)
	return
}
