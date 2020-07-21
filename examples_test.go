package jrpc2_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/code"
	"github.com/creachadair/jrpc2/handler"
)

var (
	s *jrpc2.Server

	ctx      = context.Background()
	sch, cch = channel.Direct()
	cli      = jrpc2.NewClient(cch, nil)
)

type Msg struct {
	Text string `json:"msg"`
}

func ExampleNewServer() {
	// Construct a new server with methods "Hello" and "Log".
	s = jrpc2.NewServer(handler.Map{
		"Hello": handler.New(func(ctx context.Context) string {
			return "Hello, world!"
		}),
		"Echo": handler.New(func(_ context.Context, args []json.RawMessage) []json.RawMessage {
			return args
		}),
		"Log": handler.New(func(ctx context.Context, msg Msg) (bool, error) {
			fmt.Println("Log:", msg.Text)
			return true, nil
		}),
	}, nil).Start(sch)

	// We can query the server for its current status information, including a
	// list of its methods.
	si := s.ServerInfo()

	fmt.Println(strings.Join(si.Methods, "\n"))
	// Output:
	// Echo
	// Hello
	// Log
}

func ExampleClient_Call() {
	// var cli = jrpc2.NewClient(cch, nil)
	rsp, err := cli.Call(ctx, "Hello", nil)
	if err != nil {
		log.Fatalf("Call: %v", err)
	}
	var msg string
	if err := rsp.UnmarshalResult(&msg); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_CallResult() {
	// var cli = jrpc2.NewClient(cch, nil)
	var msg string
	if err := cli.CallResult(ctx, "Hello", nil, &msg); err != nil {
		log.Fatalf("CallResult: %v", err)
	}
	fmt.Println(msg)
	// Output:
	// Hello, world!
}

func ExampleClient_Batch() {
	// var cli = jrpc2.NewClient(cch, nil)
	rsps, err := cli.Batch(ctx, []jrpc2.Spec{
		{Method: "Hello"},
		{Method: "Log", Params: Msg{"Sing it!"}, Notify: true},
	})
	if err != nil {
		log.Fatalf("Batch: %v", err)
	}

	fmt.Printf("len(rsps) = %d\n", len(rsps))
	for i, rsp := range rsps {
		var msg string
		if err := rsp.UnmarshalResult(&msg); err != nil {
			log.Fatalf("Invalid result: %v", err)
		}
		fmt.Printf("Response #%d: %s\n", i+1, msg)
	}
	// Output:
	// Log: Sing it!
	// len(rsps) = 1
	// Response #1: Hello, world!
}

func ExampleRequest_UnmarshalParams() {
	const msg = `{"jsonrpc":"2.0", "id":101, "method":"M", "params":{"a":1, "b":2, "c":3}}`

	reqs, err := jrpc2.ParseRequests([]byte(msg))
	if err != nil {
		log.Fatalf("ParseRequests: %v", err)
	}

	var t, u struct {
		A int `json:"a"`
		B int `json:"b"`
	}

	// By default, unmarshaling prohibits unknown fields (here, "c").
	err = reqs[0].UnmarshalParams(&t)
	if code.FromError(err) != code.InvalidParams {
		log.Fatalf("Expected invalid parameters, got: %v", err)
	}

	// Solution 1: Decode explicitly.
	var tmp json.RawMessage
	if err := reqs[0].UnmarshalParams(&tmp); err != nil {
		log.Fatalf("UnmarshalParams: %v", err)
	}
	if err := json.Unmarshal(tmp, &t); err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}
	fmt.Printf("t.A=%d, t.B=%d\n", t.A, t.B)

	// Solution 2: Provide a type (here, "lax") that implements json.Unmarshaler.
	if err := reqs[0].UnmarshalParams((*lax)(&u)); err != nil {
		log.Fatalf("UnmarshalParams: %v", err)
	}
	fmt.Printf("u.A=%d, u.B=%d\n", u.A, u.B)

	// Output:
	// t.A=1, t.B=2
	// u.A=1, u.B=2
}

type lax struct{ A, B int }

func (v *lax) UnmarshalJSON(bits []byte) error {
	type T lax
	return json.Unmarshal(bits, (*T)(v))
}

func ExampleResponse_UnmarshalResult() {
	// var cli = jrpc2.NewClient(cch, nil)
	rsp, err := cli.Call(ctx, "Echo", []string{"alpha", "oscar", "kilo"})
	if err != nil {
		log.Fatalf("Call: %v", err)
	}
	var r1, r3 string

	// Note the nil, which tells the decoder to skip that argument.
	if err := rsp.UnmarshalResult(&handler.Args{&r1, nil, &r3}); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(r1, r3)
	// Output:
	// alpha kilo
}
