# The Go binary's `Version` variable is set by nobody and read by nobody

## Context

A version-injection audit across the fleet's Go binaries on 2026-09-14
compared each binary's declared `main.Version` with the ldflags line in its
release configuration. strictspec's Go binary came out in a category of its
own. Line references are as of that date; verify before acting.

## Problem

`go/cmd/strictspec/version.go:10` declares `var Version string` with a
comment saying goreleaser sets it via ldflags and `init()` falls back to the
module version from build info. Neither half is what the binary actually
reports:

- The binary's version comes from a `go:embed` of a version file in the same
  package, not from `Version`; that is the working mechanism, and it is
  correct.
- Nothing in the package reads `Version` after `init()` writes it.
- Whether a release configuration injects `main.Version` or not, the value
  is never observed, so the variable is dead code either way.

Dead code that looks like the version mechanism is worse than no code: the
next reader assumes `Version` is what `--version` prints and edits the wrong
thing.

## Fix

Delete `version.go`, including its `init()` and the `runtime/debug` import,
and remove any `-X main.Version=...` ldflags line from the release
configuration so the two mechanisms cannot be confused again. The embedded
version file stays the single authority. If a release check requires an
ldflags symbol for Go projects, the project's configuration should declare
that its version is embedded rather than injected, so the check does not
demand a symbol that must not exist.

Red-green: a test that builds the binary and asserts the reported version
equals the embedded file's content, unchanged by any `-X main.Version`
override.

## Affected

`go/cmd/strictspec/version.go` (delete), the `go:embed` site in the same
package (unchanged, referenced by the test), `.goreleaser.yml` or its
equivalent, `.rlsbl/config.json` if the release tool needs a declaration.

## Effort

Small.
