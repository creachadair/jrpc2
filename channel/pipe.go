package channel

import "io"

// Pipe creates a pair of connected in-memory channels using the specified
// framing discipline. Sends to client will be received by server, and vice
// versa. Pipe will panic if framing == nil.
func Pipe(framing Framing) (client, server Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = framing(cr, cw)
	server = framing(sr, sw)
	return
}

// A Framing converts a reader and a writer into a Channel with a particular
// message-framing discipline.
type Framing func(io.Reader, io.WriteCloser) Channel

// WithTrigger returns a Channel that delegates I/O operations to ch, and when
// a Recv operation on ch returns io.EOF it synchronously calls the trigger.
func WithTrigger(ch Channel, trigger func()) Channel {
	return triggered{ch: ch, trigger: trigger}
}

type triggered struct {
	ch      Channel
	trigger func()
}

// Recv implements part of the channel.Channel interface. It delegates to the
// wrapped channel and calls the trigger when the delegate returns io.EOF.
func (c triggered) Recv() ([]byte, error) {
	msg, err := c.ch.Recv()
	if err == io.EOF {
		c.trigger()
	}
	return msg, err
}

func (c triggered) Send(msg []byte) error { return c.ch.Send(msg) }
func (c triggered) Close() error          { return c.ch.Close() }
