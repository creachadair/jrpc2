# jrpc2

http://godoc.org/bitbucket.org/creachadair/jrpc2

[![Go Report Card](https://goreportcard.com/badge/bitbucket.org/creachadair/jrpc2)](https://goreportcard.com/report/bitbucket.org/creachadair/jrpc2)

This repository provides Go package that implements a [JSON-RPC 2.0][spec] client and server.

## Packages

*  Package [jrpc2](http://godoc.org/bitbucket.org/creachadair/jrpc2) implements the base client and server.

*  Package [caller](http://godoc.org/bitbucket.org/creachadair/jrpc2/caller) provides a function to construct client call wrappers.

*  Package [channel](http://godoc.org/bitbucket.org/creachadair/jrpc2/channel) defines the communication channel abstractioon used by the server & client.

*  Package [code](http://godoc.org/bitbucket.org/creachadair/jrpc2/code) defines standard error codes as defined by the JSON-RPC 2.0 protocol.

*  Package [handler](http://godoc.org/bitbucket.org/creachadair/jrpc2/handler) defines support for adapting functions to service methods.

*  Package [jctx](http://godoc.org/bitbucket.org/creachadair/jrpc2/jctx) implements an encoder and decoder for request context values, allowing context metadata to be propagated through JSON-RPC requests.

*  Package [metrics](http://godoc.org/bitbucket.org/creachadair/jrpc2/metrics) defines a server metrics collector.

*  Package [proxy](http://godoc.org/bitbucket.org/creachadair/jrpc2/proxy) defines a transparent proxy that allows a connected client to be re-exported as a server.

*  Package [server](http://godoc.org/bitbucket.org/creachadair/jrpc2/server) provides support for running a server to handle multiple connections, and an in-memory implementation for testing.

[spec]: http://www.jsonrpc.org/specification
