package channel

// LSP is a framing that transmits and receives messages on r and wc using the
// Language Server Protocol (LSP) framing, defined by the LSP specification at
// https://microsoft.github.io/language-server-protocol
var LSP = Header("application/vscode-jsonrpc; charset=utf-8")
