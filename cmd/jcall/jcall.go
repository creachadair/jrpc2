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
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/channel/chanutil"
	"github.com/creachadair/jrpc2/jctx"
	"github.com/creachadair/jrpc2/jhttp"
)

var (
	dialTimeout = flag.Duration("dial", 5*time.Second, "Timeout on dialing the server (0 for no timeout)")
	callTimeout = flag.Duration("timeout", 0, "Timeout on each call (0 for no timeout)")
	doHTTP      = flag.Bool("http", false, "Connect via HTTP (address is the endpoint URL)")
	doNotify    = flag.Bool("notify", false, "Send a notification")
	withContext = flag.Bool("c", false, "Send context with request")
	chanFraming = flag.String("f", "raw", "Channel framing")
	doBatch     = flag.Bool("batch", false, "Issue calls as a batch rather than sequentially")
	doTiming    = flag.Bool("T", false, "Print call timing stats")
	withLogging = flag.Bool("v", false, "Enable verbose logging")
	withMeta    = flag.String("meta", "", "Attach this JSON value as request metadata (implies -c)")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [options] <address> {<method> <params>}...

Connect to the specified address and transmit the specified JSON-RPC method
calls (as a batch, if more than one is provided).  The resulting response
values are printed to stdout.

The -f flag sets the framing discipline to use. The client must agree with the
server in order for communication to work. The options are:

  chunked    -- length-prefixed chunks
  decimal    -- length-prefixed, length as a decimal integer
  header:<t> -- header-framed, content-type <t>
  line       -- byte-terminated, records end in LF (Unicode 10)
  lsp        -- header-framed, content-type application/vscode-jsonrpc (like LSP)
  raw        -- unframed, each message is a complete JSON value
  varint     -- length-prefixed, length is a binary varint

See also: https://godoc.org/github.com/creachadair/jrpc2/channel

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

	ctx := context.Background()
	if *withMeta != "" {
		mc, err := jctx.WithMetadata(ctx, json.RawMessage(*withMeta))
		if err != nil {
			log.Fatalf("Invalid request metadata: %v", err)
		}
		ctx = mc
		*withContext = true
	}

	if *callTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *callTimeout)
		defer cancel()
	}

	// Establish a client channel. If we are using HTTP we do not need to dial a
	// connection; the HTTP client will handle that.
	start := time.Now()
	var cc channel.Channel
	if *doHTTP || isHTTP(flag.Arg(0)) {
		cc = jhttp.NewChannel(flag.Arg(0))
	} else if nc := chanutil.Framing(*chanFraming); nc == nil {
		log.Fatalf("Unknown channel framing %q", *chanFraming)
	} else {
		ntype := jrpc2.Network(flag.Arg(0))
		conn, err := net.DialTimeout(ntype, flag.Arg(0), *dialTimeout)
		if err != nil {
			log.Fatalf("Dial %q: %v", flag.Arg(0), err)
		}
		defer conn.Close()
		cc = nc(conn, conn)
	}
	tdial := time.Now()

	cli := newClient(cc)
	rsps, err := issueCalls(ctx, cli, flag.Args()[1:])
	// defer failure on error till after we print timing stats
	tcall := time.Now()
	if err != nil {
		log.Printf("Call failed: %v", err)
	} else if ok := printResults(rsps); !ok {
		log.Fatal("Printing results failed") // should not generally happen
	}
	tprint := time.Now()
	if *doTiming {
		fmt.Fprintf(os.Stderr, "%v elapsed: %v dial, %v call, %v print [succeeded: %v]\n",
			tprint.Sub(start), tdial.Sub(start), tcall.Sub(tdial), tprint.Sub(tcall), err == nil)
	}
	if err != nil {
		os.Exit(1)
	}
}

func newClient(conn channel.Channel) *jrpc2.Client {
	opts := &jrpc2.ClientOptions{
		OnNotify: func(req *jrpc2.Request) {
			var p json.RawMessage
			req.UnmarshalParams(&p)
			fmt.Printf(`{"method":%q,"params":%s}`+"\n", req.Method(), string(p))
		},
	}
	if *withContext {
		opts.EncodeContext = jctx.Encode
	}
	if *withLogging {
		opts.Logger = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
	}
	return jrpc2.NewClient(conn, opts)
}

func printResults(rsps []*jrpc2.Response) bool {
	ok := true
	for i, rsp := range rsps {
		if rerr := rsp.Error(); rerr != nil {
			log.Printf("Error (%d): %v", i+1, rerr)
			ok = false
			continue
		}
		var result json.RawMessage
		if err := rsp.UnmarshalResult(&result); err != nil {
			log.Printf("Decoding (%d): %v", i+1, err)
			ok = false
			continue
		}
		fmt.Println(string(result))
	}
	return ok
}

func issueCalls(ctx context.Context, cli *jrpc2.Client, args []string) ([]*jrpc2.Response, error) {
	specs := newSpecs(args)
	if *doBatch {
		return cli.Batch(ctx, specs)
	}
	return issueSequential(ctx, cli, specs)
}

func issueSequential(ctx context.Context, cli *jrpc2.Client, specs []jrpc2.Spec) ([]*jrpc2.Response, error) {
	var rsps []*jrpc2.Response
	for _, spec := range specs {
		if spec.Notify {
			if err := cli.Notify(ctx, spec.Method, spec.Params); err != nil {
				return nil, err
			}
		} else if rsp, err := cli.Call(ctx, spec.Method, spec.Params); err != nil {
			return nil, err
		} else {
			rsps = append(rsps, rsp)
		}
	}
	return rsps, nil
}

func newSpecs(args []string) []jrpc2.Spec {
	specs := make([]jrpc2.Spec, 0, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		specs = append(specs, jrpc2.Spec{
			Method: args[i],
			Params: param(args[i+1]),
			Notify: *doNotify,
		})
	}
	return specs
}

func param(s string) interface{} {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

func isHTTP(addr string) bool {
	return strings.HasPrefix(addr, "http:") || strings.HasPrefix(addr, "https:")
}
