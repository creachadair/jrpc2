package channel

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math/bits"
)

// Varint constructs a Channel that transmits and receives messages on r and
// wc, each message prefixed by its length encoded in a varint as defined by
// the encoding/binary package.
func Varint(r io.Reader, wc io.WriteCloser) Channel {
	return &varint{wc: wc, rd: bufio.NewReader(r), buf: bytes.NewBuffer(nil)}
}

// A varint implements Channel. Messages sent on a varint channel are framed
// with a varint length prefix.
type varint struct {
	wc  io.WriteCloser
	rd  *bufio.Reader
	buf *bytes.Buffer
}

func varintLen(n int) int { return (bits.Len64(uint64(n)) + 6) / 7 }

// Send implements part of the Channel interface.
func (c *varint) Send(msg []byte) error {
	var ln [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(ln[:], uint64(len(msg)))
	c.buf.Reset()
	c.buf.Write(ln[:n])
	c.buf.Write(msg)
	_, err := c.wc.Write(c.buf.Next(c.buf.Len()))
	return err
}

// Recv implements part of the Channel interface.
func (c *varint) Recv() ([]byte, error) {
	ln, err := binary.ReadUvarint(c.rd)
	if err != nil {
		return nil, err
	}
	out := make([]byte, int(ln))
	nr, err := io.ReadFull(c.rd, out)
	return out[:nr], err
}

// Close implements part of the Channel interface.
func (c *varint) Close() error { return c.wc.Close() }
