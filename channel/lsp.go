package channel

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
)

// LSP constructs a jrpc2.Channel that transmits and receives messages on r and
// wc using the Language Server Protocol (LSP) framing, defined by the LSP
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
func LSP(r io.Reader, wc io.WriteCloser) jrpc2.Channel {
	return &lsp{wc: wc, rd: bufio.NewReader(r)}
}

// An lsp implements jrpc2.Channel. Messages sent on a LSP channel are framed
// as a header/body transaction, similar to HTTP but with less header noise.
type lsp struct {
	wc  io.WriteCloser
	rd  *bufio.Reader
	buf []byte
}

// Send implements part of jrpc2.Channel.
func (c *lsp) Send(msg []byte) error {
	if _, err := fmt.Fprintf(c.wc, "Content-Length: %d\r\n\r\n", len(msg)); err != nil {
		return err
	}
	_, err := c.wc.Write(msg)
	return err
}

// Recv implements part of jrpc2.Channel.
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

// Close implements part of jrpc2.Channel.
func (c *lsp) Close() error { return c.wc.Close() }
