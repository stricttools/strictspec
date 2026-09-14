---
title: strictspec
description: "strictspec is a schema toolchain that turns one TOML schema into Go, Python and TypeScript validators reporting identical verdicts and diagnostics."
nav_order: 0
order: 0
---

# strictspec

strictspec is a schema toolchain that turns one TOML schema into Go, Python and
TypeScript validators reporting identical verdicts, error codes, paths and
messages for JSON, TOML and JSONL files. A project declares once, in TOML, what
its spec files look like; strictspec generates the code that reads, validates
and version-gates them, and a shared conformance suite holds every target to the
same answer.

## What is here

- **[The schema language](DESIGN/)** — the constitution: the language-neutral
  definition of schemas, the constraint vocabulary, the error model, the
  versioning and migration rules, and the meta-schema.
- **[Surface syntax](appendix-surface-syntax/)** — the concrete TOML spelling of
  every construct.
- **[Error codes](appendix-error-codes/)** — the code catalogue and the pinned
  message templates every backend renders from.
- **[Rendering](appendix-rendering/)** — value rendering, the path grammar, and
  did-you-mean.
- **[Semantics](appendix-semantics/)** — per-construct formal semantics and the
  undecidability catalogue.
- **[Custom scalars](appendix-custom-scalars/)** — registering a scalar of your
  own.
- **[Emitter IR](appendix-emitter-ir/)** — the shared intermediate
  representation every backend's validator is driven by.
- **[Certificates](appendix-certificates/)** — the diff certificate and
  `doc-diff` output shapes.
- **[API reference](gen-index/)** — the public runtime surface generated
  validators import.

## The CLI

The toolchain is one CLI: `gen`, `check`, `validate`, `migrate`, `export`,
`init`, `diff` and `doc-diff`. Generation is file-driven from a
`strictspec.toml` manifest; validation takes a schema and one or more documents
and requires an explicit choice of which check phases run. Installation and a
quick start are in the
[README](https://github.com/smm-h/strictspec#readme).
