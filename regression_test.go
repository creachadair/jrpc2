package jrpc2_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/server"
)

// Verify that a notification handler will not deadlock with the dispatcher on
// holding the server lock. See: https://github.com/creachadair/jrpc2/issues/27
func TestLockRaceRegression(t *testing.T) {
	hdone := make(chan struct{})
	local := server.NewLocal(handler.Map{
		// Do some busy-work and then try to get the server lock, in this case
		// via the CancelRequest helper.
		"Kill": handler.New(func(ctx context.Context, req *jrpc2.Request) error {
			defer close(hdone) // signal we passed the deadlock point

			var id string
			if err := req.UnmarshalParams(&handler.Args{&id}); err != nil {
				return err
			}
			jrpc2.CancelRequest(ctx, id)
			return nil
		}),

		// Block indefinitely, just to give the dispatcher something to do.
		"Stall": handler.New(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
	}, nil)
	defer local.Close()

	ctx := context.Background()
	local.Client.Notify(ctx, "Kill", handler.Args{"1"})

	go local.Client.Call(ctx, "Stall", nil)

	select {
	case <-time.After(10 * time.Second):
		t.Fatal("Notification handler is probably deadlocked")
	case <-hdone:
		t.Log("Notification handler completed successfully")
	}
}

// Verify that if a callback handler panics, the client will report an error
// back to the server. See https://github.com/creachadair/jrpc2/issues/41.
func TestOnCallbackPanicRegression(t *testing.T) {
	const panicString = "the devil you say"

	loc := server.NewLocal(handler.Map{
		"Test": handler.New(func(ctx context.Context) error {
			rsp, err := jrpc2.PushCall(ctx, "Poke", nil)
			if err == nil {
				t.Errorf("Callback unexpectedly succeeded: %#q", rsp.ResultString())
			} else if !strings.HasSuffix(err.Error(), panicString) {
				t.Errorf("Callback reported unexpected error: %v", err)
			} else {
				t.Logf("Callback reported expected error: %v", err)
			}
			return nil
		}),
	}, &server.LocalOptions{
		Server: &jrpc2.ServerOptions{
			AllowPush: true,
		},
		Client: &jrpc2.ClientOptions{
			OnCallback: func(ctx context.Context, req *jrpc2.Request) (interface{}, error) {
				t.Log("Entering callback handler; about to panic")
				panic(panicString)
			},
		},
	})
	defer loc.Close()

	if _, err := loc.Client.Call(context.Background(), "Test", nil); err != nil {
		t.Errorf("Call unexpectedly failed: %v", err)
	}
}
