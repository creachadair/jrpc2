/*
Package jrpc2 implements a server and a client for the JSON-RPC 2.0 protocol
defined by http://www.jsonrpc.org/specification.

Servers

The *Server type implements a JSON-RPC server. A server communicates with a
client over a Conn, and dispatches client requests to user-defined handlers
through an Assigner. For example, suppose we have defined the following Add
function:

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

   p, err := cli.Send(req)

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

Notifications

The JSON-RPC protocol also supports a kind of request called a "notification".
Notifications differ from ordinary requests in that they are one-way: The
client sends them to the server, but the server does not reply.

A Client also supports sending notifications, as follows:

   req, err := cli.Note("Alert", struct{Msg string}{"a fire is burning"})
   ...
   _, err := cli.Send(req)

Unlike ordinary requests, there are no pending calls for notifications.  As
with ordinary requests, however, notifications can be posted in concurrent
batches. Indeed, you can even mix notifications with ordinary requests in the
same batch.  To simplify the common case of posting a single notification,
however, the client provides:

   err := cli.Notify("Alert", struct{Msg string}{"a fire is burning"})

This is equivalent to the above, for the case of a single notification.

On the server side, notifications are identical to ordinary requests, save that
their return value is discarded once the handler returns. If a handler does not
want to do anything for a notification, it can query the request:

   if req.IsNotification() {
      return 0, nil  // ignore notifications
   }

Services with Multiple Methods

The examples above show a server with only one method using NewMethod; you will
often want to expose more than one. The NewService function supports this by
applying NewMethod to all the exported methods of a concrete value to produce a
MapAssigner for those methods:

   type math struct{}

   func (math) Add(ctx context.Context, vals []int) (int, error) { ... }
   func (math) Mul(ctx context.Context, vals []int) (int, error) { ... }

   assigner := jrpc2.NewService(math{})

This assigner maps the name "Add" to the Add method, and the name "Mul" to the
Mul method, of the math value.

This may be further combined with the ServiceMapper type to allow different
services to work together:

   type status struct{}

   func (status) Get(_ context.Context, _ *jrpc2.Request) (string, error) {
      return "all is well", nil
   }

   assigner := jrpc2.ServiceMapper{
      "Math":   jrpc2.NewService(math{}),
      "Status": jrpc2.NewService(status{}),
   }

This assigner dispatches "Math.Add" and "Math.Mul" to the math value's methods,
and "Status.Get" to the status value's method. A ServiceMapper splits the
method name on the first period ("."), and you may nest ServiceMappers more
deeply if you require a deeper hierarchy.

Client Call Wrappers

The NewCaller function reflectively constructs wrapper functions for calls
through a *jrpc2.Client. This makes it easier to provide a "natural" function
call signature for the remote method, that handles the details of creating the
request and decoding the response internally.

NewCaller takes the name of a method, a request type X and a return type Y, and
returns a function having the signature:

   func(*jrpc2.Client, X) (Y, error)

The result can be asserted to this type and used as a normal function:

   // Request type: []int
   // Result type:  int
   Add := jrpc2.NewCaller("Math.Add", []int(nil), int(0)).(func(*jrpc2.Client, []int) (int, error))
   ...
   sum, err := Add(cli, []int{1, 3, 5, 7})
   ...

NewCaller can also optionally generate a variadic function:

   Mul := jrpc2.NewCaller("Math.Mul", int(0), int(0), jrpc2.Variadic()).(func(*jrpc2.Client, ...int) (int, error))
   ...
   prod, err := Mul(cli, 1, 2, 3, 4, 5)
   ...
*/
package jrpc2

// Version is the version string for the JSON-RPC protocol understood by this
// implementation, defined at http://www.jsonrpc.org/specification.
const Version = "2.0"
