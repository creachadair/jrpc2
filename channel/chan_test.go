package channel

import (
	"io"
	"strconv"
	"testing"
)

var tests = []struct {
	name    string
	framing Framing
}{
	{"Decimal", Decimal},
	{"Header", Header("binary/octet-stream")},
	{"JSON", JSON},
	{"LSP", LSP},
	{"Line", Line},
	{"NUL", Split('\x00')},
	{"NoMIME", Header("")},
	{"RS", Split('\x1e')},
	{"RawJSON", RawJSON},
	{"Varint", Varint},
}

var messages = []string{
	message1,
	message2,
	"null",
	"17",
	`"applejack"`,
	"[]",
	"{}",
	"[null]",
}

func TestChannelTypes(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := Pipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			for i, msg := range messages {
				n := strconv.Itoa(i + 1)
				t.Run("LR-"+n, func(t *testing.T) {
					t.Logf("Testing lhs → rhs :: %s", msg)
					testSendRecv(t, lhs, rhs, message1)
				})
				t.Run("RL-"+n, func(t *testing.T) {
					t.Logf("Testing rhs → lhs :: %s", msg)
					testSendRecv(t, rhs, lhs, message2)
				})
			}
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
