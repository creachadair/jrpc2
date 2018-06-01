// Package channel defines implementations of the jrpc2.Channel interface.
package channel

// A Channel represents the ability to transmit and receive data records.  A
// channel does not interpret the contents of a record, but may add and remove
// framing so that records can be embedded in higher-level protocols.  The
// methods of a Channel need not be safe for concurrent use.
type Channel interface {
	// Send transmits a record on the channel.
	Send([]byte) error

	// Recv returns the next available record from the channel.  If no further
	// messages are available, it returns io.EOF.
	Recv() ([]byte, error)

	// Close shuts down the channel, after which no further records may be
	// sent or received.
	Close() error
}
