package proxy

import (
	"context"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/server"
)

func TestProxy(t *testing.T) {
	const remoteAnswer = "the weasel has landed"

	// Set up a "remote" server that exports a method we can dispatch to.
	remote := server.NewLocal(handler.Map{
		"Test": handler.New(func(_ context.Context, vs []int) (string, error) {
			return remoteAnswer, nil
		}),
	}, nil)
	defer remote.Close()

	// Set up a "local" proxy to check the plumbing.
	local := server.NewLocal(New(remote.Client), &server.LocalOptions{
		ServerOptions: &jrpc2.ServerOptions{
			DisableBuiltin: true,
		},
	})
	defer local.Close()

	// Call through the proxy, and verify that we get back the answer from the
	// "remote" service.
	ctx := context.Background()
	var got string
	if rsp, err := local.Client.Call(ctx, "Test", []int{1, 2, 3}); err != nil {
		t.Errorf("Call(Test): unexpected error: %v", err)
	} else if err := rsp.UnmarshalResult(&got); err != nil {
		t.Errorf("Call(Test) result: unexpected error: %v", err)
	} else if got != remoteAnswer {
		t.Errorf("Call(Test): got %q, want %q", got, remoteAnswer)
	}
}
