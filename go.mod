module github.com/creachadair/jrpc2

go 1.26.0

require (
	github.com/creachadair/mds v0.31.0
	github.com/google/go-cmp v0.7.0
	golang.org/x/sync v0.24.0
)

require (
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

// A bug in handler.New could panic a wrapped handler on pointer arguments.
retract [v0.21.2, v0.22.0]

// Checksum mismatch due to accidental double tag push. Safe to use, but warns.
retract v0.23.0

tool honnef.co/go/tools/staticcheck
