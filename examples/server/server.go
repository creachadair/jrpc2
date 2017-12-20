// Program server demonstrates how to set up a JSON-RPC 2.0 server using the
// bitbucket.org/creachadair/jrpc2 package.
//
// Usage (see also the client example):
//
//   go build bitbucket.org/creachadair/jrpc2/examples/server
//   ./server -port 8080
//
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"bitbucket.org/creachadair/jrpc2"
)

// The math type defines several arithmetic methods we can expose via the
// service. The exported methods having appropriate types can be automatically
// exported by jrpc2.NewService.
type math struct{}

// A binop is carries a pair of integers for use as parameters.
type binop struct {
	X, Y int
}

func (math) Add(ctx context.Context, vs []int) (int, error) {
	sum := 0
	for _, v := range vs {
		sum += v
	}
	return sum, nil
}

func (math) Sub(ctx context.Context, arg binop) (int, error) {
	return arg.X - arg.Y, nil
}

func (math) Mul(ctx context.Context, arg binop) (int, error) {
	return arg.X * arg.Y, nil
}

func (math) Div(ctx context.Context, arg binop) (float64, error) {
	if arg.Y == 0 {
		return 0, jrpc2.Errorf(jrpc2.E_InvalidParams, "zero divisor")
	}
	return float64(arg.X) / float64(arg.Y), nil
}

type alert struct {
	Msg string
}

func Alert(ctx context.Context, a alert) (bool, error) {
	log.Printf("[ALERT]: %s", a.Msg)
	return false, nil // return values are ignored for notifications
}

var (
	port     = flag.Int("port", 0, "Service port")
	maxTasks = flag.Int("max", 1, "Maximum concurrent tasks")
)

func main() {
	flag.Parse()
	if *port <= 0 {
		log.Fatal("You must provide a positive -port to listen on")
	}

	// Bind the methods of the math type to an assigner.
	mux := jrpc2.ServiceMapper{
		"Math": jrpc2.NewService(math{}),
		"Post": jrpc2.MapAssigner{"Alert": jrpc2.NewMethod(Alert)},
	}

	lst, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	if err != nil {
		log.Fatalln("Listen:", err)
	}
	log.Printf("Listening at %v...", lst.Addr())

	srv := jrpc2.NewServer(mux, &jrpc2.ServerOptions{
		LogWriter:   os.Stderr,
		Concurrency: *maxTasks,
	})
	for {
		conn, err := lst.Accept()
		if err != nil {
			log.Fatalln("Accept:", err)
		}
		log.Printf("New connection from %v", conn.RemoteAddr())

		// Start up the server, and enable logging to stderr.
		if _, err := srv.Start(conn); err != nil {
			log.Fatalln("Start:", err)
		}
		log.Print("<serving requests>")
		log.Printf("Server finished (err=%v)", srv.Wait())
	}
}
