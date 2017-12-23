package jrpc2

import (
	"reflect"
)

// NewCaller reflectively constructs a function of type:
//
//     func(*Client, X) (Y, error)
//
// that invokes method via the given client, encoding the provided request and
// decoding the response automatically. This supports construction of client
// wrappers that have a more natural function signature. The caller should
// assert the expected type on the return value.
//
// Example:
//    cli := jrpc2.NewClient(conn, nil)
//
//    type Req struct{ X, Y int }
//
//    // Suppose Math.Add is a method taking *Req to int.
//    F := jrpc2.NewCaller("Math.Add", (*Req)(nil), int(0)).(func(*Client, *Req) (int, error))
//
//    n, err := F(cli, &Req{X: 7, Y: 3})
//    if err != nil {
//       log.Fatal(err)
//    }
//    fmt.Println(n)
//
func NewCaller(method string, X, Y interface{}) interface{} {
	cliType := reflect.TypeOf((*Client)(nil))
	reqType := reflect.TypeOf(X)
	rspType := reflect.TypeOf(Y)
	errType := reflect.TypeOf((*error)(nil)).Elem()

	// func(*Client, X) (Y, error)
	funType := reflect.FuncOf(
		[]reflect.Type{cliType, reqType},
		[]reflect.Type{rspType, errType},
		false, // not variadic
	)

	// We need to construct a pointer to the base type for unmarshaling, but
	// remember whether the caller wants the pointer or the base value.
	wantPtr := rspType.Kind() == reflect.Ptr
	if wantPtr {
		rspType = rspType.Elem()
	}

	// Callers passing slice typed arguments will expect nil to behave like an
	// empty slice, but the JSON encoder renders them as "null".  Therefore,
	// for slice typed parameters catch the nil case and convert it silently
	// into an empty slice of the correct type.
	param := func(v reflect.Value) interface{} { return v.Interface() }
	if reqType.Kind() == reflect.Slice {
		param = func(v reflect.Value) interface{} {
			if v.IsNil() {
				return reflect.MakeSlice(reqType, 0, 0).Interface()
			}
			return v.Interface()
		}
	}

	return reflect.MakeFunc(funType, func(args []reflect.Value) []reflect.Value {
		cli := args[0].Interface().(*Client)
		rsp := reflect.New(rspType)
		rerr := reflect.Zero(errType)

		// N.B. the same err is threaded all the way through, so that there is
		// only one point of exit where all the remaining reflection occurs.
		req, err := cli.Req(method, param(args[1]))
		if err == nil {
			var ps []*Pending
			ps, err = cli.Send(req)
			if err == nil {
				var raw *Response
				raw, err = ps[0].Wait()
				if err == nil {
					err = raw.UnmarshalResult(rsp.Interface())
				}
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
