package jrpc2

import (
	"context"
	"errors"
	"reflect"
)

// An Assigner assigns a Method to handle the specified method name, or nil if
// no method is available to handle the request.
type Assigner interface {
	Assign(method string) Method
}

// A Method handles a single request.
type Method interface {
	// Call invokes the method with the specified request.
	Call(context.Context, *Request) (interface{}, error)
}

// A MethodFunc adapts a function having the correct signature to a Method.
type MethodFunc func(context.Context, *Request) (interface{}, error)

func (m MethodFunc) Call(ctx context.Context, req *Request) (interface{}, error) {
	return m(ctx, req)
}

// A MapAssigner is a trivial implementation of the Assigner interface that
// looks up literal method names in a map of static Methods.
type MapAssigner map[string]Method

func (m MapAssigner) Assign(method string) Method { return m[method] }

// NewMethod adapts a function to a Method. The concrete value of fn must be a
// function with a type signature:
//
//    func(context.Context, X) (Y, error)
//
// or
//
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

// NewMethods adapts the methods of a value to a map from method names to
// Method implementations as constructed by NewMethod. It will panic if obj has
// no exported methods with a suitable signature.
func NewMethods(obj interface{}) map[string]Method {
	out := make(map[string]Method)
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

func newMethod(fn interface{}) (Method, error) {
	typ := reflect.TypeOf(fn)
	if typ.Kind() != reflect.Func {
		return nil, errors.New("not a function")
	} else if typ.NumIn() != 2 {
		return nil, errors.New("wrong number of parameters")
	} else if typ.NumOut() != 2 {
		return nil, errors.New("wrong number of results")
	} else if a, b := typ.In(0), reflect.TypeOf((*context.Context)(nil)).Elem(); a != b {
		return nil, errors.New("first parameter is not context.Context")
	} else if a, b := typ.Out(1), reflect.TypeOf((*error)(nil)).Elem(); a != b {
		return nil, errors.New("second result is not error")
	}

	// Case 1: The function wants the plain request.
	newinput := func(req *Request) (reflect.Value, error) { return reflect.ValueOf(req), nil }

	// Case 2: The function wants us to unpack the request.
	if a, b := typ.In(1), reflect.TypeOf((*Request)(nil)); a != b {
		// Keep track of whether the function wants a pointer to its argument or
		// not.  We need to create one either way to support unmarshaling, but we
		// need to indirect it back off if the callee didn't express it.
		argType := typ.In(1)
		hasPtr := argType.Kind() == reflect.Ptr
		if hasPtr {
			argType = argType.Elem()
		}
		newinput = func(req *Request) (reflect.Value, error) {
			in := reflect.New(argType).Interface()
			if err := req.UnmarshalParams(in); err != nil {
				return reflect.Value{}, Errorf(E_InvalidParams, "wrong argument type: %v", err)
			}
			arg := reflect.ValueOf(in)
			if hasPtr {
				return arg, nil
			} else {
				return arg.Elem(), nil
			}
		}
	}
	f := reflect.ValueOf(fn)

	return MethodFunc(func(ctx context.Context, req *Request) (interface{}, error) {
		arg, ierr := newinput(req)
		if ierr != nil {
			return nil, ierr
		}
		vals := f.Call([]reflect.Value{reflect.ValueOf(ctx), arg})
		out, oerr := vals[0].Interface(), vals[1].Interface()
		if oerr != nil {
			return nil, oerr.(error)
		}
		return out, nil
	}), nil
}
