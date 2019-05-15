package jrpc2_test

import (
	"context"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/server"
)

func BenchmarkRoundTrip(b *testing.B) {
	// Benchmark the round-trip call cycle for a method that does no useful
	// work, as a proxy for overhead for client and server maintenance.
	loc := server.NewLocal(handler.Map{
		"void": handler.New(func(context.Context, *jrpc2.Request) (interface{}, error) {
			return nil, nil
		}),
	}, &server.LocalOptions{
		Server: &jrpc2.ServerOptions{
			DisableBuiltin: true,
			Concurrency:    1,
		},
	})
	defer loc.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loc.Client.Call(ctx, "void", nil); err != nil {
			b.Fatalf("Call void failed: %v", err)
		}
	}
}
