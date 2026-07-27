# Spike: `toml-eslint-parser` for strictspec's lossless TOML write path

Phase 0 spike validating decision 5 (TS full format parity: lossless,
lexeme-retaining TOML). It confirms that `toml-eslint-parser` — the same library
strictcli's TypeScript config splicer uses (`strictcli/typescript/src/toml.ts`)
— covers strictspec's broader lossless requirements: parse preserving every
lexeme, targeted edits where every untouched byte stays byte-identical, and
integer-vs-float lexeme-class distinction.

The technique is **AST-with-ranges + text splicing**: every value node carries a
precise `[start, end]` source range; all edits are pure string surgery on those
ranges, so anything not explicitly touched is copied verbatim.

## Running

```
npm install   # fetches toml-eslint-parser (network)
npm test      # node --test, runs test/*.test.mjs
```

`node_modules/` is gitignored. Library version: `toml-eslint-parser@1.0.3`,
Node 22. These tests are permanent acceptance evidence and are reused in the TS
runtime build phase. `lib/lossless.mjs` is the reusable splice core.

## Per-requirement verdict (24 tests, all passing)

### 1. AST completeness + spans (`test/01-ast-spans.test.mjs`) — PASS

Parses a torture document (`test/fixture.mjs`) exercising line + inline
comments, blank lines, standard tables, array-of-tables, inline tables, inline
and multiline arrays, all four string styles, integers (decimal-with-underscore,
negative, hex, octal, binary, 64-bit-max), floats (`1.0`, `3.14`, `1e5`, `-0.0`,
`6.626e-34`, `inf`, `-inf`, `nan`), booleans, all four datetime forms, dotted
keys, literal key spellings. Every scalar value node's range recovers the EXACT
original lexeme (43 values checked by resolved path; count asserted so nothing is
dropped). Key nodes and comment tokens also carry ranges.

### 2. Lexeme-class distinction (`test/02-lexeme-class.test.mjs`) — PASS (with one documented caveat)

`node.kind` cleanly separates `"integer"` from `"float"` — `1` vs `1.0` do not
collapse (same numeric value, different class). `inf`/`nan` are float-kind.
Integers expose an exact `bigint` (64-bit-max value verified beyond
`Number.MAX_SAFE_INTEGER`).

**Caveat (documented, not a blocker):** the convenience `node.number` field
**normalizes digit-group underscores away** — `1_000` → `"1000"`,
`0xDEAD_beef` → `"0xDEADbeef"`. So `.number` is NOT byte-lossless and must not be
used for spelling recovery. The source **range** recovers the raw spelling
exactly (`1_000`, `0xDEAD_beef`), which is the recovery path strictspec (like
strictcli) relies on. A regression test pins both behaviors so this never
silently changes.

### 3. Byte-identity under no-op (`test/03-byte-identity.test.mjs`) — PASS

The whole document is reconstructed once from the original inter-node gaps plus
each value node's lexeme recovered through its source range; sorting nodes by
range start and asserting the ranges are non-overlapping is a precondition, so
the tiling genuinely proves each range locates its lexeme. The result is
byte-identical to the original — no canonicalization, no reflow, no lost bytes.
Ranges are in-bounds and non-overlapping; parsing does not mutate the source.

### 4. Targeted edit fidelity (`test/04-targeted-edits.test.mjs`) — PASS

Each edit changes exactly one thing via a range splice; the (old, new) pair is
reduced to its single contiguous difference region via longest common
prefix + suffix, which proves every other byte is identical. Covered:

- **Scalar value replacement** — only the target value changes; sibling trailing
  comments and same-line inline comments survive verbatim.
- **Key rename** — only the key spelling changes; the value is untouched.
- **Append key to an existing table** — one additive line inserted after the
  table's last key, before the next table header.
- **Append key to the root block** — additive line, all other bytes identical.
- **Delete key + value line** — exactly that line vanishes; neighbours survive.

Splice output re-parses as valid TOML.

### 5. Error / edge behavior (`test/05-errors.test.mjs`) — PASS

Invalid TOML throws a typed `ParseError` carrying `index` (byte offset),
`lineNumber`, and `column`, plus a message — enough to build a structured
strictspec diagnostic pointing at the offending region. Unterminated strings and
empty values are rejected with position. Version gating works: a TOML-1.1-only
construct (trailing comma in an inline table) is accepted at `tomlVersion: "1.1"`
and rejected with a positioned error at `"1.0"` — the acceptance-gate lever
strictcli uses, available to strictspec.

## Limitations found

- **`node.number` is normalized, not raw** (underscores stripped). Range-based
  lexeme recovery is the only byte-lossless path. This is inherent to the
  splicing technique and already how strictcli operates, so it is not a blocker
  — but the write path must never use `.number`/`.value` for serialization,
  only the source range.

No other limitations surfaced. Comments, whitespace, value spellings, and string
styles all round-trip byte-exactly through range-based splicing.

VERDICT: CONFIRMS decision 5
