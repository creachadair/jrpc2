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
	"bitbucket.org/creachadair/jrpc2/server"
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

func (math) Status(context.Context) (string, error) { return "OK", nil }

type alert struct {
	Msg string
}

// Alert implements a notification handler that logs its argument.
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
	server.Loop(lst, mux, &server.LoopOptions{
		ServerOptions: &jrpc2.ServerOptions{
			Logger:      log.New(os.Stderr, "[jrpc2.Server] ", log.LstdFlags|log.Lshortfile),
			Concurrency: *maxTasks,
			Metrics:     jrpc2.NewMetrics(),
		},
	})
}
