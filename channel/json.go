package channel

import (
	"encoding/json"
	"io"
)

// JSON constructs a Channel that transmits and receives records on r and wc,
// in which each record is defined by being a complete JSON value. No padding
// or other separation is added.
func JSON(r io.Reader, wc io.WriteCloser) Channel {
	return jsonc{wc: wc, dec: json.NewDecoder(r)}
}

// A jsonc implements channel.Channel. Messages sent on a raw channel are not
// explicitly framed, and messages received are framed by JSON syntax.
type jsonc struct {
	wc  io.WriteCloser
	dec *json.Decoder
}

// Send implements part of the Channel interface.
func (c jsonc) Send(msg []byte) error { _, err := c.wc.Write(msg); return err }

// Recv implements part of the Channel interface.
func (c jsonc) Recv() ([]byte, error) {
	var msg json.RawMessage
	err := c.dec.Decode(&msg)
	return msg, err
}

// Close implements part of the Channel interface.
func (c jsonc) Close() error { return c.wc.Close() }
