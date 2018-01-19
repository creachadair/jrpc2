// Program jcall issues RPC calls to a JSON-RPC server.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"

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

	if n := flag.NArg(); n < 2 || n > 3 {
		log.Fatal("Arguments are <address> <method> [<params>]")
	}
	addr, method := flag.Arg(0), flag.Arg(1)
	var params interface{}
	if flag.NArg() == 3 {
		params = json.RawMessage(flag.Arg(2))
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("Dial %q: %v", addr, err)
	}
	defer conn.Close()
	cli := jrpc2.NewClient(channel.Raw(conn), nil)
	if *doNotify {
		if err := cli.Notify(method, params); err != nil {
			log.Fatalf("Notify failed: %v", err)
		}
		return
	}

	rsp, err := cli.CallWait(method, params)
	if err != nil {
		log.Fatalf("Call failed: %v", err)
	} else if rerr := rsp.Error(); rerr != nil {
		log.Fatalf("Error: %v", rerr)
	}
	var result json.RawMessage
	if err := rsp.UnmarshalResult(&result); err != nil {
		log.Fatalf("Decoding result: %v", err)
	}
	fmt.Println(string(result))
}
