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

var newChan = channel.Varint

func mustListen(t *testing.T) net.Listener {
	t.Helper()

	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return lst
}

func mustDial(t *testing.T, addr string) *jrpc2.Client {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial %q: %v", addr, err)
	}
	return jrpc2.NewClient(newChan(conn, conn), nil)
}

func mustServe(t *testing.T, lst net.Listener) <-chan struct{} {
	t.Helper()

	sc := make(chan struct{})
	go func() {
		defer close(sc)
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
	}()
	return sc
}

// Test that sequential clients against the same server work sanely.
func TestSeq(t *testing.T) {
	lst := mustListen(t)
	addr := lst.Addr().String()
	sc := mustServe(t, lst)

	for i := 0; i < 5; i++ {
		cli := mustDial(t, addr)
		var rsp string
		if err := cli.CallResult(context.Background(), "Test", nil, &rsp); err != nil {
			t.Errorf("[client %d] Test call: unexpected error: %v", i, err)
		} else if rsp != "OK" {
			t.Errorf("[client %d]: Test call: got %q, want OK", i, rsp)
		}
		cli.Close()
	}
	lst.Close()
	<-sc
}

// Test that concurrent clients against the same server work sanely.
func TestLoop(t *testing.T) {
	lst := mustListen(t)
	defer lst.Close()
	addr := lst.Addr().String()
	sc := mustServe(t, lst)

	// Start a bunch of clients, each of which will dial the server and make
	// some calls at random intervals to tickle the race detector.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			cli := mustDial(t, addr)
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
	wg.Wait()
	t.Log("Clients are finished; closing listener")
	lst.Close()
	<-sc
}
