package channel

import (
	"io"
	"testing"
)

var tests = []struct {
	name    string
	framing Framing
}{
	{"RawJSON", RawJSON},
	{"JSON", JSON},
	{"LSP", LSP},
	{"Header", Header("binary/octet-stream")},
	{"Line", Line},
	{"Varint", Varint},
	{"NUL", Split('\x00')},
	{"RS", Split('\x1e')},
}

func TestChannelTypes(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := Pipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			t.Logf("Testing lhs → rhs :: %s", message1)
			testSendRecv(t, lhs, rhs, message1)
			t.Logf("Testing rhs → lhs :: %s", message2)
			testSendRecv(t, rhs, lhs, message2)
		})
	}
}

func TestEmptyMessage(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := Pipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			t.Log(`Testing lhs → rhs :: "" (empty line)`)
			testSendRecv(t, lhs, rhs, "")
		})
	}
}

func TestWithTrigger(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, w := io.Pipe()
			triggered := false
			ch := WithTrigger(test.framing(r, w), func() {
				triggered = true
			})

			// Send a message to the channel, then close it.
			const message = `["fools", "rush", "in"]`
			go func() {
				t.Log("Sending...")
				if err := ch.Send([]byte(message)); err != nil {
					t.Errorf("Send failed: %v", err)
				}
				t.Logf("Close: err=%v", ch.Close())
			}()

			// Read messages from the channel till it closes, then check that
			// the trigger was correctly invoked.
			for {
				msg, err := ch.Recv()
				if err == io.EOF {
					t.Log("Recv: returned io.EOF")
					break
				} else if err != nil {
					t.Errorf("Recv: unexpected error: %v", err)
					break
				}
				t.Logf("Recv: msg=%q", string(msg))
			}

			if !triggered {
				t.Error("After channel close: trigger not called")
			}
		})
	}
}
