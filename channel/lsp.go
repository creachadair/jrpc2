package channel

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// LSP constructs a Channel that transmits and receives messages on r and wc
// using the Language Server Protocol (LSP) framing, defined by the LSP
// specification at http://github.com/Microsoft/language-server-protocol.
//
// Specifically, each message is sent in the format:
//
//    Content-Length: <nbytes>\r\n
//    \r\n
//    <payload>
//
// The length (nbytes) is encoded as decimal digits. For example, the message
// "123\n" is transmitted as:
//
//    Content-Length: 4\r\n
//    \r\n
//    123\n
//
func LSP(r io.Reader, wc io.WriteCloser) Channel {
	return &lsp{wc: wc, rd: bufio.NewReader(r), buf: bytes.NewBuffer(nil)}
}

// An lsp implements Channel. Messages sent on a LSP channel are framed as a
// header/body transaction, similar to HTTP but with less header noise.
type lsp struct {
	wc  io.WriteCloser
	rd  *bufio.Reader
	buf *bytes.Buffer
}

// Send implements part of the Channel interface.
func (c *lsp) Send(msg []byte) error {
	c.buf.Reset()
	fmt.Fprintf(c.buf, "Content-Length: %d\r\n\r\n", len(msg))
	c.buf.Write(msg)
	_, err := c.wc.Write(c.buf.Next(c.buf.Len()))
	return err
}

// Recv implements part of the Channel interface.
func (c *lsp) Recv() ([]byte, error) {
	h := make(map[string]string)
	for {
		raw, err := c.rd.ReadString('\n')
		line := strings.TrimRight(raw, "\r\n")
		if line != "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				h[strings.ToLower(parts[0])] = strings.TrimSpace(parts[1])
			} else {
				return nil, errors.New("invalid header line")
			}
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		} else if line == "" {
			break
		}
	}

	// Parse out the required content-length field.  This implementation
	// ignores unknown header fields.
	clen, ok := h["content-length"]
	if !ok {
		return nil, errors.New("missing required content-length")
	}
	size, err := strconv.Atoi(clen)
	if err != nil {
		return nil, fmt.Errorf("invalid content-length: %v", err)
	} else if size < 0 {
		return nil, errors.New("negative content-length")
	}

	// We need to use ReadFull here because the buffered reader may not have a
	// big enough buffer to deliver the whole message, and will only issue a
	// single read to the underlying source.
	data := make([]byte, size)
	if _, err := io.ReadFull(c.rd, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Close implements part of the Channel interface.
func (c *lsp) Close() error { return c.wc.Close() }
