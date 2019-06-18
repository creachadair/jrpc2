package server

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
)

func TestLocal(t *testing.T) {
	loc := NewLocal(make(handler.Map), &LocalOptions{
		Client: &jrpc2.ClientOptions{
			Logger: log.New(os.Stderr, "[local client] ", 0),
		},
		Server: &jrpc2.ServerOptions{
			Logger: log.New(os.Stderr, "[local server] ", 0),
		},
	})

	ctx := context.Background()
	si, err := jrpc2.RPCServerInfo(ctx, loc.Client)
	if err != nil {
		t.Fatalf("rpc.serverInfo failed: %v", err)
	}

	// A couple sanity checks on the server info.
	if nr := si.Counter["rpc.requests"]; nr != 1 {
		t.Errorf("rpc.serverInfo reports %d requests, wanted 1", nr)
	}
	if len(si.Methods) != 0 {
		t.Errorf("rpc.serverInfo reports methods %+q, wanted []", si.Methods)
	}

	// Close the client and wait for the server to stop.
	if err := loc.Close(); err != nil {
		t.Errorf("Server wait: got %v, want nil", err)
	}
}
