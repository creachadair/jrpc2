module github.com/creachadair/jrpc2

require (
	github.com/fortytw2/leaktest v1.3.0
	github.com/google/go-cmp v0.5.9
	golang.org/x/sync v0.3.0
)

require (
	github.com/creachadair/mds v0.1.0
	honnef.co/go/tools v0.4.5
)

require (
	github.com/BurntSushi/toml v1.2.1 // indirect
	golang.org/x/exp/typeparams v0.0.0-20221208152030-732eee02a75a // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/tools v0.9.4-0.20230601214343-86c93e8732cc // indirect
)

go 1.20

// A bug in handler.New could panic a wrapped handler on pointer arguments.
retract [v0.21.2, v0.22.0]

// Checksum mismatch due to accidental double tag push. Safe to use, but warns.
retract v0.23.0
