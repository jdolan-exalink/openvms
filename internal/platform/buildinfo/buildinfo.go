// Package buildinfo exposes version metadata injected at build time via -ldflags.
package buildinfo

import "runtime"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func GoVersion() string { return runtime.Version() }
