/*
Package jrpc2 implements a server and a client for the JSON-RPC 2.0 protocol
defined by http://www.jsonrpc.org/specification.

Servers

The *Server type implements a JSON-RPC server. A server communicates with a
client over a Conn, and dispatches client requests to user-defined method
handlers.  These handlers satisfy the jrpc2.Method interface by exporting a
Call method:

   Call(ctx Context.Context, req *jrpc2.Request) (interface{}, error)

The server finds the Method for a request by looking up its name in a
jrpc2.Assigner provided when the server is set up.

Let's work an example. Suppose we have defined the following Add function:

   // Add returns the sum of a slice of integers.
   func Add(ctx context.Context, values []int) (int, error) {
      sum := 0
      for _, v := range values {
         sum += v
      }
      return sum, nil
   }

To convert this into a jrpc2.Method, we can use the NewMethod function, which
uses reflection to lift the function into the interface:

   m := jrpc2.NewMethod(Add)  // m is a jrpc2.Method

Now let's advertise this function under the name "Math.Add".  For static
assignments, we can use a jrpc2.MapAssigner, which finds methods by looking
them up in a Go map:

   import "bitbucket.org/creachadair/jrpc2"

   assigner := jrpc2.MapAssigner{
      "Math.Add": jrpc2.NewMethod(Add),
   }

Equipped with an Assigner we can now construct a Server:

   srv := jrpc2.NewServer(assigner, nil)  // nil for default options

To serve requests, we will next need a connection. A net.Conn will do, so we
can say for example:

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
server over a Conn, and is safe for concurrent use by multiple goroutines. It
supports batched requests and may have arbitrarily many pending requests in
flight simultaneously.

To establish a client we first need a Conn:

   import "net"

   conn, err := net.Dial("tcp", "localhost:8080")
   ...
   cli := jrpc2.NewClient(conn, nil)

There are two parts to sending an RPC: First, we construct a request given the
method name and parameters, and issue it to the server. This returns a pending
call:

   p, err := cli.Call("Math.Add", []int{1, 3, 5, 7})

Second, we wait for the pending call to complete to receive its results:

   rsp, err := p.Wait()

The separation of call and response allows requests to be issued serially and
waited for in parallel.  For convenience, the client has a CallWait method that
combines these for a single synchronous call:

   rsp, err := cli.CallWait("Math.Add", []int{1, 3, 5, 7})

To issue a batch of requests all at once, use the Batch method:

   batch, err := cli.Batch([]jrpc2.Spec{
      {"Math.Add", []int{1, 2, 3}},
      {"Math.Mul", []int{4, 5, 6}},
      {"Math.Max", []int{-1, 5, 3, 0, 1}},
   })
   ...
   rsps := batch.Wait()  // waits for all the pending responses

In this mode of operation, the caller must check each response for errors:

   for i, rsp := range batch.Wait() {
      if err := rsp.Error(); err != nil {
        log.Printf("Request %d [%s] failed: %v", i, rsp.ID(), err)
      }
   }

Alternatively, you may choose to wait for each request independently (though
note that batch requests will usually not be returned until all results are
complete anyway):

   rsp0, err := batch[0].Wait()
   ...

To decode the result from a response, use its UnmarshalResult method:

   var result int
   if err := rsp.UnmarshalResult(&result); err != nil {
      log.Fatalln("UnmarshalResult:", err)
   }

To shut down a client and discard all its pending work, call cli.Close().

Notifications

The JSON-RPC protocol also supports a kind of request called a notification.
Notifications differ from ordinary requests in that they are one-way: The
client sends them to the server, but the server does not reply.

A Client supports sending notifications as follows:

   err := cli.Notify("Alert", struct{Msg string}{"a fire is burning"})

Unlike ordinary requests, there are no pending calls for notifications; the
notification is complete once it has been sent.

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

   func (math) Add(ctx context.Context, vals ...int) (int, error) { ... }
   func (math) Mul(ctx context.Context, vals []int) (int, error) { ... }

   assigner := jrpc2.NewService(math{})

This assigner maps the name "Add" to the Add method, and the name "Mul" to the
Mul method, of the math value.

This may be further combined with the ServiceMapper type to allow different
services to work together:

   type status struct{}

   func (status) Get(context.Context) (string, error) {
      return "all is well", nil
   }

   assigner := jrpc2.ServiceMapper{
      "Math":   jrpc2.NewService(math{}),
      "Status": jrpc2.NewService(status{}),
   }

This assigner dispatches "Math.Add" and "Math.Mul" to the math value's methods,
and "Status.Get" to the status value's method. A ServiceMapper splits the
method name on the first period ("."), and you may nest ServiceMappers more
deeply if you require a more complex hierarchy.

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

It can also generate a function with no request parameter (with X == nil):

   Status := jrpc.NewCaller("Status", nil, string("")).(func(*jrpc2.Client) (string, error))

*/
package jrpc2

// Version is the version string for the JSON-RPC protocol understood by this
// implementation, defined at http://www.jsonrpc.org/specification.
const Version = "2.0"
