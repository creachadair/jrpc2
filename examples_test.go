package jrpc2_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/handler"
)

var (
	s *jrpc2.Server

	ctx      = context.Background()
	srv, cli = channel.Pipe(channel.Line)
	c        = jrpc2.NewClient(cli, nil)
)

type Msg struct {
	Text string `json:"msg"`
}

func ExampleNewServer() {
	// Construct a new server with a single method "Hello".
	s = jrpc2.NewServer(handler.Map{
		"Hello": handler.New(func(ctx context.Context) (string, error) {
			return "Hello, world!", nil
		}),
		"Log": handler.New(func(ctx context.Context, msg Msg) (bool, error) {
			fmt.Println("Log:", msg.Text)
			return true, nil
		}),
	}, nil).Start(srv)

	// We can query the server for its current status information, including a
	// list of its methods.
	si := s.ServerInfo()

	fmt.Println(strings.Join(si.Methods, "\n"))
	// Output:
	// Hello
	// Log
}

func ExampleClient_Call() {
	// var c = jrpc2.NewClient(cli, nil)
	rsp, err := c.Call(ctx, "Hello", nil)
	if err != nil {
		log.Fatalf("Call: %v", err)
	}
	var msg string
	if err := rsp.UnmarshalResult(&msg); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_CallResult() {
	// var c = jrpc2.NewClient(cli, nil)
	var msg string
	if err := c.CallResult(ctx, "Hello", nil, &msg); err != nil {
		log.Fatalf("CallResult: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_Batch() {
	// var c = jrpc2.NewClient(cli, nil)
	rsps, err := c.Batch(ctx, []jrpc2.Spec{
		{Method: "Hello"},
		{Method: "Log", Params: Msg{"Sing it!"}, Notify: true},
	})
	if err != nil {
		log.Fatalf("Batch: %v", err)
	}

	// There should be only one reply in this case, since we sent 1
	// notification and 1 request.
	if len(rsps) != 1 {
		log.Fatalf("Wait: got %d responses, wanted 1", len(rsps))
	}
	fmt.Printf("len(rsps)=%d\n", len(rsps))

	// Decode the result from the request.
	var msg string
	if err := rsps[0].UnmarshalResult(&msg); err != nil {
		log.Fatalf("Invalid result: %v", err)
	}
	fmt.Printf("rsps[0]=%s\n", msg)
	// Output:
	// Log: Sing it!
	// len(rsps)=1
	// rsps[0]=Hello, world!
}
