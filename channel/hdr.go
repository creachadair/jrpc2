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

// Header defines a framing that transmits and receives messages using a header
// prefix similar to HTTP, in which the value of the string is used to describe
// the content encoding.
//
// Specifically, each message is sent in the format:
//
//    Content-Type: <mime-type>\r\n
//    Content-Length: <nbytes>\r\n
//    \r\n
//    <payload>
//
// The length (nbytes) is encoded as decimal digits. For example, given a
// ContentType value "application/json", the message "123\n" is transmitted as:
//
//    Content-Type: application/json\r\n
//    Content-Length: 4\r\n
//    \r\n
//    123\n
//
func Header(mimeType string) Framing {
	return func(r io.Reader, wc io.WriteCloser) Channel {
		return &hdr{
			mtype: mimeType,
			wc:    wc,
			rd:    bufio.NewReader(r),
			buf:   bytes.NewBuffer(nil),
		}
	}
}

// An hdr implements Channel. Messages sent on a hdr channel are framed as a
// header/body transaction, similar to HTTP.
type hdr struct {
	mtype string
	wc    io.WriteCloser
	rd    *bufio.Reader
	buf   *bytes.Buffer
}

// Send implements part of the Channel interface.
func (h *hdr) Send(msg []byte) error {
	h.buf.Reset()
	fmt.Fprintf(h.buf, "Content-Type: %s\r\n", h.mtype)
	fmt.Fprintf(h.buf, "Content-Length: %d\r\n\r\n", len(msg))
	h.buf.Write(msg)
	_, err := h.wc.Write(h.buf.Next(h.buf.Len()))
	return err
}

// Recv implements part of the Channel interface.
func (h *hdr) Recv() ([]byte, error) {
	p := make(map[string]string)
	for {
		raw, err := h.rd.ReadString('\n')
		line := strings.TrimRight(raw, "\r\n")
		if line != "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				p[strings.ToLower(parts[0])] = strings.TrimSpace(parts[1])
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

	// Verify that the content-type matches what we expect.
	if ctype, ok := p["content-type"]; !ok || ctype != h.mtype {
		return nil, errors.New("invalid content-type")
	}

	// Parse out the required content-length field.  This implementation
	// ignores unknown header fields.
	clen, ok := p["content-length"]
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
	if _, err := io.ReadFull(h.rd, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Close implements part of the Channel interface.
func (h *hdr) Close() error { return h.wc.Close() }
