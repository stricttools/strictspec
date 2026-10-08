package main

import (
	"runtime/debug"
	"strings"
)

// Version is set by goreleaser via ldflags at build time.
// Falls back to the module version embedded by go install.
var Version string

func init() {
	if Version == "" || Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "(devel)" {
			Version = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
}
