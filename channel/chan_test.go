package channel

import (
	"testing"
)

func TestChannelTypes(t *testing.T) {
	tests := []struct {
		name    string
		framing Framing
	}{
		// TODO(fromberger): Fix the implementation, then uncomment.
		//{"LSP", LSP},
		{"Line", Line},
		{"Raw", Raw},
		{"Varint", Varint},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := FramedPipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			t.Logf("Testing lhs → rhs :: %s", message1)
			testSendRecv(t, lhs, rhs, message1)
			t.Logf("Testing rhs → lhs :: %s", message2)
			testSendRecv(t, rhs, lhs, message2)
		})
	}
}
