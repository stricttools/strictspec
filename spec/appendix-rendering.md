# Appendix: Value Rendering, Path Grammar, and Did-You-Mean (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). CROSS-TARGET
> normative — every rule here is asserted identically across all four conformance targets as
> part of full VERDICT + CODE + PATH + MESSAGE-TEXT identity. VERSIONED: any change is a
> breaking-class, changelog-covered release event triggering full conformance-fixture
> regeneration.
>
> META NOTE: Under the soft-freeze regime, pre-release refinements to these rules (truncation
> length, escape forms, edge outcomes) are expected and recorded per-release. The binding
> freeze is the first release.

This appendix pins two things that must be byte-identical everywhere: how a value is rendered
inside a diagnostic message (read side) AND how a constructed value is serialized (write side),
plus the path grammar and the did-you-mean algorithm. The message templates in
`appendix-error-codes.md` interpolate slots per this appendix; the write-side table implements
the canonical-serialization appendix leg of `DESIGN.md`.

## Part A — Value rendering

Two distinct tables. The DIAGNOSTIC table (A.1–A.5) governs values embedded in message text (they
may be truncated, always quoted for clarity) and is CROSS-TARGET normative. The WRITE-SIDE table
(A.6) governs values serialized back into a document (never truncated, byte-exact): its
constructed-value rules are cross-target normative, while untouched-value lexeme retention is
within-backend (backends emit different bytes for the same migrated document). Renderers may not
deviate from either table.

### A.1 Diagnostic value rendering (read side, in messages)

| Value kind | Rendered form | Notes |
|---|---|---|
| Integer | Decimal digits, optional leading `-`. No thousands separators, no `+`. | `1000`, `-42`. int64 domain. |
| Float | In a diagnostic message a float renders FROM ITS SOURCE LEXEME, UNCHANGED (exponent form is preserved: `1e3` renders `1e3`, `5.0` renders `5.0`). A CONSTRUCTED float that has no source lexeme renders per the canonical constructed-value rule (A.3), always carrying a `.` or an exponent marker — never a bare integer. | `1e3`→`1e3`; constructed `float64(5)`→`5.0`. |
| number scalar | Rendered per its SOURCE lexeme class: an integer-classed source renders as an integer, a float-classed source renders float-marked. | The `number` scalar's whole point is lexeme-class fidelity. |
| String | Double-quoted, with the escape set in A.2. Truncated per A.4. | `"hello"`, `"line\nbreak"`. |
| Boolean | `true` / `false`, lowercase. | Never `1`/`0`. |
| Null | `null`, lowercase. | The JSON/JSONL null value. |
| Negative zero | Integer `-0` renders `0` (int64 has no negative zero). Float `-0.0` renders `-0.0`, preserving the sign. | Pinned edge case; the sign is significant for floats and is retained. |
| Date | RFC 3339 full-date, e.g. `2026-07-27`. | Scalar kind `date`. |
| Time | RFC 3339 partial-time, e.g. `13:37:00` (or with fractional seconds as written). | Scalar kind `time`. |
| Datetime (offset) | RFC 3339 date-time with the source offset preserved verbatim (`+00:00` is NOT rewritten to `Z`). | Lexeme retention rule. |
| Datetime (local) | RFC 3339 local date-time, no offset. | Local kind. |
| Array (in message) | Truncated inline form per A.5. | e.g. `[1, 2, 3, ...]`. |
| Record (in message) | Truncated inline form per A.5. | e.g. `{a: 1, b: 2, ...}`. |

### A.2 String escaping (both sides)

Within double quotes, exactly these escapes are produced, and nothing else:

