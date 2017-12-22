// Program jsh exposes a trivial command-shell functionality via JSON-RPC.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"

	"bitbucket.org/creachadair/jrpc2"
)

// RunReq is a request to invoke a program.
type RunReq struct {
	Args  []string `json:"args"`  // The command line to execute
	Input []byte   `json:"input"` // If nonempty, becomes the standard input of the subprocess
}

// RunResult is the result of executing a program.
type RunResult struct {
	Success bool   `json:"success"`          // Whether the process succeeded (exit status 0)
	Output  []byte `json:"output,omitempty"` // The output from the process
}

// Run invokes the specified process and returns the result. It is not an RPC
// error if the process returns a nonzero exit status, unless the process fails
// to start at all.
func Run(ctx context.Context, req *RunReq) (*RunResult, error) {
	if len(req.Args) == 0 || req.Args[0] == "" {
		return nil, jrpc2.Errorf(jrpc2.E_InvalidParams, "missing command name")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.Command(req.Args[0], req.Args[1:]...)
	if len(req.Input) != 0 {
		cmd.Stdin = bytes.NewReader(req.Input)
	}
	out, err := cmd.Output()
	ex, ok := err.(*exec.ExitError)
	if err != nil && !ok {
		return nil, err
	}
	return &RunResult{
		Success: err == nil || ex.Success(),
		Output:  out,
	}, nil
}

var port = flag.Int("port", 0, "Service port")

func main() {
	flag.Parse()
	if *port <= 0 {
		log.Fatal("You must specify a positive --port value")
	}

	lst, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalln("Listen:", err)
	}
	log.Printf("Listening for connections at %s...", lst.Addr())

	m := jrpc2.MapAssigner{"Run": jrpc2.NewMethod(Run)}
	for {
		conn, err := lst.Accept()
		if err != nil {
			log.Fatalln("Accept:", err)
		}
		log.Printf("New connection from %s", conn.RemoteAddr())
		go func() {
			defer conn.Close()
			srv, err := jrpc2.NewServer(m, &jrpc2.ServerOptions{
				AllowV1:   true,
				LogWriter: os.Stderr,
			}).Start(conn)
			if err != nil {
				log.Fatalln("Start:", err)
			}
			if err := srv.Wait(); err != nil {
				log.Printf("Wait: %v", err)
			}
		}()
	}
}
