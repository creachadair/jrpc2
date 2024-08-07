module github.com/creachadair/jrpc2

require (
	github.com/fortytw2/leaktest v1.3.0
	github.com/google/go-cmp v0.6.0
	golang.org/x/sync v0.7.0
)

require github.com/creachadair/mds v0.15.5

go 1.21

toolchain go1.21.0

// A bug in handler.New could panic a wrapped handler on pointer arguments.
retract [v0.21.2, v0.22.0]

// Checksum mismatch due to accidental double tag push. Safe to use, but warns.
retract v0.23.0
