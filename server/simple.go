package server

import (
	"errors"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
)

// A Simple server manages execution of a server for a single service instance.
type Simple struct {
	server *jrpc2.Server
	svc    Service
	opts   *jrpc2.ServerOptions
}

// NewSimple constructs a new, unstarted *Simple instance for the given
// service.  When run, the server will use the specified options.
func NewSimple(svc Service, opts *jrpc2.ServerOptions) *Simple {
	return &Simple{svc: svc, opts: opts}
}

// Run starts a server on the given channel, and blocks until it returns.  The
// server exit status is reported to the service, and the error value returned.
func (s *Simple) Run(ch channel.Channel) error {
	if s.server != nil {
		return errors.New("server is already running")
	}
	assigner, err := s.svc.Assigner()
	if err != nil {
		return err
	}
	s.server = jrpc2.NewServer(assigner, s.opts).Start(ch)
	return s.wait()
}

// wait for the server to exit and report its status back to the service.
// Reset the wrapper so it can be re-used.
func (s *Simple) wait() error {
	stat := s.server.WaitStatus()
	s.svc.Finish(stat)
	s.server = nil // reset
	return stat.Err
}

// Stop shuts down the server instance, waits for it to complete, and reports
// the result from its Wait method. It is safe to call Stop even if the server
// is not running; it will report nil.
func (s *Simple) Stop() error {
	if s.server == nil {
		return nil
	}
	s.server.Stop()
	return s.wait()
}
