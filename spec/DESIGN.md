+++
description = "The strictspec constitution: the language-neutral definition of the schema language, constraint vocabulary, op set, error model, versioning and migration rules, and the normative appendices."
+++
# spec/ — The strictspec Schema Language

The constitution: the language-neutral definition of strictspec schemas, the constraint
vocabulary, the error model, the read-side primitives appendix, the write-side
canonical-serialization appendix, the generated API contract, the versioning and migration
rules, the version-boundary invariant, the domain-check architecture, the accepted-set formal
semantics, and the meta-schema. The detailed pinned tables live in dedicated appendix files
alongside this document and are versioned with it: the concrete TOML surface syntax
(appendix-surface-syntax.md); the error-code catalogue and message templates
(appendix-error-codes.md); value rendering, path grammar, and did-you-mean
(appendix-rendering.md); the diff certificate and doc-diff output shapes
(appendix-certificates.md); the per-construct formal semantics and undecidability catalogue
(appendix-semantics.md); custom scalar registration (appendix-custom-scalars.md); and the
shared emitter IR (appendix-emitter-ir.md). Every backend and the internal interpreter
implement this document and its appendices; the conformance suite enforces it across all four
targets: verdict, error-code, path, and message-text identity (ordered; messages render from
the spec-pinned templates). This document, rendered by selfdoc, is also strictspec's published
language reference (decision 27) — the canonical, citable manual, versioned with the spec.

Schemas are authored in TOML (canonical, sole source syntax). JSON Schema is an export target
for editor tooling, never a source.

## Document model

Format-plural: one model, three syntaxes (JSON, TOML, JSONL) in every backend, including TS.
YAML is excluded, FINAL — anchors, implicit typing, and ambiguous scalar forms are hostile to
lexeme retention and byte-stable writes. Consumers convert once, at migration time.

- Values are TAGGED and LEXEME-RETAINING: every scalar carries its classification (integer /
  float lexeme class; datetime kind) and its source lexeme. This is what makes both verdict
  identity (read side) and byte-stable writes (write side) possible.
- Maps are ordered. A schema section declares whether order is semantic or incidental.
  "Semantic" means: the typed binding preserves order (Go binds to `[]KV`, TS to `Map` — JS
  objects reorder integer-like keys, so plain objects are unusable), serialization preserves
  order, traversal follows it. Constraints do not read order.
- Comment-preserving TOML round-trip: go-toml-edit (Go), tomlkit (Python), the runtime's own
  lossless TOML parser (TS). Different engines: round-trip fidelity is within-backend
  (read-then-write fixpoint), never across backends — backends emit different bytes for the
  same migrated TOML.
- JSONL: stream of documents, one per line, each independently validated. Readers process
  line-by-line (STREAMING) in all backends and the CLI: memory is bounded by the largest line,
  not the file; all-errors-in-one-pass applies per line; positions are byte offsets. LF-only
  splitting; trailing CR invalidates the line. Blank lines and a final line without LF are
  pinned edge cases (see appendix item 9). Append is SINGLE-WRITER by declaration: one O_APPEND
  write per complete line; rewrite is temp+rename (reader-safe, not writer-safe); cross-process
  coordination is the consumer's job. Stream-level constraints use the cross-document
  constraint vocabulary (see Domain checks).
- JSON duplicate keys are a canonical hard error in every backend (Python must read via
  object_pairs_hook to even see them; silent last-wins is the typo'd-field failure mode in
  disguise).
- Datetime values are first-class scalars (see Construct set): TOML native datetime/date/time
  lexemes bind to them; JSON carries RFC 3339 strings. A TOML datetime lexeme in a field NOT
  typed as a datetime scalar is an ordinary type-mismatch error, not a special case.
- Scalar rules (canonical): a float lexeme is not an integer (`3` vs `3.0` — lexical,
  preserved in every backend); booleans are not integers; non-finite numbers rejected.
- Special leaf types: opaque JSON object (verbatim, never introspected) — every such leaf MUST
  declare either `consumer_check = "<name>"` (the named check is CONSUMER-NATIVE code over the
  typed values — declared here so the blind spot is on record; strictspec never executes it) or
  `unchecked = true` WITH a mandatory `unchecked_reason = "<why>"`; omission of the stance or
  of the reason fails the meta-schema. Both stances are strictspec-blind by written
  declaration: a typo inside the blob is invisible to strictspec — and `strictspec check`
  prints the complete inventory of unchecked subtrees (with reasons) AND consumer-check
  declarations, so every blind spot is visible in review. Opaque domain string (validated by a
  named consumer check, same declaration).

## Primitives appendix (read side, normative)

Scope note: ALL items are CROSS-TARGET normative. Items 1–4, 6, 8, 9, 10, 11 determine
verdicts, codes, and paths; items 5 and 7 (canonical value rendering, did-you-mean) feed the
spec-pinned message templates — rendering and suggestions are deterministic and asserted
identically across all four targets as part of full message-text identity (see Error model).

1. Regex: RE2-compatible subset ONLY, vetted at generation/schema-validation time; anything
   RE2 rejects makes the SCHEMA invalid. Identical behavior in Go regexp, Python re, JS RegExp
   within the subset.
2. String length: Unicode code points. Never bytes, never UTF-16 units.
3. Integer domain: int64. The safe-integer constraint is NOT auto-applied. A schema must
   EXPLICITLY declare `safe_integers = true` (schema-wide); when declared, every backend rejects
   |n| >= 2^53 for that schema, preserving verdict identity across backends, so TS never meets an
   unrepresentable integer and binds plain `number`. Declaring a TS TARGET for a schema that
   lacks the declaration is a hard error at generation time telling the author to add it. No
   BigInt anywhere. Schemas without the declaration keep full int64 in all backends (TS still
   binds `number`).
4. Number-lexeme classification (all lossless parsers): a lexeme containing `.`, `e`, or `E`
   is a FLOAT (`1e3` is a float); otherwise an INTEGER parsed as int64, overflow = canonical
   hard error. Float lexemes beyond float64 range = canonical hard error. For the NUMBER
   scalar: any lexeme whose exact value float64 cannot represent (an integer lexeme above
   exact-float64 range, or a decimal lexeme beyond float64 precision) = canonical hard error —
   the bound value never silently diverges from the lexeme. `-0`/`-0.0` pinned in the
   rendering table (appendix-rendering.md).
