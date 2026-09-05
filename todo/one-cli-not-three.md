# One CLI, not three

## Context

All three implementations ship the same CLI surface: the Python package
declares `[project.scripts] strictspec`, the Go module ships
`go/cmd/strictspec`, and the TS package declares a `bin` entry. Each
implementation carries renderers for all target languages (verified live:
the Go binary regenerates a consumer's Python validators byte-stably at
the matching stamp), so the three CLIs are full mirrors, held in lockstep
by the conformance suite.

## Problem

A CLI is a process boundary: the invoking repo's language does not
constrain the binary on PATH, so per-language CLIs buy only per-ecosystem
install convenience (pip / npx vs `go install @v0`). Against that, the
exact-match pairing discipline (the CLI version must equal the runtime
and the generated stamps precisely, because a newer CLI restamps
regenerated files and breaks every import) turns N installed CLIs on one
machine into an N-way version-skew hazard. This is not hypothetical: a
machine recently carried a shadowed 0.2.1 CLI beside the 0.2.3 one and
had to be reconciled by hand. One CLI makes the class impossible instead
of managed.

## Ruling to implement

- The Go binary is THE strictspec CLI: single static artifact, installed
  `@v0` per the internal-Go-tools convention, rendering every target
  language's validators.
- The Python and TS packages become runtime-only libraries (what
  generated validators import). Their `[project.scripts]` and `bin`
  entries are deleted outright -- pre-stable, no alias, no deprecation
  period; docs and templates that name a pip/npx invocation of the CLI
  are updated to the Go install.

## The open design question to settle alongside

Whether the Python and TS GENERATOR implementations survive at all:

1. Keep them as internal conformance references (the parity suite
   exercises them through the adapters; they just lose their public CLI
   door). Preserves the three-way spec enforcement; keeps the triple
   maintenance cost of full generators.
2. Delete them, leaving one generator plus the committed conformance
   fixtures as the spec's enforcement. Cuts the deep duplication (the
   generator exists three times today); the spec's authority then rests
   on one implementation checked against fixtures rather than on
   independent implementations checked against each other.

The most correct answer depends on how much of the spec's confidence
comes from implementation independence vs from the fixture corpus --
decide deliberately, not by default.

## Affected

python/pyproject.toml (scripts entry), ts/package.json (bin), the
launcher modules behind both entries, conformance adapters if option 2,
docs/README install instructions, the release pipelines' artifact
expectations.

## Effort

Small for the CLI consolidation itself; the generator-implementations
question is medium-large if option 2 is chosen.
