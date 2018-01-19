// Program jcall issues RPC calls to a JSON-RPC server.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
)

var (
	doNotify = flag.Bool("notify", false, "Send a notification")
)

// TODO(fromberger): Allow Unix-domain socket connections.  Allow other channel
// layouts. Add a timeout on dial.

func main() {
	flag.Parse()

	// There must be at least one request, and more are permitted.  Each method
	// must have an argument, though it may be empty.
	if flag.NArg() < 3 || flag.NArg()%2 == 0 {
		log.Fatal("Arguments are <address> {<method> <params>}...")
	}
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
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("Dial %q: %v", addr, err)
	}
	defer conn.Close()
	cli := jrpc2.NewClient(channel.Raw(conn), nil)

	// Handle notifications...
	if *doNotify {
		for _, spec := range specs {
			if err := cli.Notify(spec.Method, spec.Params); err != nil {
				log.Fatalf("Notify %q failed: %v", spec.Method, err)
			}
		}
		return
	}

	// Handle a batch of requests.
	batch, err := cli.Batch(specs)
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
