package server_test

import (
	"context"
	"fmt"
	"log"

	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/server"
)

func ExampleLocal() {
	cli, wait := server.Local(handler.Map{
		"Hello": handler.New(func(context.Context) (string, error) {
			return "Hello, world!", nil
		}),
	}, nil)
	defer wait()

	var result string
	if err := cli.CallResult(context.Background(), "Hello", nil, &result); err != nil {
		log.Fatalf("Call failed: %v", err)
	}
	cli.Close()
	fmt.Println(result)
	// Output:
	// Hello, world!
}
