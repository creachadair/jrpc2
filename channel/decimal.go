package channel

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
)

// Decimal is a framing that transmits and receives messages on r and wc, with
// each message prefixed by its length encoded as a line of decimal digits.
//
// For example, the message "empanada\n" is encoded as:
//
//    9\n
//    empanada\n
//
func Decimal(r io.Reader, wc io.WriteCloser) Channel {
	return &decimal{wc: wc, rd: bufio.NewReader(r), buf: bytes.NewBuffer(nil)}
}

// A decimal implements Channel. Messages sent on a varint channel are framed
// with a decimal-encoded length on a line by itself.
type decimal struct {
	wc  io.WriteCloser
	rd  *bufio.Reader
	buf *bytes.Buffer
}

// Send implements part of the Channel interface.
func (d *decimal) Send(msg []byte) error {
	d.buf.Reset()
	d.buf.WriteString(strconv.Itoa(len(msg)))
	d.buf.WriteByte('\n')
	d.buf.Write(msg)
	_, err := d.wc.Write(d.buf.Next(d.buf.Len()))
	return err
}

// Recv implements part of the Channel interface.
func (d *decimal) Recv() ([]byte, error) {
	pfx, err := d.rd.ReadString('\n')
	if err == io.EOF && pfx != "" {
		// handle a partial line at EOF
	} else if err != nil {
		return nil, err
	}
	ln, err := strconv.Atoi(pfx[:len(pfx)-1])
	if err != nil {
		return nil, err
	}
	out := make([]byte, int(ln))
	nr, err := io.ReadFull(d.rd, out)
	return out[:nr], err
}

// Close implements part of the Channel interface.
func (d *decimal) Close() error { return d.wc.Close() }