5. Canonical value rendering (cross-target normative): the normative table for values inside
   diagnostic messages on every target — integers, floats, datetimes, strings
   (quoting/escaping), booleans, null, negative zero, truncation — is pinned in
   appendix-rendering.md. Messages never embed platform-dependent content; positions are
   structured fields.
6. Traversal order: document order for present keys, schema-declaration order for
   missing-required, index order for arrays. Phase-2 diagnostics follow phase 1, ordered by
   traversal order of containing records, then check-declaration order. Fixture-asserted order
   is emission order; renderers may not reorder.
   - INTERLEAVING AMENDMENT (2026-07-27): item 6 pinned the order WITHIN each of
     the present-key and missing-required groups but not the relative order BETWEEN them. Pinned
     reading (the one the corpus already implied — `pgdesign-account-invalid` emits a missing
     required field between the two present fields that bracket it in declaration order): within a
     record, each missing-required diagnostic is emitted immediately before the present-key
     diagnostic of the first field, in schema-declaration order, that is BOTH declared after it and
     present in the document; a missing-required field declared after every present field is emitted
     last. Present-key diagnostics keep document order; missing-required diagnostics keep
     declaration order among themselves. Conformance: `order-present-keys-document-order` (pure
     present-key reordering) and `order-mixed-present-and-missing` (a trailing missing-required
     after two reordered present violations).
7. Did-you-mean (cross-target normative): the pinned edit-distance metric, threshold, and
   tie-break (alphabetical) over the known-key set are in appendix-rendering.md. Every target
   implements it identically; the `suggestion` field is conformance-asserted as part of
   message-text identity.
8. Path rendering: the grammar for rendering a path (segment separators, index form,
   key-quoting, index-then-key switching) is pinned once in appendix-rendering.md. CROSS-TARGET
   normative: paths are part of the conformance identity guarantee.
9. Edge inputs: empty text, whitespace-only documents, empty TOML file, JSONL blank lines and
   final-line-without-LF — each has one pinned outcome.
10. Unicode identity: NO implicit normalization anywhere. Map keys, duplicate detection, and
    uniqueness compare by code points. unique-by / pairwise-distinct normalization options are
    an enumerated closed set: `case-fold`, `trim`. Nothing else.
11. Datetime lexeme rules: three scalar kinds — `date`, `time`, `datetime` (offset or local,
    the schema declares which; a mismatch is a canonical hard error). TOML natives bind
    directly; JSON binds RFC 3339 strings (a non-conforming string in a datetime-typed field
    is a canonical hard error). Lexemes are retained; no normalization on read (a `+00:00`
    offset is not rewritten to `Z`); comparisons for range constraints use the instant (offset
    forms) or the naive value (local forms) — comparing across the two is a schema error at
    meta-schema time. Precision is preserved as written.

## Canonical serialization appendix (write side, normative)

The write path is the same drift surface as the read path and gets the same treatment:

- Values untouched by an operation serialize BYTE-IDENTICALLY to their source lexeme (the
  document model retains lexemes; nothing re-renders what it didn't change).
- Constructed or type-coerced values render per the pinned value-rendering table
  (appendix-rendering.md): a float value always renders with a float lexeme — Go's
  `float64(5)` marshals as `5.0`, never `5`; constructed datetimes render RFC 3339 with the
  declared kind; strings escape one way; key order = document order; whitespace pinned.
- PRODUCER-CURRENT-ONLY (the boundary invariant's first leg): the write path hard-errors when
  asked to serialize a document at any format_version other than the schema's current one. No
  conforming producer can create new staleness.
- Consequence, by construction: migration output revalidates against the target schema
  (the flagship wrap_in_array of `5.0` writes `[5.0]`).
- Fixtures: JSON write fixpoint (read-then-write byte-identity), post-migration revalidation,
  non-current-version write refusal.

## Generated API contract (normative)

What every emitter emits, uniformly:

- TWO entry points per schema per language:
  1. raw text/bytes -> lossless parse -> validate;
  2. a TAGGED document-model value -> validate. Tagged values come from the backend's lossless
     parser or from GENERATED TYPED CONSTRUCTORS (where integer/float/number/datetime is
     explicit in the type). Raw untagged objects/dicts/maps are NOT accepted — ambiguity never
     enters the model. This serves in-memory mutate-then-validate consumers without
     re-serialization.
- Result type: (typed value | nothing) x ordered diagnostics list. Every diagnostic is an
  error — there is no severity field and no valid-with-warnings outcome. Assert-style wrappers
  throw/error on any diagnostic.
- The version gate runs first, before structural validation; its diagnostics use the
  three-message pattern with the STRUCTURED remediation payload (got format_version, expected
  format_version, schema id, migration-set id, exact `strictspec migrate` invocation).
- Typed values are immutable (frozen dataclasses / readonly TS types / Go values). Every
  record type gets generated `with_*` copy helpers (per-field copy-on-write; list edits
  produce new values) so mutate-then-revalidate stays typed end to end.
- Bindings: semantic-order maps -> `[]KV` (Go) / `Map` (TS) / dict-preserving wrapper (Python,
  insertion-ordered); tuples -> fixed-size forms; `number` -> float64/`number`/`float`;
  datetimes -> the backend's pinned datetime binding (Go the RETAINED LEXEME as a `string`, kind
  guard retained; Python datetime/date/time; TS a tagged runtime type — never the platform Date
  for local kinds). DATETIME-BINDING AMENDMENT (2026-07-27): the Go binding is the
  retained lexeme string, NOT a parsed `time.Time`. Parsing to `time.Time` would normalize on read
  (rewrite a `+00:00` offset to `Z`, drop written sub-second precision), violating the
  lexeme-retention invariant (appendix item 11: lexemes retained, no normalization on read). The
  KIND GUARD (date / time / datetime discrimination, offset-vs-local) is retained in full; only the
  carrier changed from `time.Time` to `string`. This matches go/DESIGN.md's already-amended note and
  the implementation (`internal/emit` goType emits `string` for datetime-kinded fields). Freezing is
  shallow-plus-generated-immutability (stated honestly).
- Partial-subtree binding: a record binds to its typed form when ITS phase 1 passed, even if a
  sibling failed — required by phase-2 domain checks.
- Cross-document constraint forms execute via the runtime's ported CONSTRAINT ENGINE (see
  Domain checks) — there is no consumer registration surface. Generated code declares which
  forms and which evidence resolvers each document type needs; invoking validation with domain
  checks in an environment that cannot satisfy a required resolver is a hard error naming the
  resolver. Consumer-native bespoke checks run DOWNSTREAM of validation, in consumer code,
  over the typed values — they emit diagnostics with consumer-prefixed codes via the runtime's
  constructor and are outside the conformance guarantee by declaration.
