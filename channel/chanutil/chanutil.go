// Package chanutil exports helper functions for working with channels and
// framing defined by the bitbucket.org/creachadair/jrpc2/channel package.
package chanutil

import "bitbucket.org/creachadair/jrpc2/channel"

// Framing returns a channel.Framing described by the specified name, or nil if
// the name is unknown. The framing types currently understood are:
//
//    decimal -- corresponds to channel.Decimal
//    json    -- corresponds to channel.JSON
//    line    -- corresponds to channel.Line
//    lsp     -- corresponds to channel.LSP
//    raw     -- corresponds to channel.RawJSON
//    varint  -- corresponds to channel.Varint
//
func Framing(name string) channel.Framing { return framings[name] }

var framings = map[string]channel.Framing{
	"decimal": channel.Decimal,
	"json":    channel.JSON,
	"line":    channel.Line,
	"lsp":     channel.LSP,
	"raw":     channel.RawJSON,
	"varint":  channel.Varint,
}
