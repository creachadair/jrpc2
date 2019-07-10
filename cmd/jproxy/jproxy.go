// Program jproxy is a reverse proxy JSON-RPC server that bridges and
// multiplexes client requests to a server that communicates over a pipe.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/channel/chanutil"
	"github.com/creachadair/jrpc2/proxy"
	"github.com/creachadair/jrpc2/server"
)

var (
	address       = flag.String("address", "", "Proxy listener address")
	clientFraming = flag.String("cf", "raw", "Client channel framing (for proxy clients)")
	serverFraming = flag.String("sf", "raw", "Server channel framing (between proxy and server)")
	doPipe        = flag.Bool("pipe", false, "Communicate with stdin/stdout")
	doStderr      = flag.Bool("stderr", false, "Send subprocess stderr to proxy stderr")
	doVerbose     = flag.Bool("v", false, "Enable verbose logging")
	graceTime     = flag.Duration("grace", 2*time.Second, "Shutdown grace period on signal")

	logger *log.Logger
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [options] <cmd> <args>...

Run a reverse proxy to a command that implements a JSON-RPC service by running
the command in a subprocess and connecting a JSON-RPC client to its stdin and
stdout. The proxy listens on the specified address and forwards requests to the
subprocess.

If the subprocess exits or the proxy receives an interrupt (SIGINT), the proxy
cleans up any remaining clients and exits.

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
	if err := run(context.Background(), cframe, sframe); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(ctx context.Context, cframe, sframe channel.Framing) error {
	ctx, cancel := context.WithCancel(ctx)
	ch, err := start(ctx, sframe)
	if err != nil {
		cancel()
		return err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		log.Printf("Received signal: %v [waiting %v]", <-sig, *graceTime)
		ch.Close()
		time.Sleep(*graceTime)
		cancel()
		signal.Stop(sig)
	}()

	pc := proxy.New(jrpc2.NewClient(channel.WithTrigger(ch, cancel), &jrpc2.ClientOptions{
		Logger: logger,
	}))
	defer pc.Close()

	lst, err := net.Listen(jrpc2.Network(*address), *address)
	if err != nil {
		return fmt.Errorf("listen %q: %v", *address, err)
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

func start(ctx context.Context, framing channel.Framing) (channel.Channel, error) {
	if *doPipe {
		return framing(os.Stdin, os.Stdout), nil
	}
	// Start the subprocess and connect its stdin and stdout to a client.
	proc := exec.CommandContext(ctx, flag.Arg(0), flag.Args()[1:]...)
	in, err := proc.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("connecting to stdin: %v", err)
	}
	out, err := proc.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("connecting to stdout: %v", err)
	}
	if *doStderr {
		proc.Stderr = os.Stderr
	}
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("starting server failed: %v", err)
	}
	go func() {
		log.Printf("Subprocess exited: %v", proc.Wait())
	}()
	return framing(out, in), nil
}