- BOUNDARY CHECKPOINT ARTIFACTS (see the version-boundary invariant): where the consumer
  manifest declares a store or a channel, the generator additionally emits the store's
  ingest write-door (migrate-then-persist, atomic) and the channel's negotiation/egress
  wrappers. Consumers that declare no boundaries get no checkpoint code — they pay nothing.
- Generated-file hygiene: every file embeds the generator version, schema name +
  format_version, regeneration command, generated-by header, the MIT/unencumbered notice, and
  the target ecosystem's machine-readable generated-file and lint-suppression markers
  (Go `// Code generated by strictspec. DO NOT EDIT.`; Python `# ruff: noqa`; TS
  `/* eslint-disable */`). Repo-level ignore files that some tools require (prettier) are
  maintained by `strictspec gen` for the manifest-declared generated paths.

## Construct set

STATUS — STABLE BUT GROWING in the GROWTH PHASE (decision 3, as reframed 2026-07-28), settled as
of Phase 3.3 (2026-07-27). The construct-set stability GATE is MET: all fifteen examples/ drafts
came back clean (or with findings absorbed into spec/), and every gap note carries a resolved
RESOLUTION (Phase 3.3) block. The construct set absorbed, over its two amendment rounds: the pinned
concrete TOML surface syntax (appendix-surface-syntax.md); the aggregate forms
count-limit/sum-limit; the intra-document forms conditional-value and collections-disjoint; the
closed six-kind gated-form condition set; and the enum-source selector grammar, ranges-disjoint
well-formedness, migration author-supplied down-ops, and aggregate-selection pins. Six candidate
shapes were REJECTED with recorded rationale (numeric comparison predicates, array-contains-literal
gates, reference-target predicates, cross-scope existential forbidden-when, open-namespace reference
resolution, enum-typed map keys) — kept as recorded history; a later recurrence is a
vocabulary-evolution conversation. Additions and amendments (new constructs via the examples/
gap-note process; semantics corrections; error-code catalogue growth) are NORMAL and EXPECTED in
the growth phase, recorded per release through the existing discipline; a new construct still
enters only through the examples/ gap-note process. Released-surface compatibility is governed by
semver at release boundaries.

Bounded to the analyzed corpus. The construct-set stability gate was all examples/ drafts (claudestream
and PixelWeaver first; the construct-only exercises for shared types, enum baking, and aggregates;
and the BetterClaude and imagine corpus-DRAFT sources) coming back clean plus resolved gap notes.
This SUPERSEDES the former demobl/F/step paper-schema precondition — those donors left the corpus.

- Closed records and typed maps (distinct constructs); regex on map keys.
- Named types; recursive references; documents are trees; a pinned max validation depth with
  its own canonical diagnostic (value chosen so the diagnostic fires before CPython stack
  exhaustion — multiple frames per nesting level under the default 1000-frame limit).
- Shared type-definition files (decision 21): a schema may import named TYPES from dedicated
  type-definition files — TYPES ONLY. No cross-file constraints, no transitive imports. A
  type-definition file is itself a versioned document of the meta-schema (gated and migrated
  exactly like a schema). Cross-file constraint references and transitive imports are
  meta-schema rejections. This and enum sourcing (below) are the ONLY two sanctioned edges out
  of an otherwise self-contained single schema file.
- Enum arms sourced from a document (decision 32): a schema may declare an enum whose arms are
  SOURCED FROM a named field of a named document. The toolchain BAKES the arms into the
  generated code at `gen` time; `strictspec gen` and `strictspec check` HARD-ERROR on staleness
  — baked arms differing from the current source document. This is a sanctioned data→schema
  dependency edge, in the open and toolchain-gated. Removing an arm is a NARROWING edit (it
  shrinks the accepted-document set) and triggers the format_version bump rule (see
  Versioning); the same-version flip-scan mechanically catches an un-bumped arm removal. The
  arm-SELECTOR grammar is pinned (appendix-surface-syntax.md §7): a restricted projection path of
  key steps and `[]` array-flatten steps (e.g. `sounds[].name`), with NO key wildcards, NO index
  selection, and NO filtering; that grammar IS the accept/reject boundary of
  `STRICTSPEC_ENUMSRC_BAD_SELECTOR`, and the selector must resolve to a flat sequence of
  string-typed leaves.
- Discriminated unions (literal-valued discriminator field) PLUS bounded NODE-KIND unions:
  undiscriminated unions allowed only when arms differ by node kind (scalar vs record vs
  array) — deterministic without a discriminator; covers predraw's fill-or-gradient.
  Same-kind arms require a discriminator.
- Nullable unions (T | null), T may be a union. Unusable with TOML documents: validating a
  TOML-syntax document against a schema in which a nullable union is reachable is a canonical
  hard error; TOML-targeting schemas model "no value" as an optional (absent) field. Fully
  available for JSON and JSONL. Where document syntax is known at schema-validation time, this
  is additionally rejected at the meta-schema level (fail earliest).
- Scalars: string, integer, float, NUMBER (accepts both lexeme classes, binds float64,
  hard-errors on lexemes float64 cannot represent exactly, rendering preserves lexeme class),
  boolean, `date`, `time`, `datetime` (offset/local declared per field; appendix item 11);
  enums and literal constants. The datetime set (`date`/`time`/`datetime`) is COMPLETE for the
  corpus — there is NO `duration` scalar (decision 35); future demand for one routes through
  custom scalar registration, never a new built-in.
