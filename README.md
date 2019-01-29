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

## Implementation Notes

The JSON-RPC 2.0 spec is ambiguous about the semantics of batch requests. Specifically, the definition of notifications says:

> A Notification is a Request object without an "id" member.
> ...
> The Server MUST NOT reply to a Notification, including those that are within
> a batch request.
>
> Notifications are not confirmable by definition, since they do not have a
> Response object to be returned. As such, the Client would not be aware of any
> errors (like e.g. "Invalid params", "Internal error").

This conflicts with the definition of batch requests, which asserts:

> A Response object SHOULD exist for each Request object, except that there
> SHOULD NOT be any Response objects for notifications.
> ...
> The Response objects being returned from a batch call MAY be returned in any
> order within the Array.
> ...
> If the batch rpc call itself fails to be recognized as an valid JSON or as an
> Array with at least one value, the response from the Server MUST be a single
> Response object.

and includes examples that contain request values with no ID (which are, perforce, notifications) and report errors back to the client. Since order may not be relied upon, and there are no IDs, the client cannot correctly match such responses back to their originating requests.

This implementation resolves the conflict in favour of the notification rules. Specifically:

-  If a batch is empty or contains structurally invalid request or notification objects, the server reports error -32700 (Invalid JSON) as a single error Response object.

-  Otherwise, errors resulting from any request object without an ID are logged by the server but not reported to the client.
