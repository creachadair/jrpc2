/*
Package jrpc2 implements a server and a client for the JSON-RPC 2.0 protocol
defined by http://www.jsonrpc.org/specification.

Servers

The *Server type implements a JSON-RPC server. A server communicates with a
client over a Channel, and dispatches client requests to user-defined method
handlers.  These handlers satisfy the jrpc2.Method interface by exporting a
Call method:

   Call(ctx Context.Context, req *jrpc2.Request) (interface{}, error)

The server finds the Method for a request by looking up its name in a
jrpc2.Assigner provided when the server is set up.

Let's work an example. Suppose we have defined the following Add function, and
would like to export it via JSON-RPC:

   // Add returns the sum of a slice of integers.
   func Add(ctx context.Context, values []int) (int, error) {
      sum := 0
      for _, v := range values {
         sum += v
      }
      return sum, nil
   }

To do this, we convert Add to a jrpc2.Method. The easiest way to do this is to
call jrpc2.NewMethod, which uses reflection to lift the function into the
jrpc2.Method interface exported by the server:

   m := jrpc2.NewMethod(Add)  // m is a jrpc2.Method that invokes Add

Next, let's advertise this function under the name "Math.Add".  For static
assignments, we can use a jrpc2.MapAssigner, which finds methods by looking
them up in a Go map:

   import "bitbucket.org/creachadair/jrpc2"

   assigner := jrpc2.MapAssigner{
      "Math.Add": jrpc2.NewMethod(Add),
   }

Equipped with an Assigner we can now construct a Server:

   srv := jrpc2.NewServer(assigner, nil)  // nil for default options

To serve requests, we will next need a connection. The channel package exports
functions that can adapt various input and output streams to a jrpc2.Channel,
for example:

   srv.Start(channel.Line(os.Stdin, os.Stdout))

The running server will handle incoming requests until the connection fails or
until it is stopped explicitly by calling srv.Stop(). To wait for the server to
finish, call:

   err := srv.Wait()

This will report the error that led to the server exiting. A working
implementation of this example can found in examples/adder/adder.go.

Clients

The *Client type implements a JSON-RPC client. A client communicates with a
server over a Channel, and is safe for concurrent use by multiple
goroutines. It supports batched requests and may have arbitrarily many pending
requests in flight simultaneously.

To establish a client we first need a Channel:

   import "net"

   conn, err := net.Dial("tcp", "localhost:8080")
   ...
   cli := jrpc2.NewClient(channel.Raw(conn), nil)

There are two parts to sending an RPC: First, we construct a request given the
method name and parameters, and issue it to the server. This returns a pending
call:

   p, err := cli.Call(ctx, "Math.Add", []int{1, 3, 5, 7})

Second, we wait for the pending call to complete to receive its results:

   rsp := p.Wait()

You can check whether a response contains an error using its Error method:

   if rsp.Error() != nil {
      log.Printf("Error from server: %v", rsp.Error())
   }

The separation of call and response allows requests to be issued serially and
waited for in parallel.  For convenience, the client has a CallWait method that
combines these for a single synchronous call:

   rsp, err := cli.CallWait(ctx, "Math.Add", []int{1, 3, 5, 7})

To issue a batch of requests all at once, use the Batch method:

   batch, err := cli.Batch(ctx, []jrpc2.Spec{
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

   rsp0 := batch[0].Wait()
   ...

To decode the result from a successful response use its UnmarshalResult method:

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

   err := cli.Notify(ctx, "Alert", struct{Msg string}{"a fire is burning"})

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

See the "caller" package for a convenient way to generate client call wrappers.
*/
package jrpc2

// Version is the version string for the JSON-RPC protocol understood by this
// implementation, defined at http://www.jsonrpc.org/specification.
const Version = "2.0"
