package jrpc2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"
)

type pipe struct {
	out io.WriteCloser
	in  io.Reader
}

func (p pipe) Write(data []byte) (int, error) { return p.out.Write(data) }
func (p pipe) Read(data []byte) (int, error)  { return p.in.Read(data) }
func (p pipe) Close() error                   { return p.out.Close() }

type thingy struct{}

func (thingy) Sleep(_ context.Context, param struct {
	For string `json:"for"`
}) (string, error) {
	dur, err := time.ParseDuration(param.For)
	if err != nil {
		return "", err
	}
	log.Printf("Sleeping for %v", dur)
	time.Sleep(dur)
	return fmt.Sprint(dur), nil
}

func (thingy) Exec(_ context.Context, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, Errorf(E_InvalidParams, "missing command name")
	}
	return exec.Command(args[0], args[1:]...).Output()
}

func (thingy) Mul(_ context.Context, req struct{ X, Y int }) (int, error) {
	if req.X == 0 || req.Y == 0 {
		return 0, errors.New("you blew it")
	}
	return req.X * req.Y, nil
}

func (thingy) Add(_ context.Context, req *Request) (interface{}, error) {
	if req.IsNotification() {
		return nil, errors.New("ignoring notification")
	}
	var vals []int
	if err := req.UnmarshalParams(&vals); err != nil {
		return nil, err
	}
	var sum int
	for _, v := range vals {
		sum += v
	}
	return sum, nil
}

func (thingy) Unrealated() string { return "this is not a method" }

func Test(t *testing.T) {
	ass := MapAssigner(NewMethods(thingy{}))
	s := NewServer(ass, LogTo(os.Stderr), AllowV1(true), Concurrency(16))
	log.Printf("Starting server on stdin/stdout...")
	s.Start(pipe{
		out: os.Stdout,
		in:  os.Stdin,
	})
	log.Printf("Wait err=%v", s.Wait())
}
