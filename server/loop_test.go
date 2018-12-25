package server

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

func mustListen(t *testing.T) net.Listener {
	t.Helper()

	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return lst
}

func TestLoop(t *testing.T) {
	lst := mustListen(t)
	defer lst.Close()
	addr := lst.Addr().String()
	newChan := channel.Varint

	// Start a bunch of clients, each of which will dial the server and make
	// some calls at random intervals to tickle the race detector.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Errorf("[client %d] Dialing %q: %v", i, addr, err)
				return
			}
			defer conn.Close()
			cli := jrpc2.NewClient(newChan(conn, conn), nil)
			defer cli.Close()

			for j := 0; j < 5; j++ {
				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
				var rsp string
				if err := cli.CallResult(context.Background(), "Test", nil, &rsp); err != nil {
					t.Errorf("[client %d]: Test call %d: unexpected error: %v", i, j+1, err)
				} else if rsp != "OK" {
					t.Errorf("[client %d]: Test call %d: got %q, want OK", i, j+1, rsp)
				}
			}
		}()
	}

	// Wait for the clients to be finished and then close the listener so that
	// the service loop will stop.
	go func() {
		wg.Wait()
		t.Log("Clients are finished; closing listener")
		lst.Close()
	}()

	// Start a server loop to accept connections from the clients. This should
	// exit cleanly once all the clients have finished and the listener closes.
	service := jrpc2.MapAssigner{
		"Test": jrpc2.NewHandler(func(context.Context) (string, error) {
			return "OK", nil
		}),
	}
	if err := Loop(lst, service, &LoopOptions{
		Framing: newChan,
	}); err != nil {
		t.Errorf("Loop: unexpected failure: %v", err)
	}
}
