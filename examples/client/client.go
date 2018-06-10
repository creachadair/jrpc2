// Program client demonstrates how to set up a JSON-RPC 2.0 client using the
// bitbucket.org/creachadair/jrpc2 package.
//
// Usage (communicates with the server example):
//
//   go build bitbucket.org/creachadair/jrpc2/examples/client
//   ./client -server :8080
//
package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"net"
	"sync"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/caller"
	"bitbucket.org/creachadair/jrpc2/channel"
)

var serverAddr = flag.String("server", "", "Server address")

var (
	// Reflective call wrappers for the remote methods.
	add = caller.New("Math.Add", caller.Options{
		Params:   int(0),
		Result:   int(0),
		Variadic: true,
	}).(func(context.Context, *jrpc2.Client, ...int) (int, error))
	div = caller.New("Math.Div", caller.Options{
		Params: binarg{},
		Result: float64(0),
	}).(func(context.Context, *jrpc2.Client, binarg) (float64, error))
	stat = caller.New("Math.Status", caller.Options{
		Result: "",
	}).(func(context.Context, *jrpc2.Client) (string, error))
)

type binarg struct{ X, Y int }

func intResult(rsp *jrpc2.Response) int {
	var v int
	if err := rsp.UnmarshalResult(&v); err != nil {
		log.Fatalln("UnmarshalResult:", err)
	}
	return v
}

func main() {
	flag.Parse()
	if *serverAddr == "" {
		log.Fatal("You must provide -server address to connect to")
	}

	conn, err := net.Dial("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("Dial %q: %v", *serverAddr, err)
	}
	log.Printf("Connected to %v", conn.RemoteAddr())

	// Start up the client, and enable logging to stderr.
	cli := jrpc2.NewClient(channel.RawJSON(conn, conn), nil)
	defer cli.Close()
	ctx := context.Background()

	log.Print("\n-- Sending a notification...")
	if err := cli.Notify(ctx, "Post.Alert", struct{ Msg string }{"There is a fire!"}); err != nil {
		log.Fatalln("Notify:", err)
	}

	log.Print("\n-- Sending some individual requests...")
	if sum, err := add(ctx, cli, 1, 3, 5, 7); err != nil {
		log.Fatalln("Math.Add:", err)
	} else {
		log.Printf("Math.Add result=%d", sum)
	}
	if quot, err := div(ctx, cli, binarg{82, 19}); err != nil {
		log.Fatalln("Math.Div:", err)
	} else {
		log.Printf("Math.Div result=%.3f", quot)
	}
	if s, err := stat(ctx, cli); err != nil {
		log.Fatalln("Math.Status:", err)
	} else {
		log.Printf("Math.Status result=%q", s)
	}

	// An error condition (division by zero)
	if quot, err := div(ctx, cli, binarg{15, 0}); err != nil {
		log.Printf("Math.Div err=%v", err)
	} else {
		log.Fatalf("Math.Div succeeded unexpectedly: result=%v", quot)
	}

	log.Print("\n-- Sending a batch of requests...")
	var specs []jrpc2.Spec
	for i := 1; i <= 5; i++ {
		x := rand.Intn(100)
		for j := 1; j <= 5; j++ {
			y := rand.Intn(100)
			specs = append(specs, jrpc2.Spec{
				Method: "Math.Mul",
				Params: struct{ X, Y int }{x, y},
			})
		}
	}
	batch, err := cli.Batch(ctx, specs)
	if err != nil {
		log.Fatalln("Batch:", err)
	}
	for i, rsp := range batch.Wait() {
		if err := rsp.Error(); err != nil {
			log.Printf("Req %q %s failed: %v", specs[i].Method, rsp.ID(), err)
			continue
		}
		log.Printf("Req %q %s: result=%d", specs[i].Method, rsp.ID(), intResult(rsp))
	}

	log.Print("\n-- Sending individual concurrent requests...")
	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		x := rand.Intn(100)
		for j := 1; j <= 5; j++ {
			y := rand.Intn(100)
			wg.Add(1)
			go func() {
				defer wg.Done()
				rsp, err := cli.Call(ctx, "Math.Sub", struct{ X, Y int }{x, y})
				if err != nil {
					log.Printf("Req (%d-%d) failed: %v", x, y, err)
					return
				}
				log.Printf("Req (%d - %d): result=%d", x, y, intResult(rsp))
			}()
		}
	}
	wg.Wait()
}
