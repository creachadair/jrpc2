// Package server provides support routines for running jrpc2 servers.
package server

import (
	"log"
	"net"
	"sync"

	"bitbucket.org/creachadair/jrpc2"
)

// Loop obtains connections from accept and starts a server for each with the
// given assigner and options, running in a new goroutine. If accept reports an
// error, the loop will terminate and the error will be reported once all the
// servers currently active have returned.
func Loop(accept func() (jrpc2.Conn, error), assigner jrpc2.Assigner, opts *jrpc2.ServerOptions) error {
	var wg sync.WaitGroup
	for {
		conn, err := accept()
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
			} else if err := srv.Wait(); err != nil {
				log.Printf("Server exit: %v", err)
			}
		}()
	}
}

// Listener adapts a net.Listener to an accept function for use with Loop.
func Listener(lst net.Listener) func() (jrpc2.Conn, error) {
	return func() (jrpc2.Conn, error) { return lst.Accept() }
}
