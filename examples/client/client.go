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
	if rsp, err := cli.Call1("Math.Add", []int{1, 3, 5, 7}); err != nil {
		log.Fatal("Math.Add:", err)
	} else {
		var result int
		if err := rsp.UnmarshalResult(&result); err != nil {
			log.Fatal("UnmarshalResult:", err)
		}
		log.Printf("Math.Add result=%d", result)
	}

	// Divide by zero...
	if rsp, err := cli.Call1("Math.Div", struct{ X, Y int }{15, 0}); err != nil {
		log.Printf("Math.Div result=%v", err)
	} else {
		log.Fatalf("Math.Div succeeded somehow, producing %v", rsp)
	}

	// Send a batch of concurrent work...
	var reqs []*jrpc2.Request
	for i := 1; i <= 5; i++ {
		for j := 1; j <= 5; j++ {
			req, err := cli.Req("Math.Mul", struct{ X, Y int }{i, j})
			if err != nil {
				log.Fatalf("Req (%d*%d): %v", i, j, err)
			}
			reqs = append(reqs, req)
		}
	}
	ps, err := cli.Call(reqs...)
	if err != nil {
		log.Fatal("Call:", err)
	}
	for i, p := range ps {
		rsp, err := p.Wait()
		if err != nil {
			log.Printf("Req %d failed: %v", i+1, err)
			continue
		}
		var result int
		if err := rsp.UnmarshalResult(&result); err != nil {
			log.Printf("Req %d bad result: %v", i+1, err)
			continue
		}
		log.Printf("Req %d: result=%d", i+1, result)
	}
}
