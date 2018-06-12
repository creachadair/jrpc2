// Program jproxy is a reverse proxy JSON-RPC server that bridges and
// multiplexes client requests to a server that communicates over a pipe.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	"bitbucket.org/creachadair/jrpc2/proxy"
	"bitbucket.org/creachadair/jrpc2/server"
)

var (
	address       = flag.String("address", "", "Proxy listener address")
	clientFraming = flag.String("cf", "raw", "Client channel framing")
	serverFraming = flag.String("sf", "raw", "Server channel framing")
	doPipe        = flag.Bool("pipe", false, "Communicate with stdin/stdout")
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
	if *doPipe != (flag.NArg() == 0) {
		log.Fatal("You must provide a command to execute or set -pipe")
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

	rc, wc, err := start(ctx, cancel)
	if err != nil {
		return err
	}

	pc := proxy.New(jrpc2.NewClient(sframe(rc, wc), &jrpc2.ClientOptions{
		Logger: logger,
	}))
	defer pc.Close()

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

func start(ctx context.Context, cancel context.CancelFunc) (io.ReadCloser, io.WriteCloser, error) {
	if *doPipe {
		return os.Stdin, os.Stdout, nil
	}
	// Start the subprocess and connect its stdin and stdout to a client.
	proc := exec.CommandContext(ctx, flag.Arg(0), flag.Args()[1:]...)
	in, err := proc.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to stdin: %v", err)
	}
	out, err := proc.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to stdout: %v", err)
	} else if err := proc.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting server failed: %v", err)
	}
	go func() {
		log.Printf("Subprocess exited: %v", proc.Wait())
		cancel()
	}()
	return out, in, nil
}
