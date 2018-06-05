// Program jcall issues RPC calls to a JSON-RPC server.
//
// Usage:
//    jcall [options] <address> {<method> <params>}...
//
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/jcontext"
)

var (
	dialTimeout = flag.Duration("dial", 5*time.Second, "Timeout on dialing the server (0 for no timeout)")
	callTimeout = flag.Duration("timeout", 0, "Timeout on each call (0 for no timeout)")
	doNotify    = flag.Bool("notify", false, "Send a notification")
	withContext = flag.Bool("c", false, "Send context with request")
	chanFraming = flag.String("f", "json", `Channel framing ("json", "line", "lsp", "varint")`)
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: [options] %s <address> {<method> <params>}...

Connect to the specified address and transmit the specified JSON-RPC method
calls (as a batch, if more than one is provided).  The resulting response
values are printed to stdout.

Options:
`, filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	// There must be at least one request, and more are permitted.  Each method
	// must have an argument, though it may be empty.
	if flag.NArg() < 3 || flag.NArg()%2 == 0 {
		log.Fatal("Arguments are <address> {<method> <params>}...")
	}
	nc := newChannel(*chanFraming)
	ctx := context.Background()

	addr := flag.Arg(0)
	specs := make([]jrpc2.Spec, flag.NArg()/2)
	for i, j := 1, 0; i < flag.NArg(); i += 2 {
		specs[j].Method = flag.Arg(i)
		if p := flag.Arg(i + 1); p != "" {
			specs[j].Params = json.RawMessage(p)
		}
		j++
	}

	// Connect to the server and establish a client.
	ntype, addr := parseAddress(addr)
	conn, err := net.DialTimeout(ntype, addr, *dialTimeout)
	if err != nil {
		log.Fatalf("Dial %q: %v", addr, err)
	}
	defer conn.Close()

	var opts *jrpc2.ClientOptions
	if *withContext {
		opts = &jrpc2.ClientOptions{EncodeContext: jcontext.Encode}
	}
	cli := jrpc2.NewClient(nc(conn, conn), opts)

	// Handle notifications...
	if *doNotify {
		for _, spec := range specs {
			if err := cli.Notify(ctx, spec.Method, spec.Params); err != nil {
				log.Fatalf("Notify %q failed: %v", spec.Method, err)
			}
		}
		return
	}

	// Handle a batch of requests.
	if *callTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *callTimeout)
		defer cancel()
	}
	batch, err := cli.Batch(ctx, specs)
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}
	rsps := batch.Wait()
	failed := false
	for i, rsp := range rsps {
		if rerr := rsp.Error(); rerr != nil {
			log.Printf("Error (%d): %v", i+1, rerr)
			failed = true
			continue
		}
		var result json.RawMessage
		if err := rsp.UnmarshalResult(&result); err != nil {
			log.Printf("Decoding (%d): %v", i+1, err)
			failed = true
			continue
		}
		fmt.Println(string(result))
	}
	if failed {
		os.Exit(1)
	}
}

func newChannel(fmt string) func(io.Reader, io.WriteCloser) channel.Channel {
	switch fmt {
	case "json":
		return channel.JSON
	case "lsp":
		return channel.LSP
	case "line":
		return channel.Line
	case "varint":
		return channel.Varint
	}
	log.Fatalf("Unknown channel format %q", fmt)
	panic("unreachable")
}

func parseAddress(s string) (ntype, addr string) {
	// A TCP address has the form [host]:port, so there must be a colon in it.
	// If we don't find that, assume it's a unix-domain socket.
	if strings.Contains(s, ":") {
		return "tcp", s
	}
	return "unix", s
}
