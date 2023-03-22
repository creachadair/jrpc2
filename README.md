# jrpc2

[![GoDoc](https://img.shields.io/static/v1?label=godoc&message=reference&color=yellow)](https://pkg.go.dev/github.com/creachadair/jrpc2)

This repository provides a Go module that implements a [JSON-RPC 2.0][spec] client and server.
There is also a working [example in the Go playground](https://go.dev/play/p/Nhg0smrxsoM).

## Packages

*  Package [jrpc2](http://godoc.org/github.com/creachadair/jrpc2) implements the base client and server and standard error codes.

*  Package [channel](http://godoc.org/github.com/creachadair/jrpc2/channel) defines the communication channel abstraction used by the server & client.

*  Package [handler](http://godoc.org/github.com/creachadair/jrpc2/handler) defines support for adapting functions to service methods.

*  Package [jhttp](http://godoc.org/github.com/creachadair/jrpc2/jhttp) allows clients and servers to use HTTP as a transport.

*  Package [server](http://godoc.org/github.com/creachadair/jrpc2/server) provides support for running a server to handle multiple connections, and an in-memory implementation for testing.

[spec]: http://www.jsonrpc.org/specification

### Versioning

From v1.0.0 onward, the API of this module is considered stable, and I intend to merge no breaking changes to the API without increasing the major version number. Following the conventions of semantic versioning, the minor version will be used to signify the presence of backward-compatible new features (for example, new methods, options, or types), while the patch version will be reserved for bug fixes, documentation updates, and other changes that do not modify the API surface.

Note, however, that this intent is limited to the package APIs as seen by the Go compiler: Changes to the implementation that change observable behaviour in ways not promised by the documentation, e.g., changing performance characteristics or the order of internal operations, are not protected. Breakage that results from reliance on undocumented side-effects of the current implementation are the caller's responsibility.

## Implementation Notes

The following describes some of the implementation choices made by this module.

### Batch requests and error reporting

The JSON-RPC 2.0 spec is ambiguous about the semantics of batch requests. Specifically, the definition of notifications says:

> A Notification is a Request object without an "id" member.
> ...
> The Server MUST NOT reply to a Notification, including those that are within a batch request.
>
> Notifications are not confirmable by definition, since they do not have a Response object to be returned. As such, the Client would not be aware of any errors (like e.g. "Invalid params", "Internal error").

This conflicts with the definition of batch requests, which asserts:

> A Response object SHOULD exist for each Request object, except that there SHOULD NOT be any Response objects for notifications.
> ...
> The Response objects being returned from a batch call MAY be returned in any order within the Array.
> ...
> If the batch rpc call itself fails to be recognized as an valid JSON or as an Array with at least one value, the response from the Server MUST be a single Response object.

and includes examples that contain request values with no ID (which are, perforce, notifications) and report errors back to the client. Since order may not be relied upon, and there are no IDs, the client cannot correctly match such responses back to their originating requests.

This implementation resolves the conflict in favour of the batch rules. Specifically:

-  If a batch is empty or not valid JSON, the server reports error -32700 (Invalid JSON) as a single error Response object.

-  Otherwise, parse or validation errors resulting from any batch member without an ID are mapped to error objects with a `null` ID, in the same position in the reply as the corresponding request. Preservation of order is not required by the specification, but it ensures the server has stable behaviour.

Because a server is allowed to reorder the results, a client should not depend on this implementation detail.
