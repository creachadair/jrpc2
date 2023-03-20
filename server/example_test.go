// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package server_test

import (
	"context"
	"fmt"
	"log"

	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/server"
)

func ExampleNewLocal() {
	loc := server.NewLocal(handler.Map{
		"Hello": handler.New(func(context.Context) (string, error) {
			return "Hello, world!", nil
		}),
	}, nil)
	defer loc.Close()

	var result string
	if err := loc.Client.CallResult(context.Background(), "Hello", nil, &result); err != nil {
		log.Fatalf("Call failed: %v", err)
	}
	fmt.Println(result)
	// Output:
	// Hello, world!
}
