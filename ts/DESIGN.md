# ts/ — TypeScript Runtime Library and npm Releasable

Published to npm as `strictspec` (unscoped; owner-reserved). NOT the toolchain. There is NO
separate binary package: the former `-bin` wrapper is eliminated (decision 31). The npm
`strictspec` package carries a stub `bin` that lazy-downloads the exact-version Go binary from
the GitHub Release — with SHA-256 checksum verification — into a platform cache on FIRST RUN,
NEVER at postinstall. So a library-only install (importing the runtime without ever invoking
the CLI) performs zero network access. The download shim is built on rlsbl's first-party
"launcher" artifact mechanism (checksum-verifying templates; an npm first-run launcher variant
is added to rlsbl in a later phase).

FULL FORMAT PARITY: this runtime ships lossless, lexeme-retaining parsers for JSON, TOML, and
JSONL — the four-target conformance identity holds for every format, not just JSON. The TOML
parser is built on the `toml-eslint-parser` library (AST ranges + text-splicing technique, as
proven by strictcli's TS implementation): lossless round-trips come from splicing edits back
into the original text using the parser's node ranges. This REPLACES the former claim that a
from-scratch lossless TOML parser was required because "no suitable lossless library exists" —
that claim was falsified in-ecosystem. CONFIRMED by the validation spike in
conformance/spikes/toml-eslint-parser/ (24 tests passing; VERDICT: CONFIRMS). One caveat the
spike surfaced: `node.number` is normalized (underscores stripped), so range-based splicing is
the only byte-lossless path — the write path must serialize from the source range, never from
`.number`/`.value` (already how strictcli operates). The parser is conformance-tested against
the Go and Python substrates for verdict/code/path/message identity; round-trip fidelity
remains within-backend, like every backend.

## Entry points (per the Generated API Contract)

1. RAW TEXT: the runtime's lossless parsers (lexeme classification per the primitives
   appendix: `.`/`e`/`E` => float; duplicate keys = hard error; overflow, non-finite, and
   number-scalar unrepresentable lexemes = hard errors) produce tagged document-model values,
   which the generated validator checks. `JSON.parse` is never used — it destroys the `3` vs
   `3.0` distinction. Consumers at I/O boundaries pass `await response.text()`. Side benefit:
   real source positions. JSONL streams line-by-line (memory bounded by the largest line;
   all-errors-in-one-pass per line; byte-offset positions; LF-only).
2. TAGGED document-model values: from the parsers or from generated typed constructors. Raw
   untagged JS objects are NOT accepted — ambiguity never enters the model. This is the entry
   PixelWeaver's real call sites need (state setters, snapshot sub-objects, literals built in
   code): post-migration they build tagged values via generated constructors and `with_*`
   helpers, no re-serialization, no false rejections.

Result type: (typed value | undefined) x ordered diagnostics; every diagnostic is an error (no
severity field, no warnings); the assert-style wrapper throws on any diagnostic. Version gate
first, with the structured remediation payload.

## Numbers in TS

There is no TS-target auto-application of the 2^53 safe-integer constraint. Instead, a schema
must explicitly declare `safe_integers = true` (schema-wide); when declared, the constraint
applies in ALL backends, preserving verdict identity. Declaring a TS target for a schema that
lacks the declaration is a hard error at `strictspec gen` time, telling the author to add it.
So a schema with a TS target always carries the declaration, and TS never meets an
unrepresentable integer: integer and number fields bind plain `number`, no BigInt anywhere. The
number scalar additionally hard-errors on any lexeme float64 cannot represent exactly — in
every backend, so verdict identity is untouched. Verdict identity comes from explicit schema
declarations and canonical rules — never from auto-application.

## Contents

- Lossless JSON, TOML (via `toml-eslint-parser`), and JSONL parsers producing tagged,
  lexeme-retaining values.
- Tagged value constructors' support types; semantic-order maps bind to `Map` (JS objects
  reorder integer-like keys — plain objects are unusable for ordered maps). Datetime scalars
  per appendix item 11 bind a tagged runtime datetime type with kind guards — never the
  platform `Date` for local kinds (Date cannot represent a naive local datetime faithfully).
- Diagnostic/result types (no severity); path building with index-then-key switching;
  code-point string length; message rendering from the spec-pinned templates and did-you-mean
  per appendix item 7 (cross-target normative — codes, paths, AND rendered message text are
  the conformance surface); the constructor for consumer-prefixed codes (used by
  consumer-native checks downstream of validation).
- Serialization per the canonical-serialization appendix (untouched values keep lexemes;
  constructed floats render with float lexemes) for consumers that persist documents; the
  write path REFUSES non-current format_version serialization (producer-current-only).
- The inline version-gate helper (structured remediation payload).
- The CONSTRAINT ENGINE: the ported cross-document vocabulary evaluator plus the evidence
  resolvers this environment can honestly provide. In Node: filesystem and sibling-document
  resolvers. In the BROWSER: no filesystem, no git, no subprocess — a schema-declared form
  requiring an unavailable resolver is a hard error naming the resolver, never a skip. There
  is no consumer registration surface; the bespoke tail is consumer-native code over typed
  values, run downstream of validation.

## Boundary posture (browser)

Per the version-boundary invariant (spec/): browser runtimes NEVER migrate. A browser client
either receives current-version bytes (the egress side migrated before sending, under the
negotiation envelope) or refuses cleanly with the structured "update the client" diagnostic
when it cannot speak the negotiated version. There is no migration engine in this runtime, no
downloaded binary in the browser, and no receiver-side upgrade path — by design, not by
omission. Node-side consumers that declare stores/channels in the manifest get generated
checkpoint wrappers that delegate to the packaged CLI.

## Notes

- Browser and Node; no Node-specific APIs anywhere in the runtime or generated code (Node-only
  evidence resolvers and checkpoint wrappers live in explicitly Node-scoped entry points).
- Conformance applicability: TS runs ALL fixtures — JSON, TOML, and JSONL, raw-text and
  tagged-value forms, including lexical-number and datetime cases.
- Pairing with generated code: on the generated-code FORMAT (`GENERATED_CODE_FORMAT`, an
  integer describing the shape of emitted code), never on the release string, which generated
  code carries as information only. The runtime declares the inclusive range of formats it
  reads and throws outside it; the remedy is regeneration.
- Generated files land chmod 444 in consumer repos, carry `/* eslint-disable */` plus the
  generated-by header, and are formatted by the generator's own canonical emitter; `strictspec
  gen` maintains prettier-ignore entries for the generated paths — consumers never
  hand-silence linters.
