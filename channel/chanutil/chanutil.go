// Package chanutil exports helper functions for working with channels and
// framing defined by the bitbucket.org/creachadair/jrpc2/channel package.
package chanutil

import "bitbucket.org/creachadair/jrpc2/channel"

// Framing returns a channel.Framing described by the specified name, or nil if
// the name is unknown. The framing types currently understood are:
//
//    json   -- corresponds to channel.JSON
//    line   -- corresponds to channel.Line
//    lsp    -- corresponds to channel.LSP
//    raw    -- corresponds to channel.RawJSON
//    varint -- corresponds to channel.Varint
//
func Framing(name string) channel.Framing {
	switch name {
	case "json":
		return channel.JSON
	case "line":
		return channel.Line
	case "lsp":
		return channel.LSP
	case "raw":
		return channel.RawJSON
	case "varint":
		return channel.Varint
	default:
		return nil
	}
}
