package jcontext

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

var bicent = time.Date(1976, 7, 4, 1, 2, 3, 4, time.UTC)

func TestEncoding(t *testing.T) {
	tests := []struct {
		desc         string
		deadline     time.Time
		params, want string
	}{
		{"zero-void", time.Time{}, "", `{}`},

		{"zero-payload", time.Time{},
			"[1,2,3]", `{"payload":[1,2,3]}`},

		{"bicentennial-void", bicent.In(time.Local),
			"", `{"deadline":"1976-07-04T01:02:03.000000004Z"}`,
		},

		{"bicentennial-payload", bicent,
			`{"apple":"pear"}`,
			`{"deadline":"1976-07-04T01:02:03.000000004Z","payload":{"apple":"pear"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx := context.Background()
			if !test.deadline.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, test.deadline)
				defer cancel()
			}
			raw, err := Encode(ctx, json.RawMessage(test.params))
			if err != nil {
				t.Errorf("Encoding %q failed: %v", test.params, err)
			} else if got := string(raw); got != test.want {
				t.Errorf("Encoding %q: got %#q, want %#q", test.params, got, test.want)
			}
		})
	}
}

func TestDecoding(t *testing.T) {
	tests := []struct {
		desc, input string
		deadline    time.Time
		want        string
	}{
		{"zero-void", `{}`, time.Time{}, ""},

		{"zero-payload", `{"payload":["a","b","c"]}`, time.Time{}, `["a","b","c"]`},

		{"bicentennial-void", `{"deadline":"1976-07-04T01:02:03.000000004Z"}`, bicent, ""},

		{"bicentennial-payload", `{
"deadline":"1976-07-04T01:02:03.000000004Z",
"payload":{"lhs":1,"rhs":2}
}`, bicent, `{"lhs":1,"rhs":2}`},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx := context.Background()
			gotctx, params, err := Decode(ctx, json.RawMessage(test.input))
			if err != nil {
				t.Fatalf("Decode(_, %q): error: %v", test.input, err)
			}
			if !test.deadline.IsZero() {
				dl, ok := gotctx.Deadline()
				if !ok {
					t.Error("Decode: missing expected deadline")
				} else if !dl.Equal(test.deadline) {
					t.Errorf("Decode deadline: got %v, want %v", dl, test.deadline)
				}
			}
			if got := string(params); got != test.want {
				t.Errorf("Decode params: got %#q, want %#q", got, test.want)
			}
		})
	}
}
