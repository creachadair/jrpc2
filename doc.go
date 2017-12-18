/*
Package jrpc2 implements a server and a client for the JSON-RPC 2.0 protocol
defined by http://www.jsonrpc.org/specification.

Servers

The *Server type implements a JSON-RPC server. A server communicates with a
client over a Conn, and dispatches client requests to user-defined handlers
dispatched by an Assigner. For example, suppose we have defined the following
Add function:

   // Add returns the sum of a slice of integers.
   func Add(ctx context.Context, values []int) (int, error) {
      sum := 0
      for _, v := range values {
         sum += v
      }
      return sum, nil
   }

The server uses an Assigner to locate the implementation of its methods.  For
this example, let's advertise this function under the name "Math.Add".  For
static assignments, we can use a jrpc2.MapAssigner, which finds methods by
looking them up in a Go map:

   import "bitbucket.org/creachadair/jrpc2"

   var assigner = jrpc2.MapAssigner{
      "Math.Add": jrpc2.NewMethod(Add),
   }

Equipped with an Assigner we can construct a Server:

   srv := jrpc2.NewServer(assigner)

Now we need a connection to serve requests on. A net.Conn will do, so let's say
for example:

   import "net"

   inc, err := net.Listen("tcp", ":8080")
   ...
   conn, err := inc.Accept()
   ...
   srv.Start(conn)

The running server will handle incoming requests until the connection fails or
until it is stopped (by calling srv.Stop()). To wait for the server to finish,

   err := srv.Wait()

This will report the error that led to the server exiting.

Clients

The *Client type implements a JSON-RPC client. A client communicates with a
server over a Conn. The client is safe for concurrent use by multiple
goroutines. It supports batched requests and may have arbitrarily many pending
requests in flight simultaneously.

To establish a client we need a Conn:

   import "net"

   conn, err := net.Dial("tcp", "localhost:8080")
   ...
   cli := jrpc2.NewClient(conn)

There are three phases to sending an RPC: First, construct the request, given
the method name to call and the arguments to the method:

   req, err := cli.Req("Math.Add", []int{1, 3, 5, 7})

Second, send the request to the server, and obtain a pending call you can use
to wait for it to complete and receive its results:

   p, err := cli.StartCall(req)

Third, wait for the pending call to complete to receive its results:

   rsp, err := p[0].Wait()

This is a fairly complicated flow, allowing in-flight requests to be batched
and to run concurrently. Fortunately for the more common case of a single,
synchronous request, there is a simpler solution that combines all three steps
in a single method:

   rsp, err := cli.Call("Math.Add", []int{1, 3, 5, 7})

To decode the response from the server, write:

   var result int
   if err := rsp.UnmarshalResult(&result); err != nil {
      log.Fatal("UnmarshalResult:", err)
   }

To shut down a client and discard all its pending work, call cli.Close().


*/
package jrpc2

// Version is the version string for the JSON-RPC protocol understood by this
// implementation, defined at http://www.jsonrpc.org/specification.
const Version = "2.0"
