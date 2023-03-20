// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package handler

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/creachadair/jrpc2"
)

// NewPos adapts a function to a jrpc2.Handler. The concrete value of fn must
// be a function accepted by Positional. The resulting handler will handle JSON
// encoding and decoding, call fn, and report appropriate errors.
//
// NewPos is intended for use during program initialization, and will panic if
// the type of fn does not have one of the accepted forms. Programs that need
// to check for possible errors should call handler.Positional directly, and
// use the Wrap method of the resulting FuncInfo to obtain the wrapper.
func NewPos(fn any, names ...string) jrpc2.Handler {
	fi, err := Positional(fn, names...)
	if err != nil {
		panic(err)
	}
	return fi.Wrap()
}

// structFieldNames reports whether atype is a struct or pointer to struct, and
// if so returns a slice of the eligible field names in order of declaration.
// If atype == nil or is not a (pointer to) struct, it returns false, nil.
func structFieldNames(atype reflect.Type) (bool, []string) {
	if atype == nil {
		return false, nil
	}
	if atype.Kind() == reflect.Ptr {
		atype = atype.Elem()
	}
	if atype.Kind() != reflect.Struct {
		return false, nil
	}

	var names []string
	for i := 0; i < atype.NumField(); i++ {
		fi := atype.Field(i)
		if !fi.IsExported() {
			continue
		}
		if tag, ok := fi.Tag.Lookup("json"); ok {
			if tag == "-" {
				continue // explicitly omitted
			}
			name := strings.SplitN(tag, ",", 2)[0]
			if name != "" {
				names = append(names, name)
				continue
			}
			// fall through to other cases
		}
		if fi.Anonymous {
			// This is an untagged anonymous field. Tagged anonymous fields are
			// handled by the cases above.
			continue
		}
		names = append(names, fi.Name)
	}
	return true, names
}

// Positional checks whether fn can serve as a jrpc2.Handler. The concrete
// value of fn must be a function with one of the following type signature
// schemes:
//
//	func(context.Context, X1, X2, ..., Xn) (Y, error)
//	func(context.Context, X1, X2, ..., Xn) Y
//	func(context.Context, X1, X2, ..., Xn) error
//
// for JSON-marshalable types X_i and Y. If fn does not have one of these
// forms, Positional reports an error. The given names must match the number of
// non-context arguments exactly. Variadic functions are not supported.
//
// In contrast to Check, this function allows any number of arguments, but the
// caller must provide names for them. Positional creates an anonymous struct
// type whose fields correspond to the non-context arguments of fn.  The names
// are used as the JSON field keys for the corresponding parameters.
//
// When converted into a jrpc2.Handler, the wrapped function accepts either a
// JSON array with exactly n members, or a JSON object with the field keys
// named. For example, given:
//
//	func add(ctx context.Context, x, y int) int { return x + y }
//
//	fi, err := handler.Positional(add, "first", "second")
//	// ...
//	call := fi.Wrap()
//
// the resulting handler accepts a JSON array with with (exactly) the same
// number of elements as the positional parameters:
//
//	[17, 23]
//
// No arguments can be omitted in this format, but the caller can use a JSON
// "null" in place of any argument.  The handler will also accept a parameter
// object like:
//
//	{"first": 17, "second": 23}
//
// where "first" is mapped to argument x and "second" to argument y.  In this
// form, fields may be omitted, but unknown field keys generate an error. The
// object keys are taken from the arguments to Positional, not the parameter
// names declared on the function.
func Positional(fn any, names ...string) (*FuncInfo, error) {
	if fn == nil {
		return nil, errors.New("nil function")
	}

	fv := reflect.ValueOf(fn)
	if fv.Kind() != reflect.Func {
		return nil, errors.New("not a function")
	}
	ft := fv.Type()
	if np := ft.NumIn(); np == 0 {
		return nil, errors.New("wrong number of parameters")
	} else if ft.In(0) != ctxType {
		return nil, errors.New("first parameter is not context.Context")
	} else if np == 1 {
		// If the context is the only argument, there is nothing to do.
		return Check(fn)
	} else if ft.IsVariadic() {
		return nil, errors.New("variadic functions are not supported")
	}

	// Reaching here, we have at least one non-context argument.
	atype, err := makeArgType(ft, names)
	if err != nil {
		return nil, err
	}
	fi, err := Check(makeCaller(ft, fv, atype))
	if err == nil {
		fi.strictFields = true
		fi.posNames = names
	}
	return fi, err
}

// makeArgType creates a struct type whose fields match the parameters of t,
// with JSON struct tags corresponding to the given names.
//
// Preconditions: t is a function with len(names)+1 arguments.
func makeArgType(t reflect.Type, names []string) (reflect.Type, error) {
	if t.NumIn()-1 != len(names) {
		return nil, fmt.Errorf("got %d names for %d inputs", len(names), t.NumIn()-1)
	}

	// TODO(creachadair): I wanted to implement the strictFielder interface on
	// the generated struct instead of having extra magic in the wrapper.
	// However, it is not now possible to add methods to a type constructed by
	// reflection.
	//
	// Embedding an anonymous field that exposes the method doesn't work for
	// JSON unmarshaling: The base struct will have the method, but its pointer
	// will not, probably related to https://github.com/golang/go/issues/15924.
	// JSON unmarshaling requires a pointer to its argument.
	//
	// For now, I worked around this by adding a hook into the wrapper compiler.

	var fields []reflect.StructField
	for i, name := range names {
		tag := `json:"-"`
		if name != "" && name != "-" {
			tag = fmt.Sprintf(`json:"%s,omitempty"`, name)
		}
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("P_%d", i+1),
			Type: t.In(i + 1),
			Tag:  reflect.StructTag(tag),
		})
	}
	return reflect.StructOf(fields), nil
}

// makeCaller creates a wrapper function that takes a context and an atype as
// arguments, and calls fv with the context and the struct fields unpacked into
// positional arguments.
//
// Preconditions: fv is a function and atype is its argument struct.
func makeCaller(ft reflect.Type, fv reflect.Value, atype reflect.Type) any {
	atypes := []reflect.Type{ctxType, atype}

	otypes := make([]reflect.Type, ft.NumOut())
	for i := 0; i < ft.NumOut(); i++ {
		otypes[i] = ft.Out(i)
	}

	call := fv.Call
	wtype := reflect.FuncOf(atypes, otypes, false)
	wrap := reflect.MakeFunc(wtype, func(args []reflect.Value) []reflect.Value {
		st := args[1]
		cargs := make([]reflect.Value, st.NumField()+1)
		cargs[0] = args[0] // ctx

		// Unpack the struct fields into positional arguments.
		for i := 0; i < st.NumField(); i++ {
			cargs[i+1] = st.Field(i)
		}
		return call(cargs)
	})
	return wrap.Interface()
}
