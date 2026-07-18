# ts/ — TypeScript Runtime Library and npm Releasable

Published to npm as `dclrbl` (unscoped; verified available). NOT the toolchain: TS consumers
run the Go binary at build time via the `dclrbl-bin` npm wrapper — one universal package with
a postinstall download of the exact-version binary from the GitHub Release (the ecosystem's
established wrapper mechanics, decision 31 in the root charter).

FULL FORMAT PARITY: this runtime ships lossless, lexeme-retaining parsers for JSON, TOML, and
JSONL — the four-target conformance identity holds for every format, not just JSON. The TOML
parser is written for this runtime (no suitable lossless library exists) and is
conformance-tested against the Go and Python substrates for verdict/code/path/message
identity; round-trip fidelity remains within-backend, like every backend.

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
   a private project's real call sites need (state setters, snapshot sub-objects, literals built in
   code): post-migration they build tagged values via generated constructors and `with_*`
   helpers, no re-serialization, no false rejections.

Result type: (typed value | undefined) x ordered diagnostics; every diagnostic is an error (no
severity field, no warnings); the assert-style wrapper throws on any diagnostic. Version gate
first, with the structured remediation payload.

## Numbers in TS

There is no TS-target auto-application of the 2^53 safe-integer constraint. Instead, a schema
must explicitly declare `safe_integers = true` (schema-wide); when declared, the constraint
applies in ALL backends, preserving verdict identity. Declaring a TS target for a schema that
lacks the declaration is a hard error at `dclrbl gen` time, telling the author to add it. So a
schema with a TS target always carries the declaration, and TS never meets an unrepresentable
integer: integer and number fields bind plain `number`, no BigInt anywhere. The number scalar
additionally hard-errors on any lexeme float64 cannot represent exactly — in every backend, so
verdict identity is untouched. Verdict identity comes from explicit schema declarations and
canonical rules — never from auto-application.

## Contents

- Lossless JSON, TOML, and JSONL parsers producing tagged, lexeme-retaining values.
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
`dclrbl-bin` in the browser, and no receiver-side upgrade path — by design, not by omission.
Node-side consumers that declare stores/channels in the manifest get generated checkpoint
wrappers that delegate to `dclrbl-bin`.

## Notes

- Browser and Node; no Node-specific APIs anywhere in the runtime or generated code (Node-only
  evidence resolvers and checkpoint wrappers live in explicitly Node-scoped entry points).
- Conformance applicability: TS runs ALL fixtures — JSON, TOML, and JSONL, raw-text and
  tagged-value forms, including lexical-number and datetime cases.
- Version pairing: exact match per release; dev builds pair only with themselves; the pairing
  hard error is the intended surfacing of skew under always-latest dependencies.
- Generated files land chmod 444 in consumer repos, carry `/* eslint-disable */` plus the
  generated-by header, and are formatted by the generator's own canonical emitter; `dclrbl
  gen` maintains prettier-ignore entries for the generated paths — consumers never
  hand-silence linters.
