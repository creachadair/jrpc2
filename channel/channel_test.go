package channel

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// newPipe creates a pair of connected in-memory channels using the specified
// framing discipline. Sends to client will be received by server, and vice
// versa. newPipe will panic if framing == nil.
func newPipe(framing Framing) (client, server Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = framing(cr, cw)
	server = framing(sr, sw)
	return
}

func testSendRecv(t *testing.T, s Sender, r Receiver, msg string) {
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
}

const message1 = `["Full plate and packing steel"]`
const message2 = `{"slogan":"Jump on your sword, evil!"}`

func TestDirect(t *testing.T) {
	client, server := Direct()
	defer client.Close()
	defer server.Close()

	t.Logf("Testing client ⇒ server :: %s", message1)
	testSendRecv(t, client, server, message1)
	t.Logf("Testing server ⇒ client :: %s", message2)
	testSendRecv(t, server, client, message2)
}

func TestDirectClosed(t *testing.T) {
	client, server := Direct()
	defer server.Close()
	client.Close() // immediately

	if err := client.Send([]byte("nonsense")); err == nil {
		t.Error("Send on closed channel did not fail")
	} else {
		t.Logf("Send correctly failed: %v", err)
	}
}

var tests = []struct {
	name    string
	framing Framing
}{
	{"Header", Header("binary/octet-stream")},
	{"LSP", LSP},
	{"Line", Line},
	{"NoMIME", Header("")},
	{"RS", Split('\x1e')},
	{"RawJSON", RawJSON},
	{"Varint", Varint},
}

// N.B. the messages in this list must be valid JSON, since the RawJSON framing
// requires that structure. A Channel is not required to check this generally.
var messages = []string{
	message1,
	message2,
	"null",
	"17",
	`"applejack"`,
	"[]",
	"{}",
	"[null]",

	// Include a long message to ensure size-dependent cases get exercised.
	`[` + strings.Repeat(`"ABCDefghIJKLmnopQRSTuvwxYZ!",`, 8000) + `"END"]`,
}

func clip(msg string) string {
	if len(msg) > 80 {
		return msg[:80] + fmt.Sprintf(" ...[%d bytes]", len(msg))
	}
	return msg
}

func TestChannelTypes(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := newPipe(test.framing)
			defer client.Close()
			defer server.Close()

			for i, msg := range messages {
				n := strconv.Itoa(i + 1)
				t.Run("client-2-server-"+n, func(t *testing.T) {
					t.Logf("Testing client → server :: %s", clip(msg))
					testSendRecv(t, client, server, message1)
				})
				t.Run("server-2-client-"+n, func(t *testing.T) {
					t.Logf("Testing server → client :: %s", clip(msg))
					testSendRecv(t, server, client, message2)
				})
			}
		})
	}
}

func TestEmptyMessage(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := newPipe(test.framing)
			defer client.Close()
			defer server.Close()

			t.Log(`Testing client → server :: "" (empty line)`)
			testSendRecv(t, client, server, "")
		})
		t.Run(test.name, func(t *testing.T) {
			client, server := Direct()
			defer client.Close()
			defer server.Close()

			t.Log(`Testing client → server :: "" (empty line)`)
			testSendRecv(t, client, server, "")
		})
	}
}

type writeCloser struct {
	*bufio.Writer
}

func (wc *writeCloser) Close() error {
	return wc.Flush()
}

func newWriteCloser(w *bufio.Writer) *writeCloser {
	return &writeCloser{w}
}

func TestHeaderFraming(t *testing.T) {
	testCases := []struct {
		name    string
		framing Framing

		toSend    string
		readerBuf string
		writerBuf string

		expectedSent    []byte
		expectedRecv    []byte
		expectedRecvErr string
	}{
		{
			name:         "outgoing message has Content-Type",
			framing:      Header("application/json"),
			toSend:       `{"hello": "world"}`,
			expectedSent: []byte("Content-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"hello\": \"world\"}"),
		},
		{
			name:         "incoming message with Content-Type is accepted",
			framing:      Header("application/json"),
			readerBuf:    "Content-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"hello\": \"world\"}",
			expectedRecv: []byte(`{"hello": "world"}`),
		},
		{
			name:            "incoming message without Content-Type is rejected",
			framing:         Header("application/json"),
			readerBuf:       "Content-Length: 18\r\n\r\n{\"hello\": \"world\"}",
			expectedRecvErr: "invalid content-type",
		},
		{
			name:         "incoming message without Content-Type is tolerated",
			framing:      OptionalHeader("application/json"),
			readerBuf:    "Content-Length: 18\r\n\r\n{\"hello\": \"world\"}",
			expectedRecv: []byte(`{"hello": "world"}`),
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d-%s", i, tc.name), func(t *testing.T) {
			reader := bytes.NewBufferString(tc.readerBuf)
			writer := bytes.NewBufferString(tc.writerBuf)

			wc := newWriteCloser(bufio.NewWriter(writer))
			ch := tc.framing(reader, wc)

			if tc.toSend != "" {
				err := ch.Send([]byte(tc.toSend))
				if err != nil {
					t.Fatal(err)
				}
				wc.Close()

				sent := writer.Bytes()
				if !bytes.Equal(sent, tc.expectedSent) {
					t.Fatalf("sent message doesn't match.\nexpected: %q\nsent: %q\n",
						string(tc.expectedSent), string(sent))
				}
			}

			var bytesReceived []byte
			var recvErr error
			if tc.expectedRecvErr != "" || len(tc.expectedRecv) > 0 {
				bytesReceived, recvErr = ch.Recv()
			}

			if tc.expectedRecvErr != "" {
				if recvErr == nil {
					t.Fatalf("expected error from Recv: %q", tc.expectedRecvErr)
				}
				if tc.expectedRecvErr != recvErr.Error() {
					t.Fatalf("unexpected error: %q\nexpected: %q\n",
						recvErr.Error(), tc.expectedRecvErr)
				}
			} else if recvErr != nil {
				t.Fatalf("unexpected error: %s", recvErr.Error())
			}
			if len(tc.expectedRecv) > 0 {
				if !bytes.Equal(bytesReceived, tc.expectedRecv) {
					t.Fatalf("received bytes don't match.\nexpected: %q\nreceived: %q\n",
						tc.expectedRecv, bytesReceived)
				}
			}
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
			done := make(chan struct{})
			go func() {
				defer close(done)
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

			<-done
			if !triggered {
				t.Error("After channel close: trigger not called")
			}
		})
	}
}
