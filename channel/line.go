package channel

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// Line is a framing that transmits and receives messages on r and wc with line
// framing.  Each message is terminated by a Unicode LF (10). This framing has
// the constraint that outbound records may not contain any LF characters.
func Line(r io.Reader, wc io.WriteCloser) Channel {
	return line{wc: wc, buf: bufio.NewReader(r)}
}

// line implements Channel. Messages sent on a raw channel are framed by
// terminating newlines.
type line struct {
	wc  io.WriteCloser
	buf *bufio.Reader
}

// Send implements part of the Channel interface.  It reports an error if msg
// contains a Unicode LF (10).
func (c line) Send(msg []byte) error {
	if bytes.ContainsAny(msg, "\n") {
		return errors.New("message contains LF")
	}
	out := make([]byte, len(msg)+1)
	copy(out, msg)
	out[len(msg)] = '\n'
	_, err := c.wc.Write(out)
	return err
}

// Recv implements part of the Channel interface.
func (c line) Recv() ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := c.buf.ReadSlice('\n')
		buf.Write(chunk)
		if err == bufio.ErrBufferFull {
			continue // incomplete line
		}
		line := buf.Bytes()
		if n := len(line) - 1; n >= 0 {
			return line[:n], err
		}
		return nil, err
	}
}

// Close implements part of the Channel interface.
func (c line) Close() error { return c.wc.Close() }
