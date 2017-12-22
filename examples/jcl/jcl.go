// Program jcl is a client program for the shell server defined in jsh.go.
//
// It implements a trivial command-line reader and dispatcher that sends
// commands via JSON-RPC to the server and prints the responses.  Unlike a real
// shell there is no job control or input redirection; command lines are read
// directly from stdin and packaged as written.
//
// If a line ends in "\" the backslash is stripped off and the next line is
// concatenated to the current line.
//
// If the last token on the command line is "<<" the reader accumulates all
// subsequent lines until a "." on a line by itself as input for the command.
// Escape a plain "." by doubling it "..".
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/shell"
)

var serverAddr = flag.String("service", "", "Sevice address")

func main() {
	flag.Parse()
	if *serverAddr == "" {
		log.Fatal("You must provide a non-empty --service address")
	}

	conn, err := net.Dial("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("Dialing %q: %v", *serverAddr, err)
	}
	log.Printf("Connected to %s...", conn.RemoteAddr())
	defer conn.Close()

	cli := jrpc2.NewClient(conn, nil)
	in := bufio.NewScanner(os.Stdin)
	for {
		req, err := readCommand(in)
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("ERROR: %v", err)
		}

		var result RunResult
		rsp, err := cli.Call("Run", req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "# Error: %v\n", err)
		} else if err := rsp.UnmarshalResult(&result); err != nil {
			log.Printf("Invalid result: %v", err)
		} else {
			fmt.Fprintf(os.Stderr, "# Succeeded: %v\n", result.Success)
			os.Stdout.Write(result.Output)
		}
	}
	fmt.Fprintln(os.Stderr, "Bye!")
}

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

func readCommand(in *bufio.Scanner) (*RunReq, error) {
	for {
		// Read a command line, allowing continuations.
		fmt.Fprint(os.Stderr, "> ")
		var cmd []string
		for in.Scan() {
			line := in.Text()
			trim := strings.TrimSuffix(line, "\\")
			cmd = append(cmd, trim)
			if trim == line {
				break
			}
			fmt.Fprint(os.Stderr, "+ ")
		}
		if err := in.Err(); err != nil {
			return nil, err
		} else if len(cmd) == 0 {
			return nil, io.EOF
		}

		// Burst the line into tokens.
		args, ok := shell.Split(strings.Join(cmd, " "))
		if !ok {
			log.Printf("? Invalid command: unbalanced string quotes")
			continue
		} else if len(args) == 0 {
			continue
		}

		// Check for an input marker...
		var input []string
		if n := len(args) - 1; args[n] == "<<" {
			args = args[:n]
			fmt.Fprint(os.Stderr, "* ")
		moreInput:
			for in.Scan() {
				switch in.Text() {
				case ".":
					input = append(input, "")
					break moreInput
				case "..":
					input = append(input, ".")
				default:
					input = append(input, in.Text())
				}
				fmt.Fprint(os.Stderr, "* ")
			}
			if err := in.Err(); err != nil {
				log.Fatalf("Error reading: %v", err)
			}
		}
		return &RunReq{
			Args:  args,
			Input: []byte(strings.Join(input, "\n")),
		}, nil
	}
}
