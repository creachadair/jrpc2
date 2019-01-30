package jhttp_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"

	"bitbucket.org/creachadair/jrpc2"
	"bitbucket.org/creachadair/jrpc2/channel"
	"bitbucket.org/creachadair/jrpc2/handler"
	"bitbucket.org/creachadair/jrpc2/jhttp"
)

func Example() {
	cch, sch := channel.Pipe(channel.Varint)
	srv := jrpc2.NewServer(handler.Map{
		"Test": handler.New(func(ctx context.Context, ss ...string) (string, error) {
			return strings.Join(ss, " "), nil
		}),
	}, nil).Start(sch)
	defer srv.Stop()

	b := jhttp.New(cch, nil)
	defer b.Close()

	hsrv := httptest.NewServer(b)
	defer hsrv.Close()

	rsp, err := http.Post(hsrv.URL, "application/json", strings.NewReader(`{
  "jsonrpc": "2.0",
  "id": 10235,
  "method": "Test",
  "params": ["full", "plate", "and", "packing", "steel"]
}`))
	if err != nil {
		log.Fatalf("POST request failed: %v", err)
	}
	body, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		log.Fatalf("Reading response body: %v", err)
	}

	fmt.Println(string(body))
	// Output:
	// {"jsonrpc":"2.0","id":10235,"result":"full plate and packing steel"}
}
