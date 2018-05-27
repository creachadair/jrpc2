// Program adder demonstrates a trivial JSON-RPC server that communicates over
// the process's stdin and stdout.
//
// Usage:
//    $ go run adder.go
//
// Queries to try (copy and paste):
//    {"jsonrpc":"2.0", "id":1, "method":"Add", "params":[1,2,3]}
//    {"jsonrpc":"2.0", "id":2, "method":"rpc.serverInfo"}
//
package main

import (
	"context"
	"log"
	"os"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

// This function will be exported as a method named "Add".
func Add(ctx context.Context, vs ...int) (int, error) {
	sum := 0
	for _, v := range vs {
		sum += v
	}
	return sum, nil
}

func main() {
	// Set up the server to respond to "Add" by calling the add function.
	s := jrpc2.NewServer(jrpc2.MapAssigner{
		"Add": jrpc2.NewMethod(Add),
	}, nil)

	// Start the server on a channel comprising stdin/stdout.
	s.Start(channel.Line(os.Stdin, os.Stdout))
	log.Print("Server started")

	// Wait for the server to exit, and report any errors.
	if err := s.Wait(); err != nil {
		log.Printf("Server exited: %v", err)
	}
}
