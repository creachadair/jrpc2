package channel

import (
	"encoding/json"
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// NewRaw constructs a jrpc2.Channel that transmits and receives messages on
// rwc with no explicit framing.
func NewRaw(rwc io.ReadWriteCloser) jrpc2.Channel { return Raw{rwc: rwc, dec: json.NewDecoder(rwc)} }

// Raw implements jrpc2.Channel. Messages sent on a Raw channel are not
// explicitly framed, and messages received are framed by JSON syntax.
type Raw struct {
	rwc io.ReadWriteCloser
	dec *json.Decoder
}

// Send implements part of jrpc2.Channel.
func (r Raw) Send(msg []byte) error { _, err := r.rwc.Write(msg); return err }

// Recv implements part of jrpc2.Channel.
func (r Raw) Recv() ([]byte, error) {
	var msg json.RawMessage
	err := r.dec.Decode(&msg)
	return msg, err
}

// Close implements part of jrpc2.Channel.
func (r Raw) Close() error { return r.rwc.Close() }
