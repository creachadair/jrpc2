package server

import (
	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

// Local constructs a jrpc2.Server from the specified assigner and server
// options, and connects an in-memory client to it with the client options.
func Local(assigner jrpc2.Assigner, serverOpt *jrpc2.ServerOptions, clientOpt *jrpc2.ClientOptions) *jrpc2.Client {
	cpipe, spipe := channel.Pipe(channel.Line)
	jrpc2.NewServer(assigner, serverOpt).Start(spipe)
	return jrpc2.NewClient(cpipe, clientOpt)
}