- Safe integers: optional schema-wide `safe_integers = true` declaration; when set, every
  backend rejects |n| >= 2^53 (verdict identity preserved across backends). MANDATORY whenever
  the schema declares a TS target — a TS target without it is a hard error at generation time.
  No BigInt; TS binds plain `number` either way.
- Constraints: numeric ranges (inclusive/exclusive), datetime ranges (same-kind only), string
  length bounds (code points), regex on values and map keys, array length bounds, non-empty
  string.
- Tuples; tuple and array-length bounds mutually exclusive (meta-schema error).
- Aliases: permanently co-valid spellings WITHIN one schema version, one canonical spelling on
  write; both-present = hard error; exempt from unknown-key errors; canonicalization preserves
  attached comments; may not target a discriminator. Renames between versions are migrations.
- Defaults: NOT a construct (removed 2026-07-12; decision 30). A field is required or
  optional; an absent optional field binds as ABSENT — no bind-time injection, ever. A typed
  value never carries data the author didn't write; consumers handle absence in code, visibly.
  Format evolution injects literals explicitly, once, at migration time (add_field /
  merge_defaults). A `default` key in a schema fails the meta-schema with a dedicated
  diagnostic.
- Per-field descriptions (feed the exported metadata and scaffold comments).
- Custom scalar registration: part of the language design (a named scalar with a toolchain-
  registered lexeme rule, binding, and rendering entry — pgdesign is the first consumer);
  build-sequenced after the acceptance test, not a design exclusion. The registration surface
  (lexeme rule, binding, and rendering-entry format) is pinned in appendix-custom-scalars.md.

NOT in the language: allOf, not, if/then/else, patternProperties, format keywords,
uniqueItems, multipleOf; defaults; schema composition BEYOND named-type imports from
type-definition files (no cross-file constraints, no transitive imports); YAML. Exclusions are
re-examined only through the examples/ gap-note process.

## Union diagnostics (normative)

Discriminated unions: discriminator missing -> one diagnostic listing valid values, no arm
validated; discriminator not a declared literal -> enum-style diagnostic, no arm validated;
discriminator valid + body invalid -> the matched arm's diagnostics at natural paths with the
arm name as context; non-matching arms NEVER validated. Node-kind unions: input node kind
selects the arm; a node kind matching no arm -> one diagnostic naming the accepted kinds.
Nullable: null short-circuits (for JSON/JSONL documents; a reachable nullable union against a
TOML document is a canonical hard error, per the construct set).

## Cross-field and cross-document constraint vocabulary

Attach at any nesting scope; implemented ONCE in the shared emitter IR (appendix-emitter-ir.md);
conformance-tested for verdict+code+path+message identity like every structural check.

Intra-document forms (decidable from document + schema alone):

| Form | Example origin |
|---|---|
| conditional-required (presence- or value-triggered) | orxtra retry != 0 => retry_resume |
| conditional-value (gate holds ⇒ target equals a literal) | wavescript Pin (unison == 1 when source ∈ {noise,pink,brown}); rlsbl preid=="stable" ⇒ bump=="prerelease" |
| exactly-one-of / at-least-one-of | orxtra routing; predraw placeStep |
| co-presence (A iff B) | orxtra provider iff model |
| mutual exclusion | pgdesign body XOR file (field-level: at most one of a field set present) |
| collections-disjoint (two sibling arrays share no element; normalization case-fold/trim) | rlsbl include/exclude (element-level set disjointness) |
| forbidden-when | tunebox drums forbid params |
| unique-by (normalization: case-fold, trim) | PixelWeaver x-unique-field; tunebox track names |
| pairwise-distinct (same normalization set) | PixelWeaver x-pairwise-distinct |
| ranges-disjoint (half-open; each range well-formed per ordered-pair first) | PixelWeaver x-range-nonoverlap |
| ordered-pair (a < b between siblings) | PixelWeaver x-less-than-sibling |
| intra-document references | orxtra dependencies; incantino flow->screen |

Cross-document forms (evidence supplied by named resolvers; see Domain checks — these are
declared in the schema and executed by the constraint engine, so they carry the same
conformance guarantee as everything above; they are NOT consumer code):

| Form | Example origin |
|---|---|
| named-reference-must-resolve (across documents) | incantino flow -> screen files; orxtra cross-file deps |
| set-coverage (every element of an evidence set appears in a collection) | rlsbl: every commit in range covered by a changelog entry |
| cross-collection-unique | names unique across a document family |
| count-limit (count of documents matching a declared selection ≤/≥ literal N) | aggregate over a selection; literal bounds only |
| sum-limit (sum of a declared numeric field across a selection ≤/≥ literal N) | aggregate over a selection; literal bounds only |

count-limit and sum-limit (decision 23) are AGGREGATE forms with LITERAL bounds ONLY — no
expressions, no computed bounds; the bound is a literal written in the schema. count-limit
compares the count of documents matching a declared selection against N; sum-limit compares the
sum of a declared numeric field across the selection against N. Their formal semantics are
pinned in appendix-semantics.md. Aggregate selection is resolved by the `documents-in(glob)`
resolver; the glob is ANCHORED AT THE MANIFEST ROOT and resolved in LEXICOGRAPHIC order (a
CWD-relative or document-relative anchor would make verdicts depend on invocation directory —
banned by the no-silent-degradation rule). A sum-limit selection containing a document that
lacks the summed field, or whose value is non-numeric, is a HARD ERROR
(`STRICTSPEC_CROSS_SUM_FIELD_MISSING`), never a silent skip-or-zero — mirroring the
ranges-disjoint missing-bound precedent.

### Condition set (closed) for gated forms

The three gated forms — `conditional-required`, `forbidden-when`, and `conditional-value` — share
one CLOSED set of six condition kinds over a sibling gate field:

`{ present, absent, equals-literal, not-equals-literal, in-literal-set, not-in-literal-set }`.

