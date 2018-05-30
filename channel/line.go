package channel

import (
	"bufio"
	"bytes"
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Line constructs a jrpc2.Channel that transmits and receives messages on r
// and wc with line framing. Each message is terminated by a Unicode LF (10)
// and LF are stripped from outbound messages.
func Line(r io.Reader, wc io.WriteCloser) jrpc2.Channel {
	return line{wc: wc, buf: bufio.NewReader(r)}
}

// line implements jrpc2.Channel. Messages sent on a raw channel are framed by
// terminating newlines.
type line struct {
	wc  io.WriteCloser
	buf *bufio.Reader
}

// Send implements part of jrpc2.Channel.
func (c line) Send(msg []byte) error {
	out := make([]byte, len(msg)+1)
	j := 0
	multi := false
	for _, b := range msg {
		if multi {
			multi = b&0xC0 == 0x80
		} else if b == '\n' {
			continue
		}
		out[j] = b
		j++
	}
	out[j] = '\n'
	_, err := c.wc.Write(out[:j+1])
	return err
}

// Recv implements part of jrpc2.Channel.
func (c line) Recv() ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := c.buf.ReadSlice('\n')
		buf.Write(chunk)
		if err == bufio.ErrBufferFull {
			continue // incomplete line
		} else if err == nil && buf.Len() <= 1 {
			buf.Reset()
			continue // empty line
		}
		line := buf.Bytes()
		if n := len(line) - 1; n >= 0 {
			return line[:n], err
		}
		return nil, err
	}
}

// Close implements part of jrpc2.Channel.
func (c line) Close() error { return c.wc.Close() }
