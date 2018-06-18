package proxy

import (
	"context"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/server"
)

func TestProxy(t *testing.T) {
	const remoteAnswer = "the weasel has landed"

	// Set up a "remote" server that exports a method we can dispatch to.
	remote, cleanup := server.Local(jrpc2.MapAssigner{
		"Test": jrpc2.NewHandler(func(_ context.Context) (string, error) {
			return remoteAnswer, nil
		}),
	}, nil)
	defer cleanup()
	defer remote.Close()

	// Set up a "local" proxy to check the plumbing.
	local, cleanup := server.Local(New(remote), &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{
			DisableBuiltin: true,
		},
	})
	defer cleanup()
	defer local.Close()

	// Call through the proxy, and verify that we get back the answer from the
	// "remote" service.
	ctx := context.Background()
	var got string
	if rsp, err := local.Call(ctx, "Test", nil); err != nil {
		t.Errorf("Call(Test): unexpected error: %v", err)
	} else if err := rsp.UnmarshalResult(&got); err != nil {
		t.Errorf("Call(Test) result: unexpected error: %v", err)
	} else if got != remoteAnswer {
		t.Errorf("Call(Test): got %q, want %q", got, remoteAnswer)
	}
}
