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

func add(cli *jrpc2.Client, vs ...int) (int, error) {
	rsp, err := cli.Call("Math.Add", vs)
	if err != nil {
		return 0, err
	}
	var sum int
	if err := rsp.UnmarshalResult(&sum); err != nil {
		return 0, err
	}
	return sum, nil
}

func div(cli *jrpc2.Client, x, y int) (float64, error) {
	rsp, err := cli.Call("Math.Div", struct{ X, Y int }{x, y})
	if err != nil {
		return 0, err
	}
	var quotient float64
	if err := rsp.UnmarshalResult(&quotient); err != nil {
		return 0, err
	}
	return quotient, nil
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
	cli := jrpc2.NewClient(conn, jrpc2.ClientLog(os.Stderr))
	defer cli.Close()

	// Add some numbers...
	if sum, err := add(cli, 1, 3, 5, 7); err != nil {
		log.Fatal("Math.Add:", err)
	} else {
		log.Printf("Math.Add result=%d", sum)
	}

	// Divide by zero...
	if quot, err := div(cli, 15, 0); err != nil {
		log.Printf("Math.Div err=%v", err)
	} else {
		log.Fatalf("Math.Div succeeded unexpectedly: result=%v", quot)
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
	ps, err := cli.Send(reqs...)
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

	// Send a notification...
	if err := cli.Notify("Post.Alert", struct{ Msg string }{"There is a fire!"}); err != nil {
		log.Fatal("Notify:", err)
	}
}
