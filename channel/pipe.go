package channel

import (
	"io"

	"bitbucket.org/creachadair/jrpc2"
)

// Pipe creates a pair of connected in-memory raw channels.
func Pipe() (client, server jrpc2.Channel) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	client = Raw(pipe{cr, cw})
	server = Raw(pipe{sr, sw})
	return
}

type pipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (p pipe) Close() error {
	p.PipeReader.Close()
	return p.PipeWriter.Close()
}

// Combine merges r and wc into a single io.ReadWriteCloser that closes wc when
// it itself is closed. This can be used to combine a pair of streams such as
// os.Stdin and os.Stdout, or the corresponding pipes to a subprocess.
func Combine(r io.Reader, wc io.WriteCloser) io.ReadWriteCloser {
	return rwc{Reader: r, WriteCloser: wc}
}

type rwc struct {
	io.Reader
	io.WriteCloser
}
