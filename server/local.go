package server

import (
	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

// Local constructs a jrpc2.Server from the specified assigner and options.
// If opts == nil, it behaves as if the client and server options are also nil.
// When the client is closed, the server is also stopped.
func Local(assigner jrpc2.Assigner, opts *LocalOptions) *jrpc2.Client {
	if opts == nil {
		opts = new(LocalOptions)
	}
	cpipe, spipe := channel.Pipe(channel.Line)
	jrpc2.NewServer(assigner, opts.ServerOptions).Start(spipe)
	return jrpc2.NewClient(cpipe, opts.ClientOptions)
}

// LocalOptions control the behaviour of the server and client constructed by
// the Local function.
type LocalOptions struct {
	ClientOptions *jrpc2.ClientOptions
	ServerOptions *jrpc2.ServerOptions
}
