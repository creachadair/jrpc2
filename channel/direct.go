package channel

import (
	"errors"
	"io"
)

type direct struct {
	send chan<- []byte
	recv <-chan []byte
}

func (d direct) Send(msg []byte) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New("send on closed channel")
		}
	}()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	d.send <- cp
	return nil
}

func (d direct) Recv() ([]byte, error) {
	msg, ok := <-d.recv
	if ok {
		return msg, nil
	}
	return nil, io.EOF
}

func (d direct) Close() error { close(d.send); return nil }

// Direct returns a pair of connected channels that pass message buffers
// directly without encoding. Sends to client will be received by server, and
// vice versa.
func Direct() (client, server Channel) {
	c2s := make(chan []byte)
	s2c := make(chan []byte)
	client = direct{send: c2s, recv: s2c}
	server = direct{send: s2c, recv: c2s}
	return
}
