package jrpc2_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"
)

var (
	s *jrpc2.Server

	ctx      = context.Background()
	sch, cch = channel.Direct()
	cli      = jrpc2.NewClient(cch, nil)
)

type Msg struct {
	Text string `json:"msg"`
}

func ExampleNewServer() {
	// Construct a new server with a single method "Hello".
	s = jrpc2.NewServer(handler.Map{
		"Hello": handler.New(func(ctx context.Context) string {
			return "Hello, world!"
		}),
		"Log": handler.New(func(ctx context.Context, msg Msg) (bool, error) {
			fmt.Println("Log:", msg.Text)
			return true, nil
		}),
	}, nil).Start(sch)

	// We can query the server for its current status information, including a
	// list of its methods.
	si := s.ServerInfo()

	fmt.Println(strings.Join(si.Methods, "\n"))
	// Output:
	// Hello
	// Log
}

func ExampleClient_Call() {
	// var cli = jrpc2.NewClient(cch, nil)
	rsp, err := cli.Call(ctx, "Hello", nil)
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
	// var cli = jrpc2.NewClient(cch, nil)
	var msg string
	if err := cli.CallResult(ctx, "Hello", nil, &msg); err != nil {
		log.Fatalf("CallResult: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_Batch() {
	// var cli = jrpc2.NewClient(cch, nil)
	rsps, err := cli.Batch(ctx, []jrpc2.Spec{
		{Method: "Hello"},
		{Method: "Log", Params: Msg{"Sing it!"}, Notify: true},
	})
	if err != nil {
		log.Fatalf("Batch: %v", err)
	}

	fmt.Printf("len(rsps) = %d\n", len(rsps))
	for i, rsp := range rsps {
		var msg string
		if err := rsp.UnmarshalResult(&msg); err != nil {
			log.Fatalf("Invalid result: %v", err)
		}
		fmt.Printf("Response #%d: %s\n", i+1, msg)
	}
	// Output:
	// Log: Sing it!
	// len(rsps) = 1
	// Response #1: Hello, world!
}
