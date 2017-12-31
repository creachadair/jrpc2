// Program http demonstrates how to set up a JSON-RPC server to handle requests
// delivered via HTTP, using the server.HTTP adapter.
//
// Usage:
//
//    go build bitbucket.org/creachadair/jrpc2/examples/http
//    ./http -port 8080
//
// Test query:
//    curl -v -H 'Content-Type: application/json' --data-binary '{
//      "jsonrpc":"2.0",
//      "id": 1,
//      "method": "Sort",
//      "params": [13, 17, 2, 5, 3, 11, 7, 23, 19]
//    }' http://localhost:8080/call
//
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/server"
)

var port = flag.Int("port", 0, "Service port")

func main() {
	flag.Parse()
	if *port <= 0 {
		log.Fatal("You must provide a positive -port to listen on")
	}

	cli := server.Local(jrpc2.MapAssigner{"Sort": jrpc2.NewMethod(Sort)}, nil, nil)
	http.Handle("/call", server.HTTP(cli))

	lst, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalln("listen:", err)
	}
	if err := http.Serve(lst, nil); err != nil {
		log.Fatalln("HTTP serve:", err)
	}
}

func Sort(ctx context.Context, vals []int) ([]int, error) {
	sort.Ints(vals)
	return vals, nil
}
