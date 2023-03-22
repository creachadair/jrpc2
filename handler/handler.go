// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

// Package handler provides implementations of the jrpc2.Assigner interface,
// and support for adapting functions to jrpc2.Handler signature.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/creachadair/jrpc2"
)

// Func is a convenience alias for jrpc2.Handler.
type Func = jrpc2.Handler

// A Map is a trivial implementation of the jrpc2.Assigner interface that looks
// up method names in a static map of function values.
type Map map[string]jrpc2.Handler

// Assign implements part of the jrpc2.Assigner interface.
func (m Map) Assign(_ context.Context, method string) jrpc2.Handler { return m[method] }

// Names implements the optional jrpc2.Namer extension interface.
func (m Map) Names() []string {
	var names []string
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// A ServiceMap combines multiple assigners into one, permitting a server to
// export multiple services under different names.
type ServiceMap map[string]jrpc2.Assigner

// Assign splits the inbound method name as Service.Method, and passes the
// Method portion to the corresponding Service assigner. If method does not
// have the form Service.Method, or if Service is not set in m, the lookup
// fails and returns nil.
func (m ServiceMap) Assign(ctx context.Context, method string) jrpc2.Handler {
	parts := strings.SplitN(method, ".", 2)
	if len(parts) == 1 {
		return nil
	} else if ass, ok := m[parts[0]]; ok {
		return ass.Assign(ctx, parts[1])
	}
	return nil
}

// Names reports the composed names of all the methods in the service, each
// having the form Service.Method.
func (m ServiceMap) Names() []string {
	var all []string
	for svc, assigner := range m {
		namer, ok := assigner.(jrpc2.Namer)
		if !ok {
			all = append(all, svc+".*")
			continue
		}
		for _, name := range namer.Names() {
			all = append(all, svc+"."+name)
		}
	}
	sort.Strings(all)
	return all
}

// New adapts a function to a jrpc2.Handler. The concrete value of fn must be
// function accepted by Check. The resulting jrpc2.Handler will handle JSON
// encoding and decoding, call fn, and report appropriate errors.
//
// New is intended for use during program initialization, and will panic if the
// type of fn does not have one of the accepted forms. Programs that need to
// check for possible errors should call handler.Check directly, and use the
// Wrap method of the resulting FuncInfo to obtain the wrapper.
func New(fn any) jrpc2.Handler {
	fi, err := Check(fn)
	if err != nil {
		panic(err)
	}
	return fi.Wrap()
}

var (
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem() // type context.Context
	errType = reflect.TypeOf((*error)(nil)).Elem()           // type error
	reqType = reflect.TypeOf((*jrpc2.Request)(nil))          // type *jrpc2.Request

	strictType = reflect.TypeOf((*interface{ DisallowUnknownFields() })(nil)).Elem()

	errNoParameters = &jrpc2.Error{Code: jrpc2.InvalidParams, Message: "no parameters accepted"}
)

// FuncInfo captures type signature information from a valid handler function.
type FuncInfo struct {
	Type         reflect.Type // the complete function type
	Argument     reflect.Type // the non-context argument type, or nil
	Result       reflect.Type // the non-error result type, or nil
	ReportsError bool         // true if the function reports an error

	strictFields bool     // enforce strict field checking
	posNames     []string // positional field names

	fn any // the original function value
}

// SetStrict sets the flag on fi that determines whether the wrapper it
// generates will enforce strict field checking. If set true, the wrapper will
// report an error when unmarshaling an object into a struct if the object
// contains fields unknown by the struct. Strict field checking has no effect
// for non-struct arguments.
func (fi *FuncInfo) SetStrict(strict bool) *FuncInfo { fi.strictFields = strict; return fi }

// Wrap adapts the function represented by fi to a jrpc2.Handler.  The wrapped
// function can obtain the *jrpc2.Request value from its context argument using
// the jrpc2.InboundRequest helper.
//
// This method panics if fi == nil or if it does not represent a valid function
// type. A FuncInfo returned by a successful call to Check is always valid.
func (fi *FuncInfo) Wrap() jrpc2.Handler {
	if fi == nil || fi.fn == nil {
		panic("handler: invalid FuncInfo value")
	}

	// Although it is not possible to completely eliminate reflection, the
	// intent here is to hoist as much work as possible out of the body of the
	// constructed wrapper, since that will be executed every time the handler
	// is invoked.
	//
	// To do this, we "pre-compile" helper functions to unmarshal JSON into the
	// input arguments (newInput) and to convert the results from reflectors
	// back into values (decodeOut). We pre-check the function signature and
	// types, so that the helpers do only as much reflection as is necessary:
	// for example, we won't allocate a parameter value if the function does not
	// accept a parameter, nor decode a return value if the function returns
	// only an error.

	// Special case: If fn has the exact signature of the Handle method, don't do
	// any (additional) reflection at all.
	if f, ok := fi.fn.(jrpc2.Handler); ok {
		return f
	}

	// If strict field checking or positional decoding are enabled, ensure
	// arguments are wrapped with the appropriate decoder stubs.
	wrapArg := fi.argWrapper()

	// Construct a function to unpack the parameters from the request message,
	// based on the signature of the user's callback.
	var newInput func(ctx reflect.Value, req *jrpc2.Request) ([]reflect.Value, error)

	arg := fi.Argument
	if arg == nil {
		// Case 1: The function does not want any request parameters.
		// Nothing needs to be decoded, but verify no parameters were passed.
		newInput = func(ctx reflect.Value, req *jrpc2.Request) ([]reflect.Value, error) {
			if req.HasParams() {
				return nil, errNoParameters
			}
			return []reflect.Value{ctx}, nil
		}

	} else if arg == reqType {
		// Case 2: The function wants the underlying *jrpc2.Request value.
		newInput = func(ctx reflect.Value, req *jrpc2.Request) ([]reflect.Value, error) {
			return []reflect.Value{ctx, reflect.ValueOf(req)}, nil
		}

	} else if arg.Kind() == reflect.Ptr {
		// Case 3a: The function wants a pointer to its argument value.
		newInput = func(ctx reflect.Value, req *jrpc2.Request) ([]reflect.Value, error) {
			in := reflect.New(arg.Elem())
			if err := req.UnmarshalParams(wrapArg(in)); err != nil {
				return nil, jrpc2Error(jrpc2.InvalidParams, "invalid parameters: %v", err)
			}
			return []reflect.Value{ctx, in}, nil
		}
	} else {
		// Case 3b: The function wants a bare argument value.
		newInput = func(ctx reflect.Value, req *jrpc2.Request) ([]reflect.Value, error) {
			in := reflect.New(arg) // we still need a pointer to unmarshal
			if err := req.UnmarshalParams(wrapArg(in)); err != nil {
				return nil, jrpc2Error(jrpc2.InvalidParams, "invalid parameters: %v", err)
			}
			// Indirect the pointer back off for the callee.
			return []reflect.Value{ctx, in.Elem()}, nil
		}
	}

	// Construct a function to decode the result values.
	var decodeOut func([]reflect.Value) (any, error)

	if fi.Result == nil {
		// The function returns only an error, the result is always nil.
		decodeOut = func(vals []reflect.Value) (any, error) {
			oerr := vals[0].Interface()
			if oerr != nil {
				return nil, oerr.(error)
			}
			return nil, nil
		}
	} else if !fi.ReportsError {
		// The function returns only single non-error: err is always nil.
		decodeOut = func(vals []reflect.Value) (any, error) {
			return vals[0].Interface(), nil
		}
	} else {
		// The function returns both a value and an error.
		decodeOut = func(vals []reflect.Value) (any, error) {
			if oerr := vals[1].Interface(); oerr != nil {
				return nil, oerr.(error)
			}
			return vals[0].Interface(), nil
		}
	}

	call := reflect.ValueOf(fi.fn).Call
	return func(ctx context.Context, req *jrpc2.Request) (any, error) {
		args, ierr := newInput(reflect.ValueOf(ctx), req)
		if ierr != nil {
			return nil, ierr
		}
		return decodeOut(call(args))
	}
}

// Check checks whether fn can serve as a jrpc2.Handler.  The concrete value of
// fn must be a function with one of the following type signature schemes, for
// JSON-marshalable types X and Y:
//
//	func(context.Context) error
//	func(context.Context) Y
//	func(context.Context) (Y, error)
//	func(context.Context, X) error
//	func(context.Context, X) Y
//	func(context.Context, X) (Y, error)
//	func(context.Context, *jrpc2.Request) error
//	func(context.Context, *jrpc2.Request) Y
//	func(context.Context, *jrpc2.Request) (Y, error)
//	func(context.Context, *jrpc2.Request) (any, error)
//
// If fn does not have one of these forms, Check reports an error.
//
// If the type of X is a struct or a pointer to a struct, the generated wrapper
// accepts JSON parameters as either an object or an array.  Array parameters
// are mapped to the fields of X in the order of field declaration, save that
// unexported fields are skipped. If a field has a `json:"-"` tag, it is also
// skipped. Anonymous fields are skipped unless they are tagged.
//
// For other (non-struct) argument types, the accepted format is whatever the
// json.Unmarshal function can decode into the value.  Note, however, that the
// JSON-RPC standard restricts encoded parameter values to arrays and objects.
// Check will accept argument types that cannot accept arrays or objects, but
// the wrapper will report an error when decoding the request.  The recommended
// solution is to define a struct type for your parameters.
//
// For a single arbitrary type, another approach is to use a 1-element array:
//
//	func(ctx context.Context, sp [1]string) error {
//	   s := sp[0] // pull the actual argument out of the array
//	   // ...
//	}
//
// For more complex positional signatures, see also handler.Positional.
func Check(fn any) (*FuncInfo, error) {
	if fn == nil {
		return nil, errors.New("nil function")
	}

	info := &FuncInfo{Type: reflect.TypeOf(fn), fn: fn}
	if info.Type.Kind() != reflect.Func {
		return nil, errors.New("not a function")
	}

	// Check argument values.
	if np := info.Type.NumIn(); np == 0 || np > 2 {
		return nil, errors.New("wrong number of parameters")
	} else if info.Type.In(0) != ctxType {
		return nil, errors.New("first parameter is not context.Context")
	} else if info.Type.IsVariadic() {
		return nil, errors.New("variadic functions are not supported")
	} else if np == 2 {
		info.Argument = info.Type.In(1)
	}

	// Check for struct field names on the argument type.
	if ok, names := structFieldNames(info.Argument); ok {
		info.posNames = names
	}

	// Check return values.
	no := info.Type.NumOut()
	if no < 1 || no > 2 {
		return nil, errors.New("wrong number of results")
	} else if no == 2 && info.Type.Out(1) != errType {
		return nil, errors.New("result is not of type error")
	}
	info.ReportsError = info.Type.Out(no-1) == errType
	if no == 2 || !info.ReportsError {
		info.Result = info.Type.Out(0)
	}
	return info, nil
}

// arrayStub is a wrapper for an arbitrary value that handles translation of
// JSON arrays into a corresponding object format.
type arrayStub struct {
	v        any
	posNames []string
}

// translate translates the raw JSON data into the correct format for
// unmarshaling into s.v.
//
// If s.posNames is set and data encodes an array, the array is rewritten to an
// equivalent object with field names assigned by the positional names.
// Otherwise, data is returned as-is without error.
func (s *arrayStub) translate(data []byte) ([]byte, error) {
	if firstByte(data) != '[' {
		return data, nil // not an array
	}

	// Decode the array wrapper and verify it has the correct length.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	} else if len(arr) != len(s.posNames) {
		return nil, jrpc2.Errorf(jrpc2.InvalidParams, "got %d parameters, want %d",
			len(arr), len(s.posNames))
	}

	// Rewrite the array into an object.
	obj := make(map[string]json.RawMessage, len(s.posNames))
	for i, name := range s.posNames {
		obj[name] = arr[i]
	}
	return json.Marshal(obj)
}

func (s *arrayStub) UnmarshalJSON(data []byte) error {
	actual, err := s.translate(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(actual, s.v)
}

// strictStub is a wrapper for an arbitrary value that enforces strict field
// checking when unmarshaling from JSON.
type strictStub struct{ v any }

func (s *strictStub) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(s.v)
}

func (fi *FuncInfo) argWrapper() func(reflect.Value) any {
	strict := fi.strictFields && fi.Argument != nil && !fi.Argument.Implements(strictType)
	names := fi.posNames // capture so the wrapper does not pin fi
	array := len(names) != 0
	switch {
	case strict && array:
		return func(v reflect.Value) any {
			return &arrayStub{v: &strictStub{v: v.Interface()}, posNames: names}
		}
	case strict:
		return func(v reflect.Value) any {
			return &strictStub{v: v.Interface()}
		}
	case array:
		return func(v reflect.Value) any {
			return &arrayStub{v: v.Interface(), posNames: names}
		}
	default:
		return reflect.Value.Interface
	}
}

func jrpc2Error(code jrpc2.Code, tag string, err error) error {
	var jerr *jrpc2.Error
	if errors.As(err, &jerr) {
		return jerr
	}
	return jrpc2.Errorf(code, tag, err)
}
