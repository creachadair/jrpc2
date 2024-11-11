//go:build go_mod_tidy_deps

// Package godeps depends on tools needed for build and CI
// that are not otherwise direct dependencies of the module.
package godeps

import (
	_ "honnef.co/go/tools/staticcheck"
)
