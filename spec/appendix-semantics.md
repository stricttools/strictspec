---
description: "Formal per-construct semantics of strictspec: the denotational frame, verdict algebra, and closed undecidability catalogue that every construct added to the language must extend."
---
# Appendix: Formal Semantics and the Undecidability Catalogue (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). VERSIONED and WRITTEN
> NOW. Every construct added to the language MUST extend this appendix — a construct without
> semantics cannot ship (decision 25). Any change is a breaking-class, changelog-covered release
> event.
>
> META NOTE: strictspec is in its GROWTH PHASE — this appendix is STABLE BUT GROWING; semantics
> CORRECTIONS and per-construct additions are normal and expected, recorded per-release through the
> existing discipline (decision 3), with released-surface compatibility governed by semver at
> release boundaries.
>
> DEFERRAL (decision 25, amended 2026-07-27): the DENOTATIONAL FRAME, the PER-CONSTRUCT
> semantics, the VERDICT ALGEBRA, and the CLOSED UNDECIDABILITY CATALOGUE are written now and
> are normative. The PROOF-OBJECT FORMAT and the MODEL-SEARCH ORDER for witness synthesis are
> DEFERRED to the separate future analyzer project. Nothing in the strictspec toolchain
> implements the decision procedures below; the appendix exists so that (a) construct additions
> stay semantics-honest and (b) the analyzer can later be built against a spec that never
> drifted.

## 1. The denotational frame

- Every SCHEMA denotes a REGULAR TREE LANGUAGE plus a CONSTRAINT ENVELOPE. The regular tree
  language is the set of tagged document trees the structural (phase-1) layer accepts; the
  constraint envelope is the conjunction of constraint-vocabulary predicates (phase 2) that
  further restrict that set. The schema's ACCEPTED SET is the intersection: trees in the tree
  language that also satisfy the envelope.
- Documents are finite tagged trees (per `DESIGN.md` document model): scalars carry a lexeme
  class and lexeme; records, arrays, maps, and tuples are the interior node kinds.
- Every MIGRATION denotes a RESTRICTED TREE TRANSDUCER: a function from trees to trees in which
  every output value is a verbatim carry-over of an input value, an injected LITERAL, or absent.
  Restricted because ops never compute a value from a value (the admission criterion). Predicates
  within a migration test field equality and presence/absence only.
- The ACCEPTED-SET EFFECT of each construct/constraint (below) is stated as how it grows or
  shrinks the accepted set. This grounds decision 13's bump rule: an edit that SHRINKS the
  accepted set obligates a `format_version` bump; a widening edit does not.

## 2. Verdict algebra (three-valued)

- Verdicts are `holds`, `violated`, `undecidable`. Structural and constraint checks executed by
  the runtimes only ever return `holds` or `violated` — they are decidable on a concrete
  document. `undecidable` arises ONLY in the analyzer's reasoning ABOUT schemas/migrations
  (e.g. "does every accepted document at N remain accepted at N+1?"), never in validating a
  concrete document.
