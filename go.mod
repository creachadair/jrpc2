module github.com/creachadair/jrpc2

require (
	github.com/google/go-cmp v0.5.6
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
)

go 1.16

// These versions introduced a bug in handler.New that would cause a wrapped
// handler to fail on arguments of pointer type.
retract [v0.21.2, v0.22.0]
