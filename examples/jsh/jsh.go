// Program jsh exposes a trivial command-shell functionality via JSON-RPC for
// demonstration purposes.
//
// Usage:
//    go build bitbucket.org/creachadair/jrpc2/examples/jsh
//    ./jsh -port 8080
//
// See also examples/jcl/jcl.go.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/jcontext"
	"bitbucket.org/creachadair/jrpc2/server"
)

// RunReq is a request to invoke a program.
type RunReq struct {
	Args   []string `json:"args"`   // The command line to execute
	Input  []byte   `json:"input"`  // If nonempty, becomes the standard input of the subprocess
	Stderr bool     `json:"stderr"` // Whether to capture stderr from the subprocess
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
	if req.Args[0] == "cd" {
		if len(req.Args) != 2 {
			return nil, jrpc2.Errorf(jrpc2.E_InvalidParams, "wrong arguments for cd")
		}
		return &RunResult{
			Success: os.Chdir(req.Args[1]) == nil,
		}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Args[0], req.Args[1:]...)
	if len(req.Input) != 0 {
		cmd.Stdin = bytes.NewReader(req.Input)
	}
	run := cmd.Output
	if req.Stderr {
		run = cmd.CombinedOutput
	}
	out, err := run()
	ex, ok := err.(*exec.ExitError)
	if err != nil && !ok {
		return nil, err
	}
	return &RunResult{
		Success: err == nil || ex.Success(),
		Output:  out,
	}, nil
}

var (
	port    = flag.Int("port", 0, "Service port")
	logging = flag.Bool("log", false, "Enable verbose logging")

	lw io.Writer
)

func main() {
	flag.Parse()
	if *port <= 0 {
		log.Fatal("You must specify a positive --port value")
	} else if *logging {
		lw = os.Stdout
	}

	lst, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalln("Listen:", err)
	}
	log.Printf("Listening for connections at %s...", lst.Addr())

	server.Loop(lst, jrpc2.MapAssigner{
		"Run": jrpc2.NewMethod(Run),
	}, &server.LoopOptions{
		ServerOptions: &jrpc2.ServerOptions{
			AllowV1:       true,
			LogWriter:     lw,
			DecodeContext: jcontext.Decode,
		},
	})
}