- Composition (conjunction of a record's field checks and its constraint envelope):
  - `holds ∧ holds = holds`
  - `holds ∧ violated = violated`; `violated ∧ anything = violated`
  - `holds ∧ undecidable = undecidable`
  - `undecidable ∧ undecidable = undecidable`
  - `violated ∧ undecidable = violated` (a concrete violation dominates: a witness is a proof
    regardless of any undecidable sibling claim).
- Disjunction (union arms) mirrors this with `holds` dominating: `holds ∨ anything = holds`;
  `violated ∨ violated = violated`; `violated ∨ undecidable = undecidable`.
- The empirical engine maps its evidence grades onto this algebra: a `violated` claim is a
  concrete `violated` (a corpus witness); a `corpus-supported` claim is `holds` OVER THE CORPUS
  ONLY — it is NOT a general `holds`, and the certificate records the corpus so the weaker
  quantifier is legible (see `appendix-certificates.md`). A generally-`holds` verdict (the
  `proven` grade) is the deferred analyzer's job.

## 3. Per-construct semantics

Each entry: INFORMAL statement · ACCEPTED-SET effect · INTERACTION notes. Codes referenced are
in `appendix-error-codes.md`.

### 3.1 Closed record
- Informal: a fixed set of named fields, each required or optional; unknown keys are rejected.
- Accepted-set: the product of its fields' accepted sets, restricted to trees whose key set is a
  subset of the declared keys (unknown key ⇒ excluded) and a superset of the required keys.
- Interaction: unknown-key rejection interacts with ALIASES (alias spellings are exempt) and
  with the OPAQUE LEAF (extension data lives in an opaque leaf, never a relaxed record). Adding a
  required field or removing an optional field SHRINKS the set (bump); adding an optional field
  WIDENS it (no bump).

### 3.2 Typed map (regex keys)
- Informal: an unbounded set of key→value entries; keys must match a declared RE2 regex; values
  share one declared type.
- Accepted-set: trees mapping each key (matching the regex) to a value in the value type's
  accepted set. Distinct construct from a record (records are closed; maps are open over the key
  regex).
- Interaction: key regex is vetted at schema-validation time (`STRICTSPEC_SCHEMA_REGEX_UNSUPPORTED`);
  keys compared by code points (no normalization). Ordering: a map section declares whether order
  is semantic; constraints never read order.

### 3.3 Named type
- Informal: a reusable type definition referenced by name; expands to its definition at each use.
- Accepted-set: identical to the accepted set of its expansion at each reference site.
- Interaction: a named type may be recursive (3.4) and may be IMPORTED (3.20). A dangling name is
  `STRICTSPEC_SCHEMA_UNKNOWN_TYPE_REF`.

### 3.4 Recursive reference (pinned depth limit)
- Informal: a named type may reference itself, directly or mutually; documents are finite trees;
  validation depth is capped.
- Accepted-set: the least fixed point of the type's defining equation, INTERSECTED with the set
  of trees whose nesting does not exceed the pinned maximum validation depth.
- Interaction: exceeding the depth is `STRICTSPEC_DEPTH_EXCEEDED`, a canonical diagnostic fired
  before CPython stack exhaustion. The depth cap makes the accepted set decidable and the
  automaton finite-state.

### 3.5 Discriminated union
- Informal: arms selected by a literal-valued discriminator field.
- Accepted-set: the disjoint union of arm accepted sets, keyed by discriminator value; a tree is
  accepted iff its discriminator names an arm AND the tree is in that arm's set.
- Interaction: non-matching arms are NEVER validated (union diagnostics). A discriminator may not
  be aliased (`STRICTSPEC_SCHEMA_ALIAS_ON_DISCRIMINATOR`). Adding an arm WIDENS; removing an arm
  or narrowing an arm SHRINKS.

### 3.6 Node-kind union
- Informal: undiscriminated union whose arms differ by node kind (scalar vs record vs array);
  the input node kind selects the arm.
- Accepted-set: the disjoint union of arm sets, keyed by node kind. Well-defined only because
  arms have distinct node kinds (meta-schema rejects same-kind arms,
  `STRICTSPEC_SCHEMA_NODE_KIND_UNION_AMBIGUOUS`).
- Interaction: covers predraw fill-or-gradient. A node kind matching no arm is
  `STRICTSPEC_UNION_NODE_KIND`.

### 3.7 Nullable union (T | null)
- Informal: T or the null value; T may itself be a union.
- Accepted-set: the accepted set of T, plus the singleton {null}.
- Interaction: UNUSABLE with TOML documents — a reachable nullable union against a TOML document
  is a canonical hard error, and rejected at meta-schema time when the syntax is known TOML
  (`STRICTSPEC_SCHEMA_TOML_NULLABLE`). Fully available for JSON/JSONL; null short-circuits arm
  selection.

### 3.8 String scalar
- Informal: a string value.
- Accepted-set: all strings; further restricted by length/regex/non-empty value constraints
  (3.13–3.16) when attached.
- Interaction: length is code points (item 2); no normalization.

### 3.9 Integer scalar
- Informal: an integer lexeme (no `.`, `e`, `E`); parsed as int64.
- Accepted-set: int64 values; float-classed lexemes are excluded (`STRICTSPEC_TYPE_NOT_INTEGER`).
- Interaction: `safe_integers` (3.17) further restricts to |n| < 2^53. Overflow lexeme is
  `STRICTSPEC_NUM_INT_OVERFLOW`. Booleans are not integers.

### 3.10 Float scalar
- Informal: a float-classed lexeme (contains `.`, `e`, or `E`); binds float64.
- Accepted-set: float64 values representable by the lexeme; beyond-range lexeme excluded
  (`STRICTSPEC_NUM_FLOAT_OVERFLOW`). Integer-classed lexemes excluded (`STRICTSPEC_TYPE_NOT_FLOAT`).
- Interaction: lexeme class is lexical and preserved (`3` ≠ `3.0`).

### 3.11 Number scalar
- Informal: accepts BOTH lexeme classes; binds float64; rendering preserves the source lexeme
  class.
- Accepted-set: values whose exact lexeme float64 can represent; any lexeme float64 cannot
  represent exactly (large integer lexeme, precision-exceeding decimal) is EXCLUDED
  (`STRICTSPEC_NUM_UNREPRESENTABLE`) — no silent precision loss.
- Interaction: the only scalar that spans both classes; write-side and message rendering both
  preserve the source class (`appendix-rendering.md`).

### 3.12 Boolean, date, time, datetime scalars
- Informal: boolean is `true`/`false`; date/time/datetime are RFC 3339 kinds (offset or local
  declared per field for datetime).
- Accepted-set: booleans exclude integers; datetime kinds exclude the wrong kind
  (`STRICTSPEC_TYPE_DATETIME_KIND`) and non-conforming RFC 3339 strings
  (`STRICTSPEC_TYPE_NOT_DATETIME`).
- Interaction: lexemes retained, no normalization on read (`+00:00` not rewritten to `Z`); range
  comparisons use the instant (offset) or naive value (local); cross-kind comparison is a
  meta-schema error (`STRICTSPEC_SCHEMA_DATETIME_KIND_MISMATCH`).

### 3.13 Enums
- Informal: a value must be one of a fixed set of literals.
- Accepted-set: the finite set of enum members.
- Interaction: non-membership is `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` with did-you-mean. Arms may be
  document-sourced (3.21). Adding a member WIDENS; removing SHRINKS.

### 3.14 Literal constant
- Informal: the value must equal a single pinned literal.
- Accepted-set: the singleton {literal}.
- Interaction: used as discriminator values; mismatch is `STRICTSPEC_TYPE_NOT_LITERAL`.

### 3.15 Value constraints — numeric range
- Informal: inclusive/exclusive lower and/or upper numeric bounds.
- Accepted-set: intersects the scalar's set with the closed/open interval.
- Interaction: attaches to integer/float/number. Tightening a bound SHRINKS (bump); loosening
  WIDENS. Codes `STRICTSPEC_VALUE_NUM_*`.

### 3.16 Value constraints — datetime range, string length, regex, array length, non-empty
- Informal: datetime same-kind range; string length in code points; RE2 regex on string values
  and map keys; array length bounds; non-empty string.
- Accepted-set: each intersects the underlying set with its predicate. Regex restricts to the
  matched language; length/non-empty restrict cardinality.
- Interaction: datetime range must be same-kind (meta-schema). Array-length bounds are mutually
  exclusive with tuple form (`STRICTSPEC_SCHEMA_TUPLE_ARRAY_BOUNDS`). Codes `STRICTSPEC_VALUE_*`.

### 3.17 safe_integers mode
- Informal: schema-wide `safe_integers = true`; every backend rejects |n| ≥ 2^53.
- Accepted-set: intersects every integer/number position with (−2^53, 2^53).
- Interaction: MANDATORY when a TS target is declared (`STRICTSPEC_SCHEMA_TS_WITHOUT_SAFE_INTEGERS`);
  TS binds plain `number` either way; verdict identity across backends preserved. Enabling it on
  an existing schema SHRINKS the set (bump).

### 3.18 Tuples
- Informal: a fixed-arity ordered sequence with per-position types.
- Accepted-set: the product of per-position accepted sets at the exact declared arity;
  wrong-arity excluded (`STRICTSPEC_TYPE_TUPLE_ARITY`).
- Interaction: mutually exclusive with array-length bounds (3.16). Binds to fixed-size forms per
  target.

### 3.19 Aliases
- Informal: permanently co-valid alternative spellings within one schema version; one canonical
  spelling on write.
- Accepted-set: DOES NOT change the accepted set of VALUES — it widens the accepted KEY SET so a
  field may be spelled by its canonical name or any alias (but not both).
- Interaction: exempt from unknown-key rejection; both-present is `STRICTSPEC_ALIAS_BOTH_PRESENT`;
  may not target a discriminator; canonicalization preserves comments; cross-version renames are
  migrations, not aliases.

### 3.20 Shared named-type imports (decision 21)
- Informal: a schema may import named TYPES from a dedicated type-definition file — types only,
  no cross-file constraints, no transitive imports.
- Accepted-set: identical to inlining the imported type definitions; imports are a modularity
  device with zero semantic effect beyond name resolution.
- Interaction: a cross-file CONSTRAINT is rejected (`STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT`); a
  transitive import is rejected (`STRICTSPEC_IMPORT_TRANSITIVE`); a missing file or unknown type
  is `STRICTSPEC_IMPORT_MISSING_TYPE_FILE` / `STRICTSPEC_IMPORT_UNKNOWN_TYPE`. Imports interact
  with `meta_version`: a type-definition file is itself a document of the meta-schema, gated by
  `meta_version` like any schema — importing across incompatible `meta_version` values is a
  metagate error on the imported file.

### 3.21 Enum arms sourced from a document (decision 32)
- Informal: an enum's arms are SOURCED from a named document and BAKED into the schema, with
  toolchain-enforced freshness.
- Accepted-set: at any moment the enum's set is exactly its BAKED arms; the accepted set is a
  function of the schema text, not of the live source (validation never reads the source).
- Interaction with the BUMP RULE: because arms are baked, changing the source document does NOT
  silently change any document's verdict — it changes the schema only when `gen` re-bakes.
  Re-baking that REMOVES an arm SHRINKS the set and obligates a bump; adding an arm WIDENS. A
  baked set that differs from the source is STALE and a hard error in `gen` and `check`
  (`STRICTSPEC_ENUMSRC_STALE`) — the sanctioned data→schema dependency edge, gated in the open.
  The arm SELECTOR is a restricted projection path (key steps + `[]` array-flatten only;
  appendix-surface-syntax.md §7), and that grammar is the accept/reject boundary of
  `STRICTSPEC_ENUMSRC_BAD_SELECTOR`.

### 3.22 Custom scalar registration
- Informal: a named scalar refining a base lexeme class via an anchored RE2 lexeme rule, with
  per-target bindings and rendering (see `appendix-custom-scalars.md`).
- Accepted-set: intersects the base lexeme class's set with the anchored-regex language (plus any
  declared length/non-empty refinement for effectively-opaque scalars).
