// Package server provides support routines for running jrpc2 servers.
package server

import (
	"io"
	"log"
	"net"
	"sync"

	"bitbucket.org/creachadair/jrpc2"
)

// Loop obtains connections from lst and starts a server for each with the
// given assigner and options, running in a new goroutine. If accept reports an
// error, the loop will terminate and the error will be reported once all the
// servers currently active have returned.
func Loop(lst net.Listener, assigner jrpc2.Assigner, opts *jrpc2.ServerOptions) error {
	var wg sync.WaitGroup
	for {
		conn, err := lst.Accept()
		if err != nil {
			log.Printf("Error accepting new connection: %v", err)
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, err := jrpc2.NewServer(assigner, opts).Start(conn)
			if err != nil {
				log.Printf("Error starting server: %v", err)
			} else if err := srv.Wait(); err != nil && err != io.EOF {
				log.Printf("Server exit: %v", err)
			}
		}()
	}
}
