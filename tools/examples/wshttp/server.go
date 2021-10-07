// Program wshttp demonstrates how to set up a JSON-RPC 20 server using
// the github.com/creachadair/jrpc2 package with a Websocket transport.
//
// Usage:
//   go build github.com/creachadair/jrpc2/tools/examples/wshttp
//   ./wshttp -listen :8080
//
// The server accepts RPC connections on ws://<address>/rpc.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/wschannel"
)

var listenAddr = flag.String("listen", "", "Service address")

func main() {
	flag.Parse()
	if *listenAddr == "" {
		log.Fatal("You must provide a non-empty -listen address")
	}

	lst := wschannel.NewListener(nil)
	hs := &http.Server{Addr: *listenAddr, Handler: http.DefaultServeMux}
	http.Handle("/rpc", lst)
	go hs.ListenAndServe()

	srv := jrpc2.NewServer(handler.Map{
		"Reverse": handler.New(func(_ context.Context, ss []string) []string {
			for i, j := 0, len(ss)-1; i < j; i++ {
				ss[i], ss[j] = ss[j], ss[i]
				j--
			}
			return ss
		}),
	}, nil)

	ctx := context.Background()
	for {
		ch, err := lst.Accept(ctx)
		if err != nil {
			hs.Shutdown(ctx)
			log.Fatalf("Accept: %v", err)
		}
		log.Print("Client connected")
		if err := srv.Start(ch).Wait(); err != nil {
			log.Printf("Server error: %v", err)
		}
		log.Print("Client disconnected (wave bye)")
	}
}
