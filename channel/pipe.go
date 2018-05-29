package channel

import (
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Pipe creates a pair of connected in-memory raw channels.
func Pipe() (client, server jrpc2.Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = Raw(cr, cw)
	server = Raw(sr, sw)
	return
}
