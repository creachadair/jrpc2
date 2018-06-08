package channel

import "testing"

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
		if test.name == "RawJSON" {
			continue // this framing can't handle empty messages
		}
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := Pipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			t.Log(`Testing lhs → rhs :: "" (empty line)`)
			testSendRecv(t, lhs, rhs, "")
		})
	}
}
