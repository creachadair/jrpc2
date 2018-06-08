package jrpc2

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"bitbucket.org/creachadair/jrpc2/code"
	"bitbucket.org/creachadair/stringset"
)

// An Assigner assigns a Method to handle the specified method name, or nil if
// no method is available to handle the request.
type Assigner interface {
	// Assign returns the handler for the named method, or nil.
	Assign(method string) Method

	// Names returns a slice of all known method names for the assigner.  The
	// resulting slice is ordered lexicographically and contains no duplicates.
	Names() []string
}

// A Method handles a single request.
type Method interface {
	// Call invokes the method with the specified request. The response value,
	// if non-nil, must be JSON-marshalable. In case of error, the handler can
	// return a value of type *jrpc2.Error to control the response code sent
	// back to the caller; otherwise the server will wrap the resulting value.
	//
	// The context passed to the handler by a *jrpc2.Server includes two extra
	// values that the handler may extract.
	//
	// To obtain a server metrics value, write:
	//
	//    sm := jrpc2.ServerMetrics(ctx)
	//
	// To obtain the inbound request message, write:
	//
	//    req := jrpc2.InboundRequest(ctx)
	//
	// The inbound request is the same value passed to the Call method -- the
	// latter is primarily useful in handlers generated by jrpc2.NewMethod,
	// which do not receive this value directly.
	Call(context.Context, *Request) (interface{}, error)
}

// A methodFunc adapts a function having the correct signature to a Method.
type methodFunc func(context.Context, *Request) (interface{}, error)

func (m methodFunc) Call(ctx context.Context, req *Request) (interface{}, error) {
	return m(ctx, req)
}

// A MapAssigner is a trivial implementation of the Assigner interface that
// looks up literal method names in a map of static Methods.
type MapAssigner map[string]Method

// Assign implements part of the Assigner interface.
func (m MapAssigner) Assign(method string) Method { return m[method] }

// Names implements part of the Assigner interface.
func (m MapAssigner) Names() []string { return stringset.FromKeys(m).Elements() }

// A ServiceMapper combines multiple assigners into one, permitting a server to
// export multiple services under different names.
//
// Example:
//    m := jrpc2.ServiceMapper{
//      "Foo": jrpc2.NewService(fooService),  // methods Foo.A, Foo.B, etc.
//      "Bar": jrpc2.NewService(barService),  // methods Bar.A, Bar.B, etc.
//    }
//
type ServiceMapper map[string]Assigner

// Assign splits the inbound method name as Service.Method, and passes the
// Method portion to the corresponding Service assigner. If method does not
// have the form Service.Method, or if Service is not set in m, the lookup
// fails and returns nil.
func (m ServiceMapper) Assign(method string) Method {
	parts := strings.SplitN(method, ".", 2)
	if len(parts) == 1 {
		return nil
	} else if ass, ok := m[parts[0]]; ok {
		return ass.Assign(parts[1])
	}
	return nil
}

// Names reports the composed names of all the methods in the service, each
// having the form Service.Method.
func (m ServiceMapper) Names() []string {
	var all stringset.Set
	for svc, assigner := range m {
		for _, name := range assigner.Names() {
			all.Add(svc + "." + name)
		}
	}
	return all.Elements()
}

// NewMethod adapts a function to a Method. The concrete value of fn must be a
// function with one of the following type signatures:
//
//    func(context.Context) (Y, error)
//    func(context.Context, X) (Y, error)
//    func(context.Context, ...X) (Y, error)
//    func(context.Context, *jrpc2.Request) (Y, error)
//
// for JSON-marshalable types X and Y. NewMethod will panic if the type of its
// argument does not have one of these forms.  The resulting method will handle
// encoding and decoding of JSON and report appropriate errors.
func NewMethod(fn interface{}) Method {
	m, err := newMethod(fn)
	if err != nil {
		panic(err)
	}
	return m
}

// NewService adapts the methods of a value to a map from method names to
// Method implementations as constructed by NewMethod. It will panic if obj has
// no exported methods with a suitable signature.
func NewService(obj interface{}) MapAssigner {
	out := make(MapAssigner)
	val := reflect.ValueOf(obj)
	typ := val.Type()

	// This considers only exported methods, as desired.
	for i, n := 0, val.NumMethod(); i < n; i++ {
		mi := val.Method(i)
		if v, err := newMethod(mi.Interface()); err == nil {
			out[typ.Method(i).Name] = v
		}
	}
	if len(out) == 0 {
		panic("no matching exported methods")
	}
	return out
}

