package jrpc2_test

import (
	"context"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/jctx"
	"bitbucket.org/creachadair/jrpc2/server"
)

func BenchmarkRoundTrip(b *testing.B) {
	// Benchmark the round-trip call cycle for a method that does no useful
	// work, as a proxy for overhead for client and server maintenance.
	voidService := handler.Map{
		"void": handler.Func(func(context.Context, *jrpc2.Request) (interface{}, error) {
			return nil, nil
		}),
	}
	ctxClient := &jrpc2.ClientOptions{EncodeContext: jctx.Encode}
	tests := []struct {
		desc string
		cli  *jrpc2.ClientOptions
		srv  *jrpc2.ServerOptions
	}{
		{"C01-CTX-B", nil, &jrpc2.ServerOptions{DisableBuiltin: true, Concurrency: 1}},
		{"C01-CTX+B", nil, &jrpc2.ServerOptions{Concurrency: 1}},
		{"C04-CTX-B", nil, &jrpc2.ServerOptions{DisableBuiltin: true, Concurrency: 4}},
		{"C04-CTX+B", nil, &jrpc2.ServerOptions{Concurrency: 4}},
		{"C12-CTX-B", nil, &jrpc2.ServerOptions{DisableBuiltin: true, Concurrency: 12}},
		{"C12-CTX+B", nil, &jrpc2.ServerOptions{Concurrency: 12}},

		{"C01+CTX-B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, DisableBuiltin: true, Concurrency: 1},
		},
		{"C01+CTX+B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, Concurrency: 1},
		},
		{"C04+CTX-B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, DisableBuiltin: true, Concurrency: 4},
		},
		{"C04+CTX+B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, Concurrency: 4},
		},
		{"C12+CTX-B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, DisableBuiltin: true, Concurrency: 4},
		},
		{"C12+CTX+B", ctxClient,
			&jrpc2.ServerOptions{DecodeContext: jctx.Decode, Concurrency: 12},
		},
	}
	for _, test := range tests {
		b.Run(test.desc, func(b *testing.B) {
			loc := server.NewLocal(voidService, &server.LocalOptions{
				Client: test.cli,
				Server: test.srv,
			})
			defer loc.Close()
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := loc.Client.Call(ctx, "void", nil); err != nil {
					b.Fatalf("Call void failed: %v", err)
				}
			}
		})
	}
}