`present`/`absent` test field presence; the other four test the WRITTEN value against literals
(there is no effective/default value — decision 30 deleted defaults, so conditions read only what
the author wrote). `not-equals-literal`/`not-in-literal-set` provide negative-polarity conditions
DIRECTLY, so a schema never enumerates the complement of an enum to express an "unless" rule (a
brittle pattern that silently breaks when the enum grows). NUMERIC COMPARISON predicates
(`> k`, `>= k`, `< k`, `<= k`) are REJECTED (see the vocabulary rejection rationale below):
they were single-consumer demand and are expressible via the literal predicates when the
field's domain permits — e.g. `retry > 0` over a non-negative `retry` (`min = 0`) is exactly
`retry not-equals-literal 0`. The vocabulary-table origin for value-triggered
`conditional-required` is therefore stated as `orxtra retry != 0 => retry_resume` (with the note
that it relies on `retry >= 0`), so the cited origin is honest.

`conditional-value` (NEW): when the gate condition holds, a target field — when present — must
EQUAL a declared literal (evidence: wavescript's `Pin`, e.g. `unison == 1` when
`source ∈ {noise,pink,brown}`; rlsbl's `preid=="stable" ⇒ bump=="prerelease"`). It is decidable,
portable, and analogous to conditional-required (which asserts presence, not value). Semantics in
appendix-semantics.md; code `STRICTSPEC_INTRA_CONDITIONAL_VALUE`.

`collections-disjoint` (NEW): two DECLARED sibling array fields in one record share no element
(element-level set disjointness, with the `case-fold`/`trim` normalization options mirroring
unique-by). This is distinct from `mutual exclusion`, which is FIELD-level ("at most one of a
field set present"). Evidence: rlsbl's `include`/`exclude` rule (`set(include) ∩ set(exclude) =
∅`). The former cross-field table MIS-CITED rlsbl include/exclude as the origin of `mutual
exclusion`; that citation is corrected — `mutual exclusion`'s origin is now the field-level
pgdesign body-XOR-file rule, and rlsbl include/exclude is the origin of `collections-disjoint`.
Semantics in appendix-semantics.md; code `STRICTSPEC_INTRA_COLLECTIONS_DISJOINT`.

`ranges-disjoint` (clarified): each declared range must FIRST be well-formed per ordered-pair
(its start strictly less than its end, i.e. a positive half-open interval) — an ill-formed range
is a violation before disjointness is even evaluated — and disjointness is then decided over the
HALF-OPEN intervals `[start, start+length)`. The form does NOT include an in-bounds-against-a-
sibling-array-length leg (PixelWeaver's `start+len <= len(palette)`): that leg is consumer-native
(the vocabulary carries the portable disjointness/well-formedness, not a cross-collection length
join). "Missing/invalid bounds source" remains a SCHEMA-authoring hard error (the start/length
field names do not resolve on the element record).

### Vocabulary rejection rationale (recorded, revisit on recurrence)

The following shapes surfaced in the examples/ drafts and are REJECTED (each single-consumer or
expressible/consumer-native; recorded so a later recurrence is a vocabulary-evolution
conversation, not an escape hatch):

- NUMERIC COMPARISON condition predicates (`> k`, `>= k`, …) — single-consumer (orxtra's `> 0`),
  expressible via the literal-value predicates over the field's known domain; the closed
  condition set stays literal-equality/membership only.
- `array-contains-literal` gate condition (rlsbl's Flutter gate: `"flutter" ∈ include ⇒
  targets.flutter.mode required`) — 1 consumer; consumer-native.
- REFERENCE-TARGET predicates ("resolve AND the resolved target satisfies P" — rlsbl-config F1
  `wraps → sibling.artifact=="binary"`), CROSS-SCOPE EXISTENTIAL forbidden-when (rlsbl-config F2
  launcher-under-`publish_mode==none`), and OPEN-NAMESPACE reference resolution (rlsbl-config F3
  `depends_on` spanning in-document entries ∪ tool-runtime built-in checks) — revisit at the
  rlsbl adoption wave.
- ENUM-TYPED map keys / `key_type` (betterclaude tier→display) — 1 consumer; a `key_pattern`
  regex expresses it today (and enum sourcing can bake the enum into the regex). Map keys remain
  regex-constrained only.

## Domain checks — portable by construction

Domain checks are the validation that needs the outside world. The architecture has two
strictspec-owned layers — the former third layer, a portable decision language, was REMOVED
2026-07-12 (decision 23); the bespoke tail is consumer-native (below):

1. EVIDENCE RESOLVERS — the only IO surface. A CLOSED, named vocabulary implemented by the
   toolchain and runtimes (e.g. `sibling-document(ref)`, `documents-in(glob)`, `file-exists(p)`,
   `commits-in-range(a,b)`, `resolve-git-object(h)`). Resolvers return DATA (tagged values,
   fact sets), never verdicts. RESOLVER PARITY is conformance-tested: identical evidence
   inputs yield identical resolver outputs across targets — the only thing environments may
   legitimately disagree on is the state of the world itself. A runtime that cannot satisfy a
   required resolver (git in a browser) hard-errors at check-execution time, naming the
   resolver. Extending the vocabulary is a versioned, changelog-covered language change.
2. THE DECLARATIVE LAYER — the cross-document constraint vocabulary above. Most real domain
   checks (references, coverage, uniqueness, aggregate count/sum limits) are these forms and
   stop being "domain checks" in any meaningful sense: implemented once in the shared emitter
   IR (appendix-emitter-ir.md), executed by the CONSTRAINT ENGINE ported to all four targets —
   verdict-, code-, path-, and message-identical everywhere.

THE CONSUMER-NATIVE TAIL: genuinely bespoke checks are ordinary consumer code over the
generated typed values — invoked by the consumer after validation, emitting diagnostics with
consumer-prefixed codes via the runtime's constructor. They are outside strictspec and outside
the conformance guarantee, BY DECLARATION: strictspec has no registration surface, no plugin
API, no embedded expression language — consumer checks are not plugged in, they simply run
downstream. Blobs owned by such a check are declared with the `consumer_check` stance on the
opaque leaf, so the boundary is on record and inventoried by `strictspec check`. Rationale: the
constraint vocabulary covers the portable majority; a bespoke expression DSL hand-ported to
four targets was a perpetual drift surface serving only the tail that is bespoke anyway.
CEL-class open computation remains expressly rejected — there is nothing left to embed it in; a
check shape that recurs across consumers is a vocabulary-evolution conversation, not an escape
hatch.

Execution model: phase 1 (structural) runs first and completely; phase 2 (domain — the
constraint vocabulary over resolver evidence) runs only for records whose phase 1 passed;
diagnostics append to the same ordered result. Honest carve-out: "all errors in one pass" is
per-phase. Vocabulary forms receive the TYPED containing record (partial-subtree binding —
which also serves downstream consumer-native checks). Every diagnostic is an error; warnings
do not exist. `strictspec validate` requires an explicit mode — `--structural-only` or
`--with-domain-checks` — and the CLI hosts domain checks natively (the toolchain has the
constraint engine and the resolvers); no default.

What remains outside strictspec: the consumer-native tail above, and bespoke analysis that is
not document validation at all (pgdesign's normal-form analysis). Such code consumes
strictspec-validated typed values.

## Unknown-key policy

Unknown keys are ALWAYS a canonical hard error — a language invariant, nothing to declare.
There is no per-section stance: `unknown_keys` is not a construct of the language, and the
meta-schema neither requires nor accepts it. Extension zones use the opaque JSON leaf (which
declares `consumer_check` or justified `unchecked`), not a relaxed unknown-key stance. Aliases
remain exempt from unknown-key errors. There is no warning severity anywhere in the language:
the diagnostic model has no severity field, every diagnostic is an error, and no
valid-with-warnings outcome exists — for structural validation or domain checks.

The two sanctioned edges out of a single schema file — named-type imports from type-definition
files (decision 21) and enum arms sourced from a document (decision 32) — are NOT unknown-key
relaxations. Imported types are resolved names, and sourced enum arms are baked at `gen` time;
unknown keys remain a hard error everywhere, including within imported types and against the
sourced-enum source document. A schema remains otherwise self-contained; these are the only two
cross-file/data dependency edges the language sanctions.

## Versioning, migrations, and the version-boundary invariant

Field naming (three formerly-ambiguous uses of "version", now distinct tokens):

- Documents carry integer `format_version` (fixed field name).
- Schemas declare `format_version` — the value their documents must carry. Same token as the
  document field: the pairing is visible in the text.
- Schemas carry `meta_version` — the schema-LANGUAGE version (schemas are documents of the
  meta-schema, gated and migrated like any other document).

Rules:

- The gate accepts exactly the schema's current `format_version`; absent/wrong-type/
  unsupported = hard error using the three-message pattern with a STRUCTURED remediation
  payload: got format_version, expected format_version, schema id, migration-set id, and the
  exact `strictspec migrate` invocation. No inference, no ranges, no legacy modes.
- GATE-TERMINAL (amendment, 2026-07-27): a gate failure TERMINATES that document's
  validation — no structural (phase 1) or domain (phase 2) diagnostics are emitted for a document
  whose gate did not pass. The gate runs first and, on failure, is the SOLE diagnostic for that
  document (per JSONL line for streams). This pins the previously-implicit ordering (the generated
  API contract says the gate "runs first, before structural validation"; this amendment states
  that a failing gate also stops the pass). Rationale: a document at the wrong `format_version` may
  have an entirely different shape; running structural checks against the current schema would emit
  misleading cascade diagnostics. The remediation is to migrate, not to fix individual fields. The
  document-root `format_version` key is itself EXEMPT from the unknown-key invariant (it is the
  language-level gate field, never a schema-declared field unless the schema also models it).
- NORMATIVE BUMP RULE (decision 13): any schema edit that SHRINKS the accepted-document set
  obligates a `format_version` bump — tightening a constraint, removing an enum arm (including
  a sourced-enum arm removed or gone stale), removing an alias, making an optional field
  required, narrowing a union, adding a required field, or any other edit that rejects a
  document the previous schema accepted. Widening edits (accepting strictly more documents) do
  NOT obligate a bump. Enum-sourcing interaction: removal of a sourced arm is a narrowing edit
  and triggers the bump. `strictspec diff`'s SAME-VERSION FLIP-SCAN (below) mechanically
  enforces this — an un-bumped narrowing is a hard error.
- JSONL: per-line `format_version`; mixed-version streams well-defined (per-line gate).
- Migration execution loci — exactly two, both tool-owned: (1) the CLI (`strictspec migrate`,
  including explicit in-memory migration of immutable archives — read old bytes, migrate in
  memory, hand back current-version values, never write; distinct from the banned silent
  auto-migrate-on-read); (2) TOOL-GENERATED BOUNDARY CHECKPOINTS (below). Receiver-side
  migration does not exist; automatic migration does not exist; generated read paths ship only
  the inline gate.
- Migration atomicity: execution is ALL-OR-NOTHING per run — every file transforms and
  revalidates to a temp copy first; only after ALL succeed does the rename sweep run; any
  failure anywhere leaves zero changes on disk.
- Bootstrap contract (pre-versioning consumers): a one-time conversion script stamps
  `format_version` into every existing document (per line for JSONL); it must stamp, never
  reshape, and refuse ambiguous inputs. No CLI command.
- CLOSED op set, 13 ops: add_field, remove_field, rename_field, move_field, set_value,
  set_value_where, remove_where, add_collection, drop_collection, append, merge_defaults,
  wrap_in_array, unwrap_singleton. ADMISSION CRITERION (the line, stated once): ops may move,
  rename, reshape, delete, and inject literal values; NO op may compute a new value from an
  existing value; predicates test field equality and presence/absence only. Dropped from
  migrable (retired into this engine): transform (CEL), raw, merge_defaults_by_key.
  `merge_defaults` (name inherited from migrable; unrelated to any schema construct — the
  language has no defaults, decision 30) injects the migration file's literal key/value pairs
  into each targeted record for keys ABSENT there; present keys are untouched; values are
  literals, never computed. Per-op
  collision semantics normative (add onto existing, rename onto existing, unwrap of
  non-singleton: pinned, hard errors where ambiguous).
- Reversibility taxonomy: each op declares `down` (total), `partial` (per-document; failure is
  a canonical hard error — e.g. unwrap_singleton on a 2-element list), or `irreversible`. The
  down migration is AUTHOR-SUPPLIED: the migration file carries explicit `[[down_ops]]` (the
  inverse op sequence) alongside the declared taxonomy — the engine NEVER derives down ops from
  the forward ops (the up and down directions are independently authored, per the surface in
  appendix-surface-syntax.md §9). The declared taxonomy is VERIFIED EMPIRICALLY by `strictspec
  diff` (down-taxonomy verification: the DECLARATION is checked against the corpus; a mis-declared
  taxonomy is a hard error, not documentation). Static verification arrives with the unbundled
  future analyzer (decision 25).
- Flagship examples: claudestream budget = rename_field + wrap_in_array (down: partial);
  rlsbl dev_node chain = two rename_field migrations (down: total).

THE VERSION-BOUNDARY INVARIANT (normative): a document only ever exists at the current
format_version within any boundary it inhabits, and every boundary crossing is a
tool-generated migrate-to-current checkpoint. Three legs:

1. PRODUCERS: the write path refuses to serialize any non-current format_version (canonical
   serialization appendix). No conforming producer creates staleness.
2. STORES: a consumer manifest may declare stores (databases, object stores, caches — any
   at-rest home that is not a git working tree). The generator emits the store's INGEST
   WRITE-DOOR: every document entering the store is migrated-to-current and revalidated
   atomically at the write door, or rejected. Invariant: nothing stale exists at rest.
   Readers behind an ingesting store never meet an old document — their gate is a tautology.
3. LIVE CHANNELS: a normative self-describing envelope (format_version + schema id + strictspec
   release) and a version-negotiation handshake: producer and consumer agree on a single
   version before documents flow, or the channel refuses to open — explicit agreement, hard
   failure, no fallback. When negotiated versions differ from the producer's storage version,
   the EGRESS side migrates before sending. Receivers only gate. Symmetry: old readers reject
   NEW documents exactly as cleanly as new readers reject old ones (the single-version gate
   gives this for free); a receiver that cannot speak the negotiated version refuses with a
   structured "update the client" diagnostic. Browser runtimes never migrate — they receive
   current bytes or refuse.

Consequences: the stale-document class is impossible by construction — no path exists by
which a reader meets a non-current document that did not already hard-error at a checkpoint.
Consumers that declare no stores and no channels get no checkpoint code and pay nothing.
Checkpoints are explicit, deterministic, single-target (current), and hard-failing — this is
NOT auto-migrate-on-read, which remains banned. Fleet coordination: rlsbl's format_version
deploy gate (fed by `strictspec diff` certificates) refuses to roll out a producer emitting a
version its declared consumers cannot accept, and sequences at-rest migration jobs as ordered
release steps.

## Accepted-set semantics, diff, and doc-diff

FORMAL SEMANTICS APPENDIX (normative, versioned, WRITTEN NOW — appendix-semantics.md): every
schema denotes a regular tree language plus a constraint envelope; every migration denotes a
restricted tree transducer (restricted because ops never compute values from values — every
value at N+1 is a verbatim carry-over, an injected literal, or absent). The three-valued
verdict algebra (holds/violated/undecidable), the per-construct semantics entries (every
construct in the language — including count-limit and sum-limit — has a semantics entry), and
the CLOSED UNDECIDABILITY CATALOG (the enumerated fragments where a decision procedure may
legitimately return unknown — every `undecidable` names its class; an open-ended "unknown" does
not exist; adding a class is a breaking-class, changelog-covered appendix change) are pinned
NOW in appendix-semantics.md. Every construct added to the language MUST extend that appendix —
a construct without semantics cannot ship. DEFERRED (decision 25, amending the former
written-now clause): the PROOF-OBJECT FORMAT and the MODEL-SEARCH ORDER for witness synthesis
are NOT written now — they move to the future analyzer project. UNBUNDLING (2026-07-12,
decision 25): nothing in the strictspec toolchain implements these decision procedures; the
analyzer that does (automaton inclusion, constraint solving, witness synthesis, proof objects
re-verified by a small independent checker) is a SEPARATE FUTURE PROJECT. The per-construct
semantics and undecidability catalogue are written now so that (a) construct additions stay
semantics-honest and (b) the analyzer can later be built against a spec that never drifted.

`strictspec diff` — THE EMPIRICAL ENGINE (what ships). Inputs: a schema at two format versions,
the migration M between them, and a REQUIRED `--corpus <glob>` of real documents. It runs:

- FLIP-SCAN: every corpus document validated at N and at N+1; every flip is reported with the
  document and its killing diagnostics (code + path). A flipped document is a REAL WITNESS —
  no synthesis needed.
- SAME-VERSION FLIP-SCAN (decision 25): the corpus replayed against the OLD and NEW schema at
  the SAME (unchanged) format_version. Any document flipping valid->invalid is an UN-BUMPED
  NARROWING — a HARD ERROR (pairs with the versioning bump rule). This catches a narrowing edit
  that forgot to bump format_version, including a stale/removed sourced enum arm.
- MIGRATE-ROUND-TRIP: soundness (M(d) revalidates at N+1 for every corpus d valid at N — a
  failure is a counterexample document) and completeness (M never errors on a valid-at-N
  corpus document). This enables red-green migration authoring against the corpus: write M,
  get a counterexample, fix, re-run.
- DOWN-TAXONOMY VERIFICATION: the declared down/partial/irreversible taxonomy exercised
  against the corpus (down applied where declared; partial failures must match the
  declaration); a mis-declared taxonomy is a hard error, not documentation.

Output: a CERTIFICATE in a spec-pinned JSON format (pinned in appendix-certificates.md). Every
claim carries an EVIDENCE GRADE: `violated` (a corpus document is the counterexample — a proof)
or `corpus-supported` (no counterexample in the declared corpus — explicitly NOT a proof; the
certificate records the corpus identity and size so the claim's weight is legible). The
`proven` grade — a machine-checkable proof object re-verified by the independent checker — is
RESERVED for the future analyzer, which emits the SAME certificate format; the gate consumes
certificates without caring which engine produced them.

GATE INTEGRATION (normative): the certificate is the required input to rlsbl's format_version
deploy gate. `violated` BLOCKS release. The green light is `corpus-supported` over the
consumer's DECLARED AT-REST CORPUS (the documents the migration will actually meet). A
consumer with no corpus discharges via a committed ADJUDICATION FILE — itself a
strictspec-schema'd TOML document naming each unsupported claim and attaching a signed manual
justification. There is no bypass.

`strictspec doc-diff` (inputs: one schema + two documents of it): a structured per-path
semantic delta — added/removed/changed with typed values, array element moves keyed by declared
unique-by. Pinned output shape (in appendix-certificates.md). For agents reviewing spec changes
as structure, not text.

Both commands are toolchain-only analyses; conformance is golden-output determinism, not
multi-target execution.

## Error model

- Diagnostic: stable code, path, message, expected/got, optional suggestion, optional source
  position (raw-text inputs have positions; tagged-value inputs do not). NO severity field —
  every diagnostic is an error.
- Codes are prefixed strings; `STRICTSPEC_*` reserved. STABILITY POLICY: codes are permanent —
  never renamed, never reused. Message text is SPEC-PINNED (below) and changes only as a
  versioned appendix change; consumers may assert codes AND rely on message identity across
  targets. The complete code catalogue lives in appendix-error-codes.md.
- MESSAGE TEMPLATES (normative, appendix-error-codes.md): every `STRICTSPEC_*` code has a
  spec-pinned template — fixed prose plus typed slots (path, expected, got, suggestion), slot
  values rendered per the canonical value-rendering table (appendix-rendering.md) and
  suggestions computed per the pinned did-you-mean algorithm (appendix-rendering.md). Generators
  emit each runtime's renderer FROM the templates via the shared emitter IR
  (appendix-emitter-ir.md); nothing is hand-translated per target. Consumer-prefixed codes (from
  consumer-native checks) have consumer-owned templates, outside these appendices and outside
  the conformance surface.
- CONFORMANCE GUARANTEE: ordered verdict + code + path + message-text identity across all four
  targets — the suite asserts exactly what consumers may rely on, and agents (the primary
  consumers) read messages, so messages are guaranteed.
- APPENDIX STABILITY POLICY: the appendices (path grammar, traversal order, datetime lexeme
  rules, edge-input outcomes, canonical serialization, message templates + code catalogue in
  appendix-error-codes.md, value rendering + path grammar + did-you-mean in appendix-rendering.md,
  the certificate + doc-diff shapes in appendix-certificates.md, the per-construct formal
  semantics and undecidability catalogue in appendix-semantics.md, custom scalar registration in
  appendix-custom-scalars.md, the concrete TOML surface syntax in appendix-surface-syntax.md, and
  the shared emitter IR in appendix-emitter-ir.md) are VERSIONED.
  ANY change is a breaking-class, changelog-covered entry in the strictspec release that ships
  it and triggers full conformance-fixture regeneration. Appendix-driven behavior changes are
  always declared, never silent. The proof-object format and model-search order are NOT current
  appendices — they are deferred to the future analyzer (decision 25).
- Paths: index-then-key switching, rendered per appendix-rendering.md.
- All-errors-in-one-pass (per phase); no fail-first mode.
- Renderers: terminal (emission order) and JSON.

## Export: JSON Schema and structured metadata

- JSON Schema export with a NORMATIVE LOSSINESS TABLE: every construct maps to its exported
  form; dropped semantics are named (alias both-present rule, all cross-field and
  cross-document forms, consumer-check declarations, per-line versioning). The export may use
  keywords excluded from strictspec's own language. Editors are advisory; strictspec is the
  enforcement.
- STRUCTURED SCHEMA METADATA export (JSON): the complete schema — constructs, constraints,
  descriptions, versions, migration history, unchecked inventory — as data. selfdoc owns
  rendering docs pages from this metadata; strictspec generates no docs pages. One docs system —
  which also renders THIS constitution (and its appendices) as strictspec's published language
  reference (decision 27): the canonical, citable manual, versioned with the spec.

## Meta-schema, self-hosting, and bootstrap

The language is defined as a strictspec schema, versioned and migrated by its own machinery:
each schema-language version bump ships declarative META-MIGRATIONS INSIDE the toolchain, and
`strictspec migrate` upgrades consumer schema files exactly like any other document — schema
files are documents of the meta-schema. Meta-schema rules: `default` is not a construct — its
presence fails the meta-schema with a dedicated diagnostic (decision 30); alias may
not target a discriminator; tuple/length-bounds exclusive; every regex must compile under RE2;
every opaque JSON object leaf declares `consumer_check` or `unchecked = true` with a mandatory
`unchecked_reason`; `safe_integers` is a recognized schema-wide declaration, and declaring a
TS target without it is a hard error at generation time; a reachable nullable union is
rejected when the target document syntax is known to be TOML; datetime range constraints must
compare same-kind scalars; cross-document constraint forms must reference only declared
resolvers; named-type imports resolve only to type-definition files (types only — no cross-file
constraints, no transitive imports); a sourced enum's baked arms must be fresh against their
source document (staleness is a `gen`/`check` hard error); every schema/type-definition file
declares `role` (`"schema"` or `"type-definitions"`) — the distinction is a declared literal,
never inferred from shape — and a `role = "schema"` file names its `root` type. All of this is
spelled in the single pinned surface (appendix-surface-syntax.md).
The consumer manifest (strictspec.toml) is a document of a toolchain-shipped built-in schema —
same gating, same migrations, meta-fixtures in conformance; it additionally declares stores
and channels for boundary-checkpoint generation.

Bootstrap order (entered by hand exactly once): document model -> hand-written meta-schema
check -> interpreter (full language) -> generator -> thereafter the meta-schema is validated by
the interpreter and the interpreter is pinned as the fourth conformance target.

Notation core adapted from incantino: header (name, meta_version, format_version,
description) + per-field type/required/values/description (the donated notation's `default`
is dropped — defaults are not in the language, decision 30). The COMPLETE concrete TOML surface
— the single pinned spelling of schema files, type-definition files, the manifest's custom-scalar
and vocabulary-source declarations, and migration files — is normative in
appendix-surface-syntax.md. The toolchain-shipped built-in meta-schema (the schema of schemas) is
authored in exactly that surface and MUST equal it; there is no second spelling of any construct.
</content>
</invoke>
