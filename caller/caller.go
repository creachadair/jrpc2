/*
Package caller provides a function to construct JRPC2 client call wrappers.

The New function reflectively constructs wrapper functions for calls through a
*jrpc2.Client. This makes it easier to provide a "natural" function call
signature for the remote method, that handles the details of creating the
request and decoding the response internally.

The caller.New function takes the name of a method, a request type X and a
return type Y, and returns a function having the signature:

   func(context.Context, *jrpc2.Client, X) (Y, error)

The result can be asserted to this type and used as a normal function:

   // Request type: []int
   // Result type:  int
   Add := caller.New("Math.Add", []int(nil), int(0)).(func(context.Context, *jrpc2.Client, []int) (int, error))
   ...
   sum, err := Add(ctx, cli, []int{1, 3, 5, 7})
   ...

NewCaller can also optionally generate a variadic function:

   Mul := caller.New("Math.Mul", int(0), int(0), caller.Variadic()).(func(context.Context, *jrpc2.Client, ...int) (int, error))
   ...
   prod, err := Mul(ctx, cli, 1, 2, 3, 4, 5)
   ...

It can also generate a function with no request parameter (with X == nil):

   Status := jrpc.NewCaller("Status", nil, string("")).(func(context.Context, *jrpc2.Client) (string, error))
*/
package caller

import (
	"context"
	"reflect"

	"bitbucket.org/creachadair/jrpc2"
)

// RPC_serverInfo calls the built-in rpc.serverInfo method exported by servers
// in this package.
var RPC_serverInfo = New("rpc.serverInfo",
	nil, (*jrpc2.ServerInfo)(nil)).(func(context.Context, *jrpc2.Client) (*jrpc2.ServerInfo, error))

// Common types used by all invocations.
var (
	cliType = reflect.TypeOf((*jrpc2.Client)(nil))
	errType = reflect.TypeOf((*error)(nil)).Elem()
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
)

// New reflectively constructs a function of type:
//
//     func(context.Context, *Client, X) (Y, error)
//
// that invokes the designated method via the client given, encoding the
// provided request and decoding the response automatically. This supports
// construction of client wrappers that have a more natural function
// signature. The caller should assert the expected type on the return value.
//
// As a special case, if X == nil, the returned function will omit the request
// argument and have the signature:
//
//     func(context.Context, *Client) (Y, error)
//
// New will panic if Y == nil.
//
// Example:
//    cli := jrpc2.NewClient(ch, nil)
//
//    type Req struct{ A, B int }
//
//    // Suppose Math.Add is a method taking *Req to int.
//    F := caller.New("Math.Add", (*Req)(nil), int(0)).(func(context.Context, *Client, *Req) (int, error))
//
//    n, err := F(ctx, cli, &Req{A: 7, B: 3})
//    if err != nil {
//       log.Fatal(err)
//    }
//    fmt.Println(n)
//
func New(method string, X, Y interface{}, opts ...Option) interface{} {
	var wantVariadic bool
	for _, opt := range opts {
		switch opt.(type) {
		case variadic:
			wantVariadic = true
		}
	}

	reqType := reflect.TypeOf(X)
	rspType := reflect.TypeOf(Y)
	if wantVariadic {
		reqType = reflect.SliceOf(reqType)
	}
	argTypes := []reflect.Type{ctxType, cliType}
	if reqType != nil {
		argTypes = append(argTypes, reqType)
	}
	funType := reflect.FuncOf(argTypes, []reflect.Type{rspType, errType}, wantVariadic)

	// We need to construct a pointer to the base type for unmarshaling, but
	// remember whether the caller wants the pointer or the base value.
	wantPtr := rspType.Kind() == reflect.Ptr
	if wantPtr {
		rspType = rspType.Elem()
	}

	// The default condition is we have one request argument.
	param := func(v []reflect.Value) interface{} { return v[2].Interface() }
	if reqType == nil {
		// If there is no request type, don't populate a request argument.
		param = func([]reflect.Value) interface{} { return nil }
	} else if reqType.Kind() == reflect.Slice {
		// Callers passing slice typed arguments will expect nil to behave like an
		// empty slice, but the JSON encoder renders them as "null".  Therefore,
		// for slice typed parameters catch the nil case and convert it silently
		// into an empty slice of the correct type.
		param = func(v []reflect.Value) interface{} {
			if v[2].IsNil() {
				return reflect.MakeSlice(reqType, 0, 0).Interface()
			}
			return v[2].Interface()
		}
	}

	return reflect.MakeFunc(funType, func(args []reflect.Value) []reflect.Value {
		ctx := args[0].Interface().(context.Context)
		cli := args[1].Interface().(*jrpc2.Client)
		rsp := reflect.New(rspType)
		rerr := reflect.Zero(errType)

		// N.B. the same err is threaded all the way through, so that there is
		// only one point of exit where all the remaining reflection occurs.
		raw, err := cli.CallWait(ctx, method, param(args))
		if err == nil {
			if raw.Error() == nil {
				err = raw.UnmarshalResult(rsp.Interface())
			} else {
				err = raw.Error()
			}
		}
		if err != nil {
			rerr = reflect.ValueOf(err).Convert(errType)
		}
		if wantPtr {
			return []reflect.Value{rsp, rerr}
		}
		return []reflect.Value{rsp.Elem(), rerr}
	}).Interface()
}

// An Option controls an optional behaviour of the New function.
type Option interface {
	callOption()
}

type variadic struct{}

func (variadic) callOption() {}

// Variadic returns a CallerOption that makes the generated function wrapper
// variadic in its request parameter type, i.e.,
//
//    func(*jrpc2.Client, ...X) (Y, error)
//
// instead of
//
//    func(*jrpc2.Client, X) (Y, error)
//
func Variadic() Option { return variadic{} }
