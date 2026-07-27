# Appendix: Diff Certificate, Adjudication File, and doc-diff Output (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). VERSIONED: the shapes
> pinned here are conformance-tested as toolchain GOLDEN OUTPUT (single-engine determinism, not
> multi-target execution). Any change is a breaking-class, changelog-covered release event.
>
> META NOTE: Under the soft-freeze regime, pre-release field additions and refinements are
> expected and recorded per-release. The binding freeze is the first release. The certificate
> format is FORWARD-COMPATIBLE with the future analyzer's `proven` grade (decision 25): the
> analyzer emits the SAME format, and the gate consumes certificates without caring which engine
> produced them.

This appendix pins three artifacts:

- Part A — the `strictspec diff` CERTIFICATE (the empirical engine's output).
- Part B — the ADJUDICATION FILE (how a no-corpus consumer discharges the deploy gate).
- Part C — the `strictspec doc-diff` structured-delta output.

All are described as FIELD TABLES, not code. Values inside these artifacts render per
`appendix-rendering.md` Part A. Errors emitted while producing them use `STRICTSPEC_DIFF_*` /
`STRICTSPEC_DOCDIFF_*` (see `appendix-error-codes.md`).

## Part A — The diff certificate

`strictspec diff` takes a schema at two format versions, the migration M between them, and a
REQUIRED `--corpus <glob>` of real documents. It emits ONE certificate (JSON) whose claims each
carry an evidence grade. The certificate is the required input to rlsbl's `format_version`
deploy gate.

### A.1 Top-level fields

| Field | Type | Meaning |
|---|---|---|
| `certificate_format_version` | integer | Version of THIS certificate shape (this appendix). Distinct from any document `format_version`. Bumped as a breaking-class appendix change. |
| `schema_id` | string | Stable identity of the schema under test. |
| `old_format_version` | integer or the marker `"same-version"` | The N side. The literal string `"same-version"` when the run is a same-version flip-scan (decision 25). |
| `new_format_version` | integer | The N+1 side. In same-version mode this equals the format_version both schema revisions share. |
| `migration_set_id` | string, ABSENT in same-version mode | Identity of the migration M. Omitted entirely when `old_format_version` is `"same-version"` (no migration exists in that mode). |
| `corpus` | object (see A.2) | Corpus identity. |
| `claims` | array of claim objects (see A.3) | One entry per verified claim. |
| `strictspec_release` | string | The strictspec release that produced the certificate (pairs with the engine identity). |

### A.2 Corpus identity object

| Field | Type | Meaning |
|---|---|---|
| `declared_glob` | string | The exact `--corpus` glob as given. |
| `resolved_file_count` | integer | Number of documents the glob resolved to. Zero is a hard error (`STRICTSPEC_DIFF_CORPUS_EMPTY`), so a valid certificate always has ≥ 1. |
| `content_hash` | string | Aggregate content hash over the resolved documents (order-independent hash of per-file content hashes), so the corpus a certificate speaks for is pinned and re-checkable. |

### A.3 Per-claim entry

| Field | Type | Meaning |
|---|---|---|
| `kind` | enum: `flip-scan`, `migrate-round-trip-soundness`, `migrate-round-trip-completeness`, `down-taxonomy` | Which analysis produced the claim. In same-version mode the only claim kind is `flip-scan` (the same-version narrowing scan). |
| `grade` | enum: `violated`, `corpus-supported`, `proven` | `proven` is RESERVED for the future analyzer and never emitted by the empirical engine. |
| `counterexamples` | array of counterexample objects (see A.4), present IFF `grade == "violated"` | Absent or empty for `corpus-supported`. |
| `statement` | string | Human-readable statement of the claim (e.g. "every corpus document valid at N re-validates at N+1 after M"). |

### A.4 Counterexample object

| Field | Type | Meaning |
|---|---|---|
| `document_path` | string | Path to the corpus document that witnesses the violation (a real witness — no synthesis). |
| `diagnostics` | array of diagnostic objects | The FULL killing diagnostics: each carries `code`, `path`, and rendered `message` (per `appendix-error-codes.md` + `appendix-rendering.md`). |

### A.5 Grade semantics and the gate rule (normative)

- `violated` — a corpus document IS the counterexample; this is a proof of failure. Any
  `violated` claim BLOCKS release. Same-version flip-scan narrowings surface as `violated`
  claims and likewise block (they are un-bumped narrowings; pairs with the decision-13 bump
  rule).
- `corpus-supported` — no counterexample exists in the DECLARED corpus. This is explicitly NOT
  a proof; the corpus identity (A.2) records exactly what the claim is supported over. This is
  the GREEN LIGHT for the deploy gate over the consumer's declared at-rest corpus.
- `proven` — RESERVED. Machine-checkable proof object re-verified by the independent checker;
  emitted only by the future analyzer, in this same format.
- A consumer with NO corpus cannot reach `corpus-supported` and must discharge each otherwise
  unsupported claim via a committed ADJUDICATION FILE (Part B). There is NO bypass: a claim that
  is neither `corpus-supported`, `proven`, nor adjudicated blocks release.

## Part B — The adjudication file

A no-corpus consumer (e.g. claudestream greenfield, zero at-rest corpus) discharges the gate by
committing an adjudication file. It is itself a strictspec-schema'd TOML document (gated and
migrated like any document; its schema is toolchain-shipped). Its shape is pinned here.

