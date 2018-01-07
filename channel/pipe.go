package channel

import (
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Pipe creates a pair of connected in-memory raw channels.
func Pipe() (client, server jrpc2.Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = NewRaw(pipe{cr, cw})
	server = NewRaw(pipe{sr, sw})
	return
}

type pipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (p pipe) Close() error {
	p.PipeReader.Close()
	return p.PipeWriter.Close()
}
