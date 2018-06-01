package channel

import "io"

// Pipe creates a pair of connected in-memory raw channels.  It is a shorthand
// for channel.FramedPipe(channel.Raw).
func Pipe() (client, server Channel) { return FramedPipe(Raw) }

// A Framing represents a rule that converts a reader and a writer into a
// jrpc2.Channel with a particular message-framing discipline.
type Framing func(io.Reader, io.WriteCloser) Channel

// FramedPipe creates a pair of connected in-memory channels using the
// specified framing discipline.
func FramedPipe(framing Framing) (client, server Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = framing(cr, cw)
	server = framing(sr, sw)
	return
}