| Character | Escape |
|---|---|
| `"` (U+0022) | `\"` |
| `\` (U+005C) | `\\` |
| newline (U+000A) | `\n` |
| carriage return (U+000D) | `\r` |
| tab (U+0009) | `\t` |
| other control chars U+0000–U+001F | `\u00XX` (lowercase hex, four digits) |

All other code points — including non-ASCII — are emitted verbatim as UTF-8. No implicit
Unicode normalization (code-point identity, per `DESIGN.md` primitives appendix item 10). No
`\uXXXX` escaping of printable non-ASCII.

### A.3 Float rendering (canonical) — constructed values only

The canonical form below applies ONLY to a CONSTRUCTED or type-coerced float — a float value with
NO source lexeme (produced by a generated constructor or a migration op). A float that HAS a
source lexeme is never canonicalized: it renders from that lexeme unchanged in diagnostic messages
(A.1) and serializes byte-identically on the write side (A.6, lexeme retention). This is the pin
that resolves the former "does `1e3` render `1000.0`?" ambiguity: exponent-form lexemes are
retained verbatim wherever a source lexeme exists; canonicalization touches only lexeme-less
constructed values.

- A constructed float renders with a decimal point OR an exponent, never as a bare integer:
  `float64(5)` renders `5.0`.
- The canonical form is the SHORTEST decimal string that round-trips to the same float64 (the
  standard shortest-round-trip algorithm): a value with no fractional part gains a trailing `.0`;
  a value whose shortest round-trip requires exponent notation uses a lowercase `e` with a signed
  exponent. The chosen shortest form is identical across Go, Python, and TS by construction (all
  three implement shortest-round-trip IEEE-754 formatting; the conformance suite asserts identity).

### A.4 String truncation (diagnostic messages only)

- Maximum rendered string length inside a message: 64 code points of CONTENT (measured after
  unescaping, i.e. source code points, not escape-expanded characters).
- When a string exceeds 64 code points, render the first 64 code points followed by the pinned
  ellipsis: the three-character ASCII sequence `...`, appended immediately before the closing
  quote: `"<first 64 code points>..."`. (The ellipsis is exactly these three ASCII dots — never
  the single U+2026 HORIZONTAL ELLIPSIS character.)
- Truncation never splits an escape sequence: if the 64th code point would land inside a
  rendered escape, the whole escaped character is dropped and the ellipsis follows.
- Write side is NEVER truncated.

### A.5 Container inline rendering (diagnostic messages only)

- Arrays render `[e1, e2, e3, ...]` where each element renders per A.1 (recursively,
  truncated). At most 3 elements are shown; if the array has more, `, ...` follows the third.
  An empty array renders `[]`.
- Records render `{k1: v1, k2: v2, k3: v3, ...}` in document order, keys unquoted when they are
  identifier-shaped (`[A-Za-z_][A-Za-z0-9_-]*`), otherwise quoted per A.2. At most 3 pairs are
  shown; `, ...` follows the third when more exist. An empty record renders `{}`.
- Nesting is rendered to at most 2 levels deep; deeper values render as their kind sentinel:
  `{...}` for a record, `[...]` for an array.
- Containers are rendered inline only when a template's slot is a container value; most
  diagnostics report scalar `{got}`/`{actual}` values.

### A.6 Write-side value rendering (serialization, normative)

The write-side table governs values serialized BACK into a document (never truncated,
byte-exact). It is the canonical-serialization appendix leg of `DESIGN.md` and is cross-target
normative WITHIN A BACKEND (Go, Python, and TS emit different bytes for the same migrated TOML;
each backend is self-consistent).

| Value kind | Write-side rendering | Notes |
|---|---|---|
| Any UNTOUCHED value | Byte-identical to its retained SOURCE LEXEME. | Lexeme retention: nothing re-renders what an op did not change. `1e3` stays `1e3`, `+00:00` stays `+00:00`, `007` stays `007` where the source allowed it. |
| Constructed integer | Decimal digits, optional leading `-`, no `+`, no separators. | int64 domain. |
| Constructed float | Canonical shortest-round-trip form (A.3), always float-marked. | `float64(5)` serializes `5.0`, never `5` — so a wrapped `5.0` writes `[5.0]`. |
| Constructed number | Serialized per the SOURCE lexeme class it was constructed with (integer-class → integer lexeme; float-class → float-marked). | The `number` scalar preserves lexeme-class fidelity on write. |
| Constructed string | Double-quoted with the A.2 escape set; never truncated. | Write side is never truncated (contrast A.4). |
| Constructed boolean | `true` / `false`, lowercase. | |
| Constructed null | `null` (JSON/JSONL only; TOML has no null). | |
| Constructed date / time / datetime | RFC 3339 with the declared kind; offset forms keep the constructed offset. | Constructed datetimes are the only datetimes not covered by lexeme retention. |
| Negative zero | Float `-0.0` retains its sign; integer `-0` serializes `0`. | Matches A.1's diagnostic pin. |

- PRODUCER-CURRENT-ONLY: the write path hard-errors when asked to serialize a document at any
  `format_version` other than the schema's current one (`STRICTSPEC_SERIALIZE_NONCURRENT`) — no
  conforming producer creates staleness.
- Key order follows document order; whitespace and comments on untouched regions are preserved
  (within-backend round-trip fixpoint).

### A.7 Regex-valued string slots (normative)

A slot whose value is a regular expression — `{pattern}` in `STRICTSPEC_VALUE_STRING_REGEX`,
`STRICTSPEC_VALUE_MAP_KEY_REGEX`, and `STRICTSPEC_SCALAR_LEXEME` — is a STRING slot and renders
EXACTLY like any other string slot: double-quoted, with the A.2 escape set applied UNIFORMLY, and
truncated per A.4. There is NO verbatim special case: a regex is never emitted bare, and its
metacharacters (`\`, `"`, control chars) are not exempt from A.2 escaping. Example: the pattern
`^\d+$` renders `"^\\d+$"`; the pattern containing a literal quote `a"b` renders `"a\"b"`.