### B.1 Adjudication top-level

| Field | Type | Meaning |
|---|---|---|
| `format_version` | integer | The adjudication schema's format_version (gated like any document). |
| `schema_id` | string | The schema whose migration is being adjudicated. |
| `old_format_version` | integer | N. |
| `new_format_version` | integer | N+1. |
| `adjudications` | array of adjudication entries (B.2) | One entry per unsupported claim being discharged. |

### B.2 Adjudication entry

| Field | Type | Meaning |
|---|---|---|
| `claim_kind` | enum matching A.3 `kind` | Which claim is discharged. |
| `scope` | string | The precise scope of the claim being discharged (e.g. a claim statement or claim identifier), so an adjudication covers exactly one claim and no more. |
| `justification` | string (non-empty) | The signed manual justification text — why this claim is safe absent corpus evidence. |
| `author` | string (non-empty) | Who adjudicated. |
| `date` | date scalar (RFC 3339 full-date) | When. |

Validation: the adjudication file is validated against its shipped schema; a malformed file is
`STRICTSPEC_DIFF_ADJUDICATION_INVALID`. A claim that is unsupported and unadjudicated is
`STRICTSPEC_DIFF_ADJUDICATION_MISSING`. Every `adjudications` entry must map to a real
unsupported claim in the certificate; a stray entry that matches no claim is an invalid
adjudication (over-broad or dangling scope).

## Part C — doc-diff output

`strictspec doc-diff` takes ONE schema and TWO documents of it (same schema, same
format_version) and emits a structured per-path delta. Golden-output conformance (toolchain
determinism). Values render per `appendix-rendering.md` Part A; the change detection is
LEXEME-CLASS-AWARE (`1` → `1.0` IS a change, because the classification differs).

### C.1 doc-diff top-level

| Field | Type | Meaning |
|---|---|---|
| `schema_id` | string | The shared schema. |
| `format_version` | integer | The shared format_version (both operands must agree; otherwise `STRICTSPEC_DOCDIFF_SCHEMA_MISMATCH`). |
| `deltas` | array of delta entries (C.2) | Per-path structural deltas, in traversal order (per `DESIGN.md` primitives appendix item 6). |

### C.2 Delta entry

| Field | Type | Meaning |
|---|---|---|
| `op` | enum: `added`, `removed`, `changed`, `moved` | The delta kind. `moved` is emitted only for array elements keyed by a declared `unique-by` (element identity is the unique-by key). |
| `path` | string | The path (per `appendix-rendering.md` Part B) at which the delta occurs. For `moved`, the path of the moved element in the NEW document. |
| `old_value` | rendered value, present for `removed` and `changed` | The value in the OLD document. |
| `new_value` | rendered value, present for `added` and `changed` | The value in the NEW document. |
| `old_index` | integer, present for `moved` | The element's index in the old array. |
| `new_index` | integer, present for `moved` | The element's index in the new array. |

Change detection rules:

- A value present in both documents at the same path but with a different rendered value OR a
  different lexeme class is `changed`. Lexeme-class awareness: `1` (integer) vs `1.0` (float) at
  the same path is a `changed` delta even though the numeric magnitude is equal.
- Array element MOVES are detected only when the array's schema declares a `unique-by`: elements
  are matched by their unique-by key across the two documents; a matched element at a different
  index yields `moved` (not removed+added). Arrays without a declared unique-by report
  positional `added`/`removed`/`changed` only.
- JSONL: deltas are line-scoped. Each delta additionally carries the JSONL line suffix in its
  `path` (per the path grammar) so a delta names both its in-document location and its line.

## Cross-references

- The diagnostics embedded in counterexamples: `appendix-error-codes.md`.
- Value/path rendering used throughout: `appendix-rendering.md`.
- The empirical-engine semantics, evidence grades, and the reserved `proven` grade:
  `appendix-semantics.md` and `DESIGN.md` decision 25.
- The gate's place in the version-boundary invariant and rlsbl integration: `DESIGN.md`.
