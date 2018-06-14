package server

import (
	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

// Local constructs a *jrpc2.Server and a *jrpc2.Client connected to it via an
// in-memory pipe, using the specified assigner and options.  If opts == nil,
// it behaves as if the client and server options are also nil.
//
// When the client is closed, the server is also stopped; the caller may invoke
// wait to wait for the server to complete.
func Local(assigner jrpc2.Assigner, opts *LocalOptions) (client *jrpc2.Client, wait func() error) {
	if opts == nil {
		opts = new(LocalOptions)
	}
	cpipe, spipe := channel.Pipe(channel.Varint)
	srv := jrpc2.NewServer(assigner, opts.ServerOptions).Start(spipe)
	return jrpc2.NewClient(cpipe, opts.ClientOptions), srv.Wait
}

// LocalOptions control the behaviour of the server and client constructed by
// the Local function.
type LocalOptions struct {
	ClientOptions *jrpc2.ClientOptions
	ServerOptions *jrpc2.ServerOptions
}