var (
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem() // type context.Context
	errType = reflect.TypeOf((*error)(nil)).Elem()           // type error
	reqType = reflect.TypeOf((*Request)(nil))                // type *Request
)

func newMethod(fn interface{}) (Method, error) {
	// Special case: If fn has the exact signature of the Call method, don't do
	// any (additional) reflection at all.
	if f, ok := fn.(func(context.Context, *Request) (interface{}, error)); ok {
		return methodFunc(f), nil
	}

	// Check that fn is a function of one of the correct forms.
	typ, err := checkMethodType(fn)
	if err != nil {
		return nil, err
	}

	// Construct a function to unpack the request values from the request
	// message, based on the signature of the user's callback.
	var newinput func(req *Request) ([]reflect.Value, error)

	if typ.NumIn() == 1 {
		// Case 1: The function does not want any request parameters.
		newinput = func(req *Request) ([]reflect.Value, error) { return nil, nil }
	} else if a := typ.In(1); a == reqType {
		// Case 2: The function wants the underlying *Request value.
		newinput = func(req *Request) ([]reflect.Value, error) {
			return []reflect.Value{reflect.ValueOf(req)}, nil
		}
	} else {
		// Case 3: The function wants us to unpack the request parameters.
		argType := typ.In(1)
		if typ.IsVariadic() {
			// Case 3a: If the function is variadic in its argument, unpack the
			// arguments before calling. Note that in this case argType is
			// already of slice type (see reflect.IsVariadic).
			newinput = func(req *Request) ([]reflect.Value, error) {
				in := reflect.New(argType).Interface()
				if err := req.UnmarshalParams(in); err != nil {
					return nil, Errorf(code.InvalidParams, "wrong argument type: %v", err)
				}
				args := reflect.ValueOf(in).Elem()
				vals := make([]reflect.Value, args.Len())
				for i := 0; i < args.Len(); i++ {
					vals[i] = args.Index(i)
				}
				return vals, nil
			}
		} else {
			// Check whether the function wants a pointer to its argument.  We
			// need to create one either way to support unmarshaling, but we
			// need to indirect it back off if the callee didn't want it.

			// Case 3b: The function wants a bare value, not a pointer.
			undo := reflect.Value.Elem

			if argType.Kind() == reflect.Ptr {
				// Case 3c: The function wants a pointer.
				undo = func(v reflect.Value) reflect.Value { return v }
				argType = argType.Elem()
			}

			newinput = func(req *Request) ([]reflect.Value, error) {
				in := reflect.New(argType).Interface()
				if err := req.UnmarshalParams(in); err != nil {
					return nil, Errorf(code.InvalidParams, "wrong argument type: %v", err)
				}
				arg := reflect.ValueOf(in)
				return []reflect.Value{undo(arg)}, nil
			}
		}
	}
	f := reflect.ValueOf(fn)

	return methodFunc(func(ctx context.Context, req *Request) (interface{}, error) {
		rest, ierr := newinput(req)
		if ierr != nil {
			return nil, ierr
		}
		args := append([]reflect.Value{reflect.ValueOf(ctx)}, rest...)
		vals := f.Call(args)
		out, oerr := vals[0].Interface(), vals[1].Interface()
		if oerr != nil {
			return nil, oerr.(error)
		}
		return out, nil
	}), nil
}

func checkMethodType(fn interface{}) (reflect.Type, error) {
	typ := reflect.TypeOf(fn)
	if typ.Kind() != reflect.Func {
		return nil, errors.New("not a function")
	} else if np := typ.NumIn(); np == 0 || np > 2 {
		return nil, errors.New("wrong number of parameters")
	} else if typ.NumOut() != 2 {
		return nil, errors.New("wrong number of results")
	} else if a := typ.In(0); a != ctxType {
		return nil, errors.New("first parameter is not context.Context")
	} else if a := typ.Out(1); a != errType {
		return nil, errors.New("second result is not error")
	}
	return typ, nil
}
