// Package channel defines a communications channel that can encode/transmit
// and decode/receive data records with a configurable framing discipline, and
// provides some simple framing implementations.
package channel

// A Sender represents the ability to transmit a message on a channel.
type Sender interface {
	// Send transmits a record on the channel. Each call to Send transmits one
	// complete record.
	Send([]byte) error
}

// A Receiver represents the ability to receive a message from a channel.
type Receiver interface {
	// Recv returns the next available record from the channel.  If no further
	// messages are available, it returns io.EOF.  Each call to Recv fetches a
	// single complete record.
	Recv() ([]byte, error)
}

// A Channel represents the ability to transmit and receive data records.  A
// channel does not interpret the contents of a record, but may add and remove
// framing so that records can be embedded in higher-level protocols.
//
// One sender and one receiver may use a Channel concurrently, but the methods
// of a Channel are not otherwise required to be safe for concurrent use.
type Channel interface {
	Sender
	Receiver

	// Close shuts down the channel, after which no further records may be
	// sent or received.
	Close() error
}
