package server

import (
	"context"
	"testing"

	"bitbucket.org/creachadair/jrpc2"
)

func TestProxy(t *testing.T) {
	type input struct {
		Code  int    `json:"code,omitempty"`
		Error string `json:"error,omitempty"`
		Value string `json:"value,omitempty"`
	}
	cli := Local(jrpc2.MapAssigner{
		"Test": jrpc2.NewMethod(func(_ context.Context, req *input) (interface{}, error) {
			if msg := req.Error; msg != "" {
				return nil, jrpc2.Errorf(jrpc2.Code(req.Code), msg)
			}
			return req.Value, nil
		}),
	}, nil, nil)
	proxy := NewProxy(cli)

	tests := []struct {
		input, want string
	}{
		// A complete request returns its response.
		{`{"jsonrpc":"2.0","id":1,"method":"Test","params":{"value":"OK"}}`,
			`{"jsonrpc":"2.0","id":1,"result":"OK"}`},
		// An error request reports an error result.
		{`{"jsonrpc":"2.0","id":2,"method":"Test","params":{"code":123,"error":"bad"}}`,
			`{"jsonrpc":"2.0","id":2,"error":{"code":123,"message":"bad"}}`},
		// The proxy tolerates missing version annotations.
		{`{"id":3,"method":"Test","params":{"value":"hornswoggled"}}`,
			`{"jsonrpc":"2.0","id":3,"result":"hornswoggled"}`},
	}
	for _, test := range tests {
		raw, err := proxy.Send([]byte(test.input))
		if err != nil {
			t.Errorf("Send %#q: unexpected error: %v", test.input, err)
		} else if got := string(raw); got != test.want {
			t.Errorf("Send %#q:\ngot  %#q\nwant %#q", test.input, got, test.want)
		}
	}
}