## Part B — Path grammar (EBNF, normative)

Diagnostic paths are part of the conformance identity guarantee. A path names the location of a
value within a document (and, for JSONL, within a stream). Rendered per this grammar on every
target.

```ebnf
path            = root , { step } , [ jsonl-suffix ] ;
root            = "$" ;                          (* the document root *)
step            = key-step | index-step | map-key-step | arm-step ;

key-step        = "." , key-name ;               (* record field *)
key-name        = ident | quoted-key ;
ident           = ( letter | "_" ) , { letter | digit | "_" | "-" } ;

index-step      = "[" , index , "]" ;            (* array element *)
index           = digit , { digit } ;            (* zero-based *)

map-key-step    = "[" , quoted-key , "]" ;       (* typed-map key *)
quoted-key      = '"' , { key-char | escape } , '"' ;
key-char        = ? any code point except '"' , '\' , or a control char ? ;
escape          = "\\" , ( '"' | "\\" | "n" | "r" | "t" | "u" , 4 * hexdigit ) ;

arm-step        = "(" , arm-name , ")" ;         (* union-arm disambiguation *)
arm-name        = ident ;

jsonl-suffix    = "@" , "L" , line , ":" , byte-offset ;
line            = digit , { digit } ;            (* one-based line number *)
byte-offset     = digit , { digit } ;            (* zero-based byte offset within the line *)

letter          = "A"…"Z" | "a"…"z" ;
digit           = "0"…"9" ;
hexdigit        = digit | "a"…"f" ;
```

Rules and clarifications:

- ROOT is always `$`. A diagnostic at the document root renders path `$`.
- KEY STEPS use dotted notation when the key is `ident`-shaped. A record key that is NOT
  ident-shaped (contains special characters, spaces, leading digit) is rendered as a MAP-KEY
  STEP with quoting: `$.config["weird key"]`. This is the "index-then-key switching" rule —
  the renderer switches from `.key` to `["key"]` exactly when the key is not ident-shaped.
- INDEX STEPS are zero-based and always bracketed: `$.items[0]`.
- MAP-KEY STEPS quote the key per A.2 escaping and bracket it: `$.headers["Content-Type"]`.
  Map keys are compared and rendered by code points (no normalization).
- ARM STEPS disambiguate which union arm produced a nested diagnostic:
  `$.shape(gradient).stops[0]`. The arm name is the schema-declared arm identifier. Arm steps
  appear only when a matched discriminated-union arm's body produced the diagnostic.
- JSONL SUFFIX addresses a value within a stream: `@L<line>:<byteoffset>`, appended after the
  in-document path. Line numbers are ONE-based (human-facing); byte offsets are ZERO-based
  within the line. Example: `$.budget@L42:17`. A whole-line diagnostic (e.g. a parse failure)
  renders `$@L42:0`.
- Separators: `.` before ident key steps; no separator before `[`, `(`, or `@`.
- `{path}` SLOT AUTO-INJECTION (template contract, normative): the `{path}` slot in every
  catalogue template is bound AUTOMATICALLY from the diagnostic's own path — the location the
  diagnostic is attached to — and rendered per this grammar. Emitters and generated renderers
  NEVER bind `{path}` manually; it is supplied by the diagnostic infrastructure from the current
  traversal path (the path-push / path-pop discipline of `appendix-emitter-ir.md`). A template
  that references `{path}` therefore needs no hand-supplied slot value; the catalogue rows whose
  Slots column omits `path` while the template references it (e.g. `STRICTSPEC_PARSE_JSON_SYNTAX`)
  rely on exactly this auto-injection.

