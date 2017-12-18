// Program client demonstrates how to set up a JSON-RPC 2.0 client using the
// bitbucket.org/creachadair/jrpc2 package.
//
// Usage:
//   go run exmamples/client/client.go -server localhost:8080
//
package main

import (
	"flag"
	"log"
	"net"
	"os"

	"bitbucket.org/creachadair/jrpc2"
)

var serverAddr = flag.String("server", "", "Server address")

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
	cli := jrpc2.NewClient(conn, jrpc2.ClientLog(os.Stderr))
	defer cli.Close()

	// Add some numbers...
	if rsp, err := cli.Call1("Add", []int{1, 3, 5, 7}); err != nil {
		log.Fatal("Add:", err)
	} else {
		var result int
		if err := rsp.UnmarshalResult(&result); err != nil {
			log.Fatal("UnmarshalResult:", err)
		}
		log.Printf("Add result=%d", result)
	}

	// Divide by zero...
	if rsp, err := cli.Call1("Div", struct{ X, Y int }{15, 0}); err != nil {
		log.Printf("Div result=%v", err)
	} else {
		log.Fatalf("Div succeeded somehow, producing %v", rsp)
	}
}
