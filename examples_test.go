package jrpc2_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

var (
	s *jrpc2.Server

	ctx      = context.Background()
	srv, cli = channel.Pipe(channel.Line)
	c        = jrpc2.NewClient(cli, nil)
)

func ExampleNewServer() {
	// Construct a new server with a single method "Hello".
	s = jrpc2.NewServer(jrpc2.MapAssigner{
		"Hello": jrpc2.NewMethod(func(ctx context.Context) (string, error) {
			return "Hello, world!", nil
		}),
	}, nil).Start(srv)

	// We can query the server for its current status information, including a
	// list of its methods.
	si := s.ServerInfo()

	fmt.Println(strings.Join(si.Methods, "\n"))
	// Output:
	// Hello
}

func ExampleClient_Call() {
	// var c = jrpc2.NewClient(cli, nil)
	p, err := c.Call(ctx, "Hello", nil)
	if err != nil {
		log.Fatalf("Call: %v", err)
	}
	rsp := p.Wait()
	if err := rsp.Error(); err != nil {
		log.Fatalf("Response: %v", err)
	}
	var msg string
	if err := rsp.UnmarshalResult(&msg); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_CallWait() {
	// var c = jrpc2.NewClient(cli, nil)
	rsp, err := c.CallWait(ctx, "Hello", nil)
	if err != nil {
		log.Fatalf("CallWait: %v", err)
	}
	var msg string
	if err := rsp.UnmarshalResult(&msg); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}
