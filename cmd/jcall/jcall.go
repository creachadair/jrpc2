// Program jcall issues RPC calls to a JSON-RPC server.
//
// Usage:
//    jcall [options] <address> {<method> <params>}...
//
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/channel/chanutil"
	"bitbucket.org/creachadair/jrpc2/jctx"
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

See also: https://godoc.org/bitbucket.org/creachadair/jrpc2/channel

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
	if *doHTTP {
		cc = newHTTP(ctx, flag.Arg(0))
	} else if nc := chanutil.Framing(*chanFraming); nc == nil {
		log.Fatalf("Unknown channel framing %q", *chanFraming)
	} else {
		ntype, addr := "tcp", flag.Arg(0)
		if !strings.Contains(addr, ":") {
			ntype = "unix"
		}
		conn, err := net.DialTimeout(ntype, addr, *dialTimeout)
		if err != nil {
			log.Fatalf("Dial %q: %v", addr, err)
		}
		defer conn.Close()
		cc = nc(conn, conn)
	}
	tdial := time.Now()

	cli := newClient(cc)
	rsps, err := issueCalls(ctx, cli, flag.Args()[1:])
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	}
	tcall := time.Now()
	if ok := printResults(rsps); !ok {
		os.Exit(1)
	}
	tprint := time.Now()
	if *doTiming {
		fmt.Fprintf(os.Stderr, "%v elapsed: %v dial, %v call, %v print\n",
			tprint.Sub(start), tdial.Sub(start), tcall.Sub(tdial), tprint.Sub(tcall))
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

// roundTripper implements the channel.Channel interface by sending messages to
// an HTTP server as POST requests with content type "application/json".
type roundTripper struct {
	ctx    context.Context
	cancel context.CancelFunc
	url    string
	rsp    chan []byte // requires at least 1 buffer slot
}

func newHTTP(ctx context.Context, addr string) roundTripper {
	ctx, cancel := context.WithCancel(ctx)
	return roundTripper{
		ctx:    ctx,
		cancel: cancel,
		url:    addr,
		rsp:    make(chan []byte, 1),
	}
}

// Send implements channel.Sender. Each request is sent synchronously to the
// HTTP server at the recorded URL, and the response is either empty or is
// enqueued immediately for the receiver. This implies that there may be at
// most cap(r.rsp) concurrent requests in flight simultaneously with this
// channel.
func (r roundTripper) Send(data []byte) error {
	rsp, err := http.Post(r.url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	} else if rsp.StatusCode == http.StatusNoContent {
		return nil
	} else if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("http: %s", rsp.Status)
	}
	defer rsp.Body.Close()
	bits, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return err
	}
	r.rsp <- bits
	return err
}

// Recv implements channel.Receiver. It blocks until the stored request context
// ends or a message becomes available.
func (r roundTripper) Recv() ([]byte, error) {
	select {
	case <-r.ctx.Done():
		return nil, r.ctx.Err()
	case rsp, ok := <-r.rsp:
		if ok {
			return rsp, nil
		}
		return nil, io.EOF
	}
}

// Close implements part of channel.Channel.
func (r roundTripper) Close() error {
	r.cancel()
	close(r.rsp)
	return nil
}
