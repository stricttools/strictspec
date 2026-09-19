# python/ — Python Runtime Library and PyPI Releasable

NOT the toolchain (that's Go). Runtime support for generated Python code, published to PyPI as
`strictspec`. There is NO separate binary package: the former `-bin` wrapper is eliminated
(decision 31). The PyPI `strictspec` wheel (stays `py3-none-any`) exposes a `strictspec`
console script that lazy-downloads the exact-version Go binary from the GitHub Release — with
SHA-256 checksum verification — into a platform cache on FIRST CLI INVOCATION. Library-only
installs (importing the runtime without ever invoking the CLI) do zero network access. The
download shim is built on rlsbl's first-party "launcher" artifact mechanism (checksum-verifying
templates). Runtime package version = strictspec release version, so the CLI a consumer runs is
always the same release as the runtime it installed.

## Generated-code style

Frozen dataclasses + explicit generated checks + generated `with_*` copy helpers. pydantic and
msgspec are out: two-phase domain checks require partial-subtree binding, and deterministic
generated output requires full control of every emitted line. Zero third-party runtime deps in
generated code; one emitter architecture across all three languages. Generated checks always
hard-error on unknown keys — a language invariant, not a declared policy and never a library
default. Freezing is shallow-plus-generated-immutability (nested edits go through `with_*`
helpers). Generated files land chmod 444 in consumer repos, carry a `# ruff: noqa` header plus
the generated-by header, and are formatted by the generator's own canonical emitter — no
external formatter is ever involved, and consumers never hand-silence linters on generated
paths.

## Entry points (per the Generated API Contract)

1. Raw text/bytes: this runtime's reader parses losslessly (lexeme classification per the
   primitives appendix; duplicate keys surfaced via object_pairs_hook and hard-errored;
   int64/float64 overflow and number-scalar unrepresentable lexemes hard-errored) into tagged
   document-model values, then validates.
2. Tagged document-model values: from the reader or from generated typed constructors (where
   integer/float/number/datetime is explicit in the type). Raw untagged dicts are NOT accepted
   as validation input — ambiguity never enters the model. This serves in-memory
   mutate-then-validate consumers (PixelWeaver's server validates dicts built by MCP mutations
   today; post-migration it builds tagged values via constructors and `with_*` helpers).

Result type: (typed value | None) x ordered diagnostics; every diagnostic is an error (no
severity field, no warnings); the assert-style wrapper raises on any diagnostic. Version gate
first, with the structured remediation payload.

## Contents

- Diagnostics: the specification's error model (code, path, message, expected/got, suggestion, optional
  position — NO severity), terminal + JSON renderers emitting from the spec-pinned message
  templates, did-you-mean per appendix item 7 (cross-target normative — codes, paths, AND
  rendered message text are the conformance surface), the constructor for consumer-prefixed
  codes (used by consumer-native checks downstream of validation).
- Tagged document model: lexeme-retaining values; JSON reader (ordered, duplicate-rejecting),
  TOML round-trip via tomlkit (within-backend fixpoint; never byte-identical to the Go
  substrate), JSONL streamed line-by-line (memory bounded by the largest line, not the file;
  all-errors-in-one-pass applies per line; positions are byte offsets; LF-only; per-line
  positional errors), O_APPEND single-writer appends, temp+rename rewrites, immutability
  helpers. Datetime scalars per appendix item 11 (TOML natives; RFC 3339 strings in JSON;
  binds datetime/date/time with kind guards). Write side per the canonical-serialization
  appendix: untouched values keep their lexemes; constructed floats render with float lexemes;
  the write path REFUSES non-current format_version serialization (producer-current-only).
  Validating a TOML-syntax document against a schema in which a nullable union is reachable =
  canonical hard error.
- The inline version-gate helper (three-message pattern + structured remediation payload).
- Scalar guards per the specification (number scalar with unrepresentable-lexeme rejection; datetime
  kinds; integral-float, bool-not-int, non-finite).
- The CONSTRAINT ENGINE: the ported cross-document vocabulary evaluator plus the Python
  implementations of the evidence resolvers (filesystem, sibling documents, git where
  available). There is no consumer registration surface — vocabulary checks are
  schema-declared and portable; the bespoke tail is consumer-native code over typed values,
  run by the consumer after validation. A resolver this environment cannot satisfy is a hard
  error naming the resolver.
- Boundary-checkpoint support: generated ingest write-doors and egress wrappers invoke the
  migration engine via the packaged CLI (the engine itself never lives in this runtime); the
  wrappers are generated only for manifest-declared stores/channels.

## Invariants

- No toolchain logic: no schema parsing, no generation, no migration engine (checkpoints
  delegate to the packaged CLI).
- No lenient modes; loading and validation inseparable; discovery collects per-file errors and
  fails loudly. No warnings anywhere.
- Pairing with generated code: on the generated-code FORMAT (`GENERATED_CODE_FORMAT`, an
  integer describing the shape of emitted code), never on the release string, which generated
  code carries as information only. The runtime declares the inclusive range of formats it
  reads and hard-errors outside it; remediation is regeneration, never pinning.
