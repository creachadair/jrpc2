package jrpc2

import (
	"context"
	"encoding/json"
	"strings"

	"bitbucket.org/creachadair/jrpc2/code"
	"bitbucket.org/creachadair/jrpc2/metrics"
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

// Handle the special rpc.count notification, that updates counters.
func (s *Server) handleRPCCount(ctx context.Context, m metrics.Int64) error {
	if !InboundRequest(ctx).IsNotification() {
		return code.MethodNotFound.Err()
	} else if m.Name != "" && !strings.HasPrefix(m.Name, "rpc.") {
		s.metrics.Count(m.Name, m.Value)
	}
	return nil
}

// Handle the special rpc.maxValue notification, that updates max value trackers.
func (s *Server) handleRPCMaxValue(ctx context.Context, m metrics.Int64) error {
	if !InboundRequest(ctx).IsNotification() {
		return code.MethodNotFound.Err()
	} else if m.Name != "" && !strings.HasPrefix(m.Name, "rpc.") {
		s.metrics.SetMaxValue(m.Name, m.Value)
	}
	return nil
}

// Handle the special rpc.setLabel notification, that updates label metrics.
func (s *Server) handleRPCSetLabel(ctx context.Context, m metrics.Label) error {
	if !InboundRequest(ctx).IsNotification() {
		return code.MethodNotFound.Err()
	} else if m.Name != "" && !strings.HasPrefix(m.Name, "rpc.") {
		s.metrics.SetLabel(m.Name, m.Value)
	}
	return nil
}

func (s *Server) installBuiltins() {
	s.rpcHandlers = map[string]Handler{
		"rpc.cancel":     NewHandler(s.handleRPCCancel),
		"rpc.count":      NewHandler(s.handleRPCCount),
		"rpc.maxValue":   NewHandler(s.handleRPCMaxValue),
		"rpc.serverInfo": NewHandler(s.handleRPCServerInfo),
		"rpc.setLabel":   NewHandler(s.handleRPCSetLabel),
	}
}
