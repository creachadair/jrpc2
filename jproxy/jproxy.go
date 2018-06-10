// Program jproxy is a reverse proxy JSON-RPC server that bridges and
// multiplexes client requests to a server that communicates over a pipe.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/channel/chanutil"
	"bitbucket.org/creachadair/jrpc2/server"
)

var (
	address       = flag.String("address", "", "Proxy listener address")
	clientFraming = flag.String("cf", "raw", "Client channel framing")
	serverFraming = flag.String("sf", "raw", "Server channel framing")
	doVerbose     = flag.Bool("v", false, "Enable verbose logging")

	logger *log.Logger
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [options] <cmd> <args>...

Start the specified command in a subprocess and connect a JSON-RPC client to
its stdin and stdout. Listen at the given address, and reverse proxy clients
that connect to it via the client to the subprocess.

If the subprocess exits or the proxy receives an interrupt (SIGINT), the
process cleans up any remaining clients and exits.

Options:
`, filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("You must provide a command to execute")
	} else if *address == "" {
		log.Fatal("You must provide an -address to listen on")
	}
	if *doVerbose {
		logger = log.New(os.Stderr, "[proxy] ", log.LstdFlags|log.Lshortfile)
	}

	cframe := chanutil.Framing(*clientFraming)
	if cframe == nil {
		log.Fatalf("Unknown client channel framing %q", *clientFraming)
	}
	sframe := chanutil.Framing(*serverFraming)
	if sframe == nil {
		log.Fatalf("Unknown server channel framing %q", *serverFraming)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		log.Printf("Received signal: %v", <-sig)
		cancel()
		signal.Stop(sig)
	}()

	if err := run(ctx, cframe, sframe); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(ctx context.Context, cframe, sframe channel.Framing) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start the subprocess and connect its stdin and stdout to a client.
	proc := exec.CommandContext(ctx, flag.Arg(0), flag.Args()[1:]...)
	in, err := proc.StdinPipe()
	if err != nil {
		return fmt.Errorf("connecting to stdin: %v", err)
	}
	out, err := proc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("connecting to stdout: %v", err)
	} else if err := proc.Start(); err != nil {
		return fmt.Errorf("starting server failed: %v", err)
	}
	go func() {
		log.Printf("Subprocess exited: %v", proc.Wait())
		cancel()
	}()

	pc := newProxyClient(sframe(out, in))

	kind, addr := "tcp", *address
	if !strings.Contains(addr, ":") {
		kind = "unix"
	}
	lst, err := net.Listen(kind, addr)
	if err != nil {
		return fmt.Errorf("Listen %s %q: %v", kind, addr, err)
	}
	go func() {
		<-ctx.Done()
		lst.Close()
	}()

	server.Loop(lst, pc, &server.LoopOptions{
		Framing: cframe,
		ServerOptions: &jrpc2.ServerOptions{
			Concurrency:    8,
			DisableBuiltin: true,
			Logger:         logger,
		},
	})
	return nil
}

func newProxyClient(ch channel.Channel) *proxy {
	return &proxy{
		client: jrpc2.NewClient(ch, &jrpc2.ClientOptions{
			Logger: logger,
		}),
	}
}

// proxy is a reverse proxy that redirects requests through a client.
type proxy struct{ client *jrpc2.Client }

// Assign implements part of the jrpc2.Assigner interface.
func (p *proxy) Assign(_ string) jrpc2.Method { return p }

// Names implements part of the jrpc2.Assigner interface.
func (proxy) Names() []string { return []string{"*"} }

// Call implements the jrpc2.Method interface.
func (p *proxy) Call(ctx context.Context, req *jrpc2.Request) (interface{}, error) {
	// If the request has parameters, unpack them so we can pass them to the call.
	var params interface{}
	if req.HasParams() {
		var msg json.RawMessage
		if err := req.UnmarshalParams(&msg); err != nil {
			log.Printf("MJF :: UnmarshalParams err=%v", err)
			return nil, err
		}
		params = msg
	}

	// Invoke the requested method on the proxied server.
	rsp, err := p.client.Call(ctx, req.Method(), params)
	if err != nil {
		return nil, err
	}

	// Extract the response value or error.
	var result json.RawMessage
	if err := rsp.UnmarshalResult(&result); err != nil {
		return nil, err
	}
	return result, nil
}