- Interaction: registration lives in the manifest and travels with the schema; an unregistered
  reference is `STRICTSPEC_SCALAR_UNKNOWN`; a missing per-target binding is
  `STRICTSPEC_SCALAR_NO_BINDING`. Regex refinement is honest about its limit — a scalar like
  `sql-expression` refines only length/non-empty, not grammaticality.

### 3.23 Per-field descriptions
- Informal: human-readable prose attached to a field.
- Accepted-set: NO effect — descriptions feed exported metadata and scaffold comments only.
- Interaction: purely documentary; part of the structured-metadata export.

### 3.24 Intra-document constraint forms
Each is a phase-2 predicate over the typed containing record; each INTERSECTS the accepted set
with the trees it accepts. Decidable from document + schema alone.

The three GATED forms (conditional-required, forbidden-when, conditional-value) share one CLOSED
condition set of six kinds over a sibling gate field: `{present, absent, equals-literal,
not-equals-literal, in-literal-set, not-in-literal-set}`. Value-testing kinds read the WRITTEN
value only (no defaults — decision 30); the negative kinds give negative-polarity conditions
directly (no complement enumeration). Numeric comparison predicates (`> k`, `>= k`, …) are NOT in
the set (rejected — see `DESIGN.md` vocabulary rejection rationale).

