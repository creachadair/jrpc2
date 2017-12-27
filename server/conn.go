package server

import (
	"net"

	"bitbucket.org/creachadair/jrpc2"
)

// Listener adapts a net.Listener to an accept function for use with Loop.
func Listener(lst net.Listener) func() (jrpc2.Conn, error) {
	return func() (jrpc2.Conn, error) { return lst.Accept() }
}
