package handler

import (
	"errors"
	"fmt"
	"reflect"
)

// Positional checks whether fn can serve as a jrpc2.Handler. The concrete
// value of fn must be a function with one of the following type signature
// schemes:
//
//   func(context.Context, X1, x2, ..., Xn) (Y, error)
//   func(context.Context, X1, x2, ..., Xn) Y
//   func(context.Context, X1, x2, ..., Xn) error
//
// For JSON-marshalable types Xi and Y. If fn does not have one of these forms,
// Positional reports an error. The given names must match the number of
// non-context arguments exactly. Variadic functions are not supported.
//
// This function works by creating an anonymous struct type whose fields
// correspond to the non-context arguments of fn.  The names are used to assign
// JSON decoding tags to the fields of this argument struct.
func Positional(fn interface{}, names ...string) (*FuncInfo, error) {
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

	// TODO(creachadair): I would like to make the generated wrapper strict
	// about unknown fields. However, it is not currently possible to add
	// methods to a type constructed by reflection.
	//
	// Embedding an anonymous field that exposes the method doesn't work for
	// JSON unmarshaling: The struct will have the method, but its pointer will
	// not, probably related to https://github.com/golang/go/issues/15924.

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
func makeCaller(ft reflect.Type, fv reflect.Value, atype reflect.Type) interface{} {
	atypes := []reflect.Type{ctxType, atype}

	otypes := make([]reflect.Type, ft.NumOut())
	for i := 0; i < ft.NumOut(); i++ {
		otypes[i] = ft.Out(i)
	}

	wtype := reflect.FuncOf(atypes, otypes, false)
	wrap := reflect.MakeFunc(wtype, func(args []reflect.Value) []reflect.Value {
		cargs := []reflect.Value{args[0]} // ctx

		// Unpack the struct fields into positional arguments.
		st := args[1]
		for i := 0; i < st.NumField(); i++ {
			cargs = append(cargs, st.Field(i))
		}
		return fv.Call(cargs)
	})
	return wrap.Interface()
}
