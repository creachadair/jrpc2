// Copyright (C) 2017 Michael J. Fromberger. All Rights Reserved.

package channel_test

import (
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/creachadair/jrpc2/channel"
)

// newPipe creates a pair of connected in-memory channels using the specified
// framing discipline. Sends to client will be received by server, and vice
// versa. newPipe will panic if framing == nil.
func newPipe(framing channel.Framing) (client, server channel.Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = framing(cr, cw)
	server = framing(sr, sw)
	return
}

func testSendRecv(t *testing.T, s, r channel.Channel, msg string) {
	synctest.Test(t, func(t *testing.T) {
		var wg sync.WaitGroup
		var sendErr, recvErr error
		var data []byte

		wg.Add(2)
		go func() {
			defer wg.Done()
			data, recvErr = r.Recv()
		}()
		go func() {
			defer wg.Done()
			sendErr = s.Send([]byte(msg))
		}()
		wg.Wait()

		if sendErr != nil {
			t.Errorf("Send(%q): unexpected error: %v", msg, sendErr)
		}
		if recvErr != nil {
			t.Errorf("Recv(): unexpected error: %v", recvErr)
		}
		if got := string(data); got != msg {
			t.Errorf("Recv():\ngot  %#q\nwant %#q", got, msg)
		}
	})
}

const message1 = `["Full plate and packing steel"]`
const message2 = `{"slogan":"Jump on your sword, evil!"}`

func TestDirect(t *testing.T) {
	lhs, rhs := channel.Direct()
	defer lhs.Close()
	defer rhs.Close()

	testSendRecv(t, lhs, rhs, message1)
	testSendRecv(t, rhs, lhs, message2)
}

func TestDirectClosed(t *testing.T) {
	lhs, rhs := channel.Direct()
	defer rhs.Close()
	lhs.Close() // immediately

	if err := lhs.Send([]byte("nonsense")); err == nil {
		t.Error("Send on closed channel did not fail")
	} else {
		t.Logf("Send correctly failed: %v", err)
	}
}

var tests = []struct {
	name    string
	framing channel.Framing
}{
	{"Header", channel.Header("")},
	{"Header", channel.Header("binary/octet-stream")},
	{"LSP", channel.LSP},
	{"Line", channel.Line},
	{"NoMIME", channel.Header("")},
	{"RS", channel.Split('\x1e')},
	{"RawJSON", channel.RawJSON},
	{"StrictHeader", channel.StrictHeader("")},
	{"StrictHeader", channel.StrictHeader("text/plain")},
}

// N.B. the first two messages in this list must be valid JSON, since the
// RawJSON framing requires that structure. A Channel is not required to check
// this generally.
var messages = []string{
	message1,
	message2,
	"null",
	"17",
	`"applejack"`,
	"[]",
	"{}",
	"[null]",
	"    ",
	"xy z z y",

	// Include a long message to ensure size-dependent cases get exercised.
	`[` + strings.Repeat(`"ABCDefghIJKLmnopQRSTuvwxYZ!",`, 8000) + `"END"]`,
}

func TestChannelTypes(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := newPipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()
			msgs := messages

			// The RawJSON encoding requires a self-delimited value.  The first
			// two are by design, the rest may not be.
			if test.name == "RawJSON" {
				msgs = messages[:2]
			}

			for i, msg := range msgs {
				n := strconv.Itoa(i + 1)
				t.Run("LR-"+n, func(t *testing.T) {
					testSendRecv(t, lhs, rhs, msg)
				})
				t.Run("RL-"+n, func(t *testing.T) {
					testSendRecv(t, rhs, lhs, msg)
				})
			}
		})
	}
}

func TestEmptyMessage(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := newPipe(test.framing)
			defer lhs.Close()
			defer rhs.Close()

			testSendRecv(t, lhs, rhs, "")
		})
		t.Run(test.name, func(t *testing.T) {
			lhs, rhs := channel.Direct()
			defer lhs.Close()
			defer rhs.Close()

			testSendRecv(t, lhs, rhs, "")
		})
	}
}