| Form | Informal statement | Accepted-set effect | Interaction |
|---|---|---|---|
| conditional-required | field required when the gate condition holds | excludes trees satisfying the condition but missing the field | condition is one of the closed six kinds |
| conditional-value (NEW) | target field, when present, must equal a LITERAL when the gate condition holds | excludes trees where the condition holds and the present target ≠ the literal | asserts VALUE (contrast conditional-required, which asserts PRESENCE); literal only; code `STRICTSPEC_INTRA_CONDITIONAL_VALUE` |
| exactly-one-of | exactly one of a field set present | excludes trees with zero or ≥2 present | scopes at any nesting |
| at-least-one-of | at least one of a field set present | excludes trees with none present | — |
| co-presence (A iff B) | fields present together or absent together | excludes trees with exactly one present | — |
| mutual exclusion | at most one of a field set present (FIELD-level) | excludes trees with ≥2 present | field-level; contrast collections-disjoint (element-level) |
| collections-disjoint (NEW) | two declared sibling arrays share no element (ELEMENT-level) | excludes trees where an element appears in both arrays | normalization case-fold/trim (item 10); origin rlsbl include/exclude; code `STRICTSPEC_INTRA_COLLECTIONS_DISJOINT` |
| forbidden-when | field forbidden when the gate condition holds | excludes trees satisfying the condition with the field present | mirrors conditional-required; closed six-kind condition set |
| unique-by | a keyed value is unique across a collection; normalization case-fold/trim | excludes trees with a duplicate key | normalization is a CLOSED set: none/case-fold/trim (item 10) |
| pairwise-distinct | all values in a set are distinct; same normalization set | excludes trees with any repeat | shares normalization set with unique-by |
| ranges-disjoint | each range is FIRST well-formed per ordered-pair (start < end), THEN half-open ranges do not overlap | excludes trees with an ill-formed range OR intersecting ranges | no in-bounds-against-sibling-length leg (that is consumer-native); a non-resolving start/length field source is a schema hard error |
| ordered-pair | sibling a < sibling b | excludes trees where the order fails | comparison uses the scalars' natural order |
| intra-document references | a reference resolves within the same document | excludes trees with a dangling in-document reference | distinct from cross-document references (3.25); carries NO predicate on the resolved target (reference-target predicates are rejected — consumer-native) |

