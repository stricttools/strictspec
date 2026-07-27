// Package strictspecroot is the module-root package whose sole job is to embed
// the VERSION file — the single source of truth for the release version — so the
// public runtime (strictspec.Version) and the CLI derive their version from the
// exact file rlsbl's Go release target bumps during `rlsbl release run`.
//
// Why a separate module-root package instead of embedding directly in
// strictspec/? `go:embed` cannot reference a path outside the embedding file's
// own directory subtree (no `../`). rlsbl's Go target fixes the VERSION file at
// the module root (rlsbl/rlsbl/targets/go.py: VERSION_FILE = "VERSION", written
// via write_version at the project dir). The embedding file must therefore live
// beside VERSION, at the module root. The public strictspec package re-exports
// this value, keeping the version-pairing constant impossible to drift from the
// released version (TestVersionMatchesFile is the gate).
package strictspecroot

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is the strictspec release version, trimmed of the trailing newline the
// VERSION file carries. It is a var (computed from the embedded file at init),
// not a const, precisely so it tracks the file rather than a hand-typed literal.
var Version = strings.TrimSpace(versionFile)
