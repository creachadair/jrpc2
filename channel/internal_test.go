// Copyright (C) 2022 Michael J. Fromberger. All Rights Reserved.

package channel

import (
	"io"
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

func TestHeaderTypeMismatch(t *testing.T) {
	cli, srv := newPipe(StrictHeader("text/plain"))
	defer cli.Close()
	defer srv.Close()

	noError := func(err error) bool { return err == nil }
	tests := []struct {
		payload string
		ok      func(error) bool
	}{
		// With a content type provided, no error is reported.
		// Order of headers and extra headers should not affect this.
		{"Content-Type: text/plain\r\nContent-Length: 3\r\n\r\nfoo", noError},
		{"Extra: ok\r\nContent-Length: 4\r\nContent-Type: text/plain\r\n\r\nquux", noError},

		// With a content type provided, report an error if it doesn't match.
		{"Content-Length: 2\r\nContent-Type: application/json\r\n\r\nno", func(err error) bool {
			v, ok := err.(*ContentTypeMismatchError)
			return ok && v.Got == "application/json" && v.Want == "text/plain"
		}},

		// With a content type omitted, a sentinel error is reported.
		{"Content-Length: 5\r\n\r\nabcde", func(err error) bool {
			v, ok := err.(*ContentTypeMismatchError)
			return ok && v.Got == "" && v.Want == "text/plain"
		}},

		// Other errors do not use this sentinel.
		{"Nothing: nohow\r\n\r\nfailure\n", func(err error) bool {
			_, isSentinel := err.(*ContentTypeMismatchError)
			return err != nil && !isSentinel
		}},
	}
	h := cli.(*hdr)
	for _, test := range tests {
		go func() {
			if _, err := h.wc.Write([]byte(test.payload)); err != nil {
				t.Errorf("Send %q failed: %v", test.payload, err)
			}
		}()
		msg, err := srv.Recv()
		if !test.ok(err) {
			t.Errorf("Recv failed: %v\n >> %q", err, msg)
		} else {
			t.Logf("Recv OK: %q", msg)
		}
	}
}