### 3.25 Cross-document constraint forms
Evidence supplied by named resolvers (the closed IO vocabulary); executed by the constraint
engine; SAME conformance guarantee as structural checks. Each intersects the accepted set with
trees satisfying the predicate GIVEN THE RESOLVER EVIDENCE — so the accepted set is relative to
the evidence (a fact recorded in the undecidability catalogue, section 4).

| Form | Informal statement | Accepted-set effect | Interaction |
|---|---|---|---|
| named-reference-must-resolve | a reference resolves in an evidence set | excludes trees whose reference is absent from the resolved set | resolver unavailable ⇒ hard error, never skip |
| set-coverage | every element of an evidence set appears in a collection | excludes trees missing coverage of any evidence element | e.g. every commit covered by a changelog entry |
| cross-collection-unique | a value is unique across a document family | excludes trees whose value collides in the family | evidence is the sibling collections |
| count-limit (NEW) | a collection's element count across evidence ≤/≥ a LITERAL bound | excludes trees exceeding the literal count | bound is LITERAL only — never computed (decision 23); `documents-in` selection is manifest-root-anchored, lexicographic order |
| sum-limit (NEW) | a summed field across evidence ≤/≥ a LITERAL bound | excludes trees whose sum exceeds the literal | bound is LITERAL only; a selected document lacking the summed field or with a non-numeric value is a HARD ERROR (`STRICTSPEC_CROSS_SUM_FIELD_MISSING`), never skip-or-zero; selection is manifest-root-anchored, lexicographic |