## Part C — Did-you-mean (normative, pinned)

Five decided parameters, normative — every target implements them identically and the
`suggestion` slot is conformance-asserted as part of message-text identity:

1. METRIC: Levenshtein edit distance (insertions, deletions, substitutions each cost 1;
   no transposition).
2. CASE SENSITIVITY: case-SENSITIVE. `Foo` and `foo` are at distance 1, not 0.
3. THRESHOLD: 2. Only candidates within edit distance 2 (inclusive) of the unknown token are
   eligible.
4. MAX SUGGESTIONS: at most 3. When more than 3 candidates tie or qualify, the first 3 in the
   ordering below are taken.
5. ORDERING / TIE-BREAK: primary sort by ascending edit distance; ties broken ALPHABETICALLY
   (code-point order, ascending). This fully determines the suggestion set and order.

Application:

- The candidate set is the known-key set at the relevant scope (record fields, enum members,
  discriminator values), as declared by the schema.
- If no candidate is within distance 2, the `suggestion` slot renders empty (the message's
  `{suggestion}` interpolates to the empty string; templates that append ` Did you mean {name}?`
  omit that clause entirely).
- With one qualifying candidate, the rendered clause is ` Did you mean {name}?`. With two or
  three, ` Did you mean {n1}, {n2}, or {n3}?` (Oxford comma; two candidates: ` Did you mean {n1}
  or {n2}?`). Candidate names render per A.1 as strings if not ident-shaped, bare otherwise.

## Part D — Gated-constraint condition rendering (normative)

Three diagnostics interpolate a `{condition}` slot describing the gate that triggered a gated
constraint: `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED`, `STRICTSPEC_INTRA_CONDITIONAL_VALUE`, and
`STRICTSPEC_INTRA_FORBIDDEN_WHEN`. The condition is one of the CLOSED six-kind gate-condition set
(`appendix-emitter-ir.md` §4 — present, absent, equals-literal, not-equals-literal, in-literal-set,
not-in-literal-set), each over a named field and literal operands. Its rendered form is pinned:

| Condition kind | Rendered form |
|---|---|
| present | `{field} present` |
| absent | `{field} absent` |
| equals-literal | `{field} == {literal}` |
| not-equals-literal | `{field} != {literal}` |
| in-literal-set | `{field} in [{l1}, {l2}, ...]` |
| not-in-literal-set | `{field} not in [{l1}, {l2}, ...]` |

- `{field}` is the schema-declared field name, rendered BARE (it is always identifier-shaped).
- Each literal renders per Part A (A.1): string literals are double-quoted with A.2 escaping,
  integers decimal, booleans lowercase `true`/`false`, and so on.
- The literal set is rendered IN FULL — comma+space separated inside `[...]`. It is a finite
  schema-declared set, NOT a runtime container, so the A.5 three-element truncation does NOT apply.
- Examples: `status == "active"`; `tier in ["gold", "silver"]`; `count != 0`; `flag present`;
  `x in [1]`.

## Soft-freeze amendment log

Per the soft-freeze regime (see the META NOTE above), pre-release refinements are recorded here.

- 2026-07-27 — Added A.7: regex-valued string slots render identically to any other string slot
  (double-quoted, A.2 escaping applied uniformly, A.4 truncation); no verbatim special case.
  Resolves a deferred conformance-fixture question about regex slot rendering.
- 2026-07-27 — Pinned the `{path}` slot auto-injection contract in Part B: `{path}` is bound from
  the diagnostic's own path by the diagnostic infrastructure; emitters never bind it manually.
  Promotes an established conformance-harness convention into the appendix.
- 2026-07-27 — Added Part D: the gated-constraint `{condition}` rendering scheme. Promotes an
  established conformance-harness convention into the appendix.

## Cross-references

- Codes and the templates that interpolate these slots: `appendix-error-codes.md`.
- Write-side revalidation and producer-current-only rule: `DESIGN.md` (canonical
  serialization appendix).
- Semantics of the constructs whose values are rendered here: `appendix-semantics.md`.
- Certificate/doc-diff rendered values reuse Part A: `appendix-certificates.md`.
