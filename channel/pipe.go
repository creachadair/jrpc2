package channel

import "io"

// Pipe creates a pair of connected in-memory channels using the specified
// framing discipline. Pipe will panic if framing == nil.
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