## 4. The CLOSED undecidability catalogue

Every `undecidable` verdict the future analyzer may return names EXACTLY ONE class below.
An open-ended "unknown" does not exist. Adding a class is a BREAKING-CLASS, changelog-covered
appendix change. (The empirical engine that ships never returns `undecidable`; this catalogue
bounds the deferred analyzer.)

| Class | Statement | Why undecidable |
|---|---|---|
| `regex-pair-relationship` | claims about the relationship (inclusion, disjointness, equivalence) between two RE2 regexes at corresponding positions across schema revisions | regex language relationships are decidable for regular languages in principle but the analyzer MAY return this class rather than run the full automaton procedure; and anchored-scalar refinements over external lexeme grammars (custom scalars) are genuinely outside the regular fragment |
| `cross-document-resolver-dependent` | claims about cross-document constraints whose truth depends on the resolver's DOMAIN (the state of the world) which lies OUTSIDE the declared corpus | the accepted set is relative to evidence not fixed by the schema; no static reasoning can quantify over all possible resolver outputs |
| `aggregate-over-unresolved-selection` | count-limit / sum-limit claims where the selection (the collection the aggregate ranges over) is resolved by evidence not present in the corpus | the aggregate's inputs are not statically knowable |
| `recursion-depth-parametric` | claims quantified over documents of unbounded nesting within a recursive type (independent of the pinned depth cap) | reasoning about the fixed point across all depths, rather than the depth-capped finite automaton, escapes the finite-state fragment |

Notes:
- A claim that touches multiple classes names the FIRST applicable class in the table order
  above (deterministic classification).
- The depth cap (3.4) makes CONCRETE-DOCUMENT validation always decidable; the
  `recursion-depth-parametric` class is strictly about the analyzer reasoning ABOUT the schema
  over all depths.

## 5. Deferral note

Per decision 25 (amended 2026-07-27): the PROOF-OBJECT FORMAT (the machine-checkable object the
`proven` grade carries, re-verified by a small independent checker) and the MODEL-SEARCH ORDER
for witness synthesis live with the SEPARATE FUTURE ANALYZER PROJECT. They are NOT specified in
this appendix. The analyzer emits the same certificate format as the empirical engine
(`appendix-certificates.md`); the `proven` grade is reserved for it. Everything else in this
appendix is normative now so the analyzer can be built against a spec that never drifted.

## Cross-references

- Codes for every excluded-tree condition: `appendix-error-codes.md`.
- Value/path rendering of the values reasoned about here: `appendix-rendering.md`.
- Evidence grades and the certificate the algebra maps onto: `appendix-certificates.md`.
- Custom-scalar refinement semantics: `appendix-custom-scalars.md`.
- The concrete surface that spells these constructs (fields, constraints, conditions, selectors):
  `appendix-surface-syntax.md`.
- The constitution: `DESIGN.md`.
