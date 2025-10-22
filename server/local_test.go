// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package server_test

import (
	"context"
	"flag"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/server"
)

var doDebug = flag.Bool("debug", false, "Enable server and client debugging logs")

func testOpts(t *testing.T) *server.LocalOptions {
	if !*doDebug {
		return nil
	}
	return &server.LocalOptions{
		Client: &jrpc2.ClientOptions{Logger: func(s string) { t.Log(s) }},
		Server: &jrpc2.ServerOptions{Logger: func(s string) { t.Log(s) }},
	}
}

func TestLocal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		loc := server.NewLocal(make(handler.Map), testOpts(t))
		var si jrpc2.ServerInfo
		if err := loc.Client.CallResult(t.Context(), "rpc.serverInfo", nil, &si); err != nil {
			t.Fatalf("rpc.serverInfo failed: %v", err)
		}

		// A couple coherence checks on the server info.
		if nr, ok := si.Metrics["rpc_requests"].(float64); !ok {
			t.Fatalf("rpc.serverInfo does not have rpc_requests: %[1]T %[1]v", si.Metrics["rpc_requests"])
		} else if nr <= 0 {
			t.Errorf("rpc.serverInfo reports %v requests, wanted 1 or more", nr)
		}
		if len(si.Methods) != 0 {
			t.Errorf("rpc.serverInfo reports methods %+q, wanted []", si.Methods)
		}

		// Close the client and wait for the server to stop.
		if err := loc.Close(); err != nil {
			t.Errorf("Server wait: got %v, want nil", err)
		}
	})
}

// Test that concurrent callers to a local service do not deadlock.
func TestLocalConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		loc := server.NewLocal(handler.Map{
			"Test": handler.New(func(context.Context) error { return nil }),
		}, testOpts(t))

		const numCallers = 20

		var wg sync.WaitGroup
		for i := range numCallers {
			wg.Go(func() {
				_, err := loc.Client.Call(t.Context(), "Test", nil)
				if err != nil {
					t.Errorf("Caller %d failed: %v", i, err)
				}
			})
		}
		wg.Wait()
		if err := loc.Close(); err != nil {
			t.Errorf("Server close: %v", err)
		}
	})
}
