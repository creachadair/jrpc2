package server

import (
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Local constructs a jrpc2.Server from the specified assigner and server
// options, and connects an in-memory client to it with the client options.
func Local(assigner jrpc2.Assigner, serverOpt *jrpc2.ServerOptions, clientOpt *jrpc2.ClientOptions) *jrpc2.Client {
	cpipe, spipe := newPipe()
	jrpc2.NewServer(assigner, serverOpt).Start(spipe)
	return jrpc2.NewClient(cpipe, clientOpt)
}

// newPipe creates a pair of connected jrpc2.Conn values suitable for wiring
// together an in-memory client and server. The resulting values are safe for
// concurrent use by multiple goroutines.
func newPipe() (client, server jrpc2.Conn) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	return pipe{PipeReader: cr, PipeWriter: cw}, pipe{PipeReader: sr, PipeWriter: sw}
}

type pipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (p pipe) Close() error {
	rerr := p.PipeReader.Close()
	werr := p.PipeWriter.Close()
	if werr != nil {
		return werr
	}
	return rerr
}
