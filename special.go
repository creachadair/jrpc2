package jrpc2

import (
	"context"
	"encoding/json"

	"bitbucket.org/creachadair/jrpc2/code"
)

// Handle the special rpc.cancel notification, that requests cancellation of a
// set of pending methods. This only works if issued as a notification.
func (s *Server) handleRPCCancel(ctx context.Context, ids []json.RawMessage) error {
	if !InboundRequest(ctx).IsNotification() {
		return code.MethodNotFound.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, raw := range ids {
		id := string(raw)
		if s.cancel(id) {
			s.log("Cancelled request %s by client order", id)
		}
	}
	return nil
}

// Handle the special rpc.serverInfo method, that requests server vitals.
func (s *Server) handleRPCServerInfo(context.Context) (*ServerInfo, error) {
	return s.serverInfo(), nil
}

func (s *Server) installBuiltins() {
	s.rpcHandlers = map[string]Handler{
		"rpc.cancel":     NewHandler(s.handleRPCCancel),
		"rpc.serverInfo": NewHandler(s.handleRPCServerInfo),
	}
}
