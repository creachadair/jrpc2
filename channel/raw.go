package channel

import (
	"encoding/json"
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Raw constructs a jrpc2.Channel that transmits and receives messages on rwc
// with no explicit framing.
func Raw(r io.Reader, wc io.WriteCloser) jrpc2.Channel {
	return raw{wc: wc, dec: json.NewDecoder(r)}
}

// A raw implements jrpc2.Channel. Messages sent on a raw channel are not
// explicitly framed, and messages received are framed by JSON syntax.
type raw struct {
	wc  io.WriteCloser
	dec *json.Decoder
}

// Send implements part of jrpc2.Channel.
func (r raw) Send(msg []byte) error { _, err := r.wc.Write(msg); return err }

// Recv implements part of jrpc2.Channel.
func (r raw) Recv() ([]byte, error) {
	var msg json.RawMessage
	err := r.dec.Decode(&msg)
	return msg, err
}

// Close implements part of jrpc2.Channel.
func (r raw) Close() error { return r.wc.Close() }
