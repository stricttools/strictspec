---
description: "The normative catalogue of strictspec STRICTSPEC_* error codes with their pinned message templates, slot sets, and the closed area set that drives conformance and code generation."
---
# Appendix: Error-Code Catalogue and Message Templates (normative)

> NORMATIVE STATUS: This appendix is part of the strictspec constitution (see `DESIGN.md`).
> It is VERSIONED. Any change to a code, template, or slot set is a breaking-class,
> changelog-covered release event that triggers full conformance-fixture regeneration
> (see `DESIGN.md` — Error model, Appendix stability policy).
>
> META NOTE: strictspec is in its GROWTH PHASE — this catalogue is STABLE BUT GROWING. Catalogue
> GROWTH — new codes for newly discovered hard-error conditions, wording refinement, slot-set
> corrections — is EXPECTED and recorded here through the existing discipline, not exceptional.
> The catalogue only grows; existing (released) codes are permanent, and released-surface
> compatibility is governed by semver at release boundaries.

## 1. Scope

This appendix is the single source of every hard-error condition strictspec can emit, and the
single source of every diagnostic message. strictspec has no warnings and no severity field:
every entry below is an error (`DESIGN.md` — Unknown-key policy, Error model). Consumer-native
checks emit consumer-prefixed codes with consumer-owned templates; those are outside this
appendix by declaration and are never `STRICTSPEC_*`.

Each row is a diagnostic. Generators compile these templates into each target's renderer table
at `gen` time (see `appendix-emitter-ir.md`); no message string is ever hand-translated per
target. Slot values render per `appendix-rendering.md`; `{suggestion}` is computed per the
did-you-mean pin in that appendix.

## 2. Code grammar and stability rules

- Every code is `STRICTSPEC_<AREA>_<NAME>` — uppercase, underscore-separated, ASCII.
- `<AREA>` is drawn from the closed area set in section 3. `<NAME>` is a stable, descriptive
  slug.
- STABILITY (normative, from `DESIGN.md` decision 16 / Error model):
  1. Codes are PERMANENT. A code is never renamed and never reused for a different condition.
     Consumers may assert on codes indefinitely.
  2. Message WORDING is spec-pinned. A wording change is a versioned appendix change
     (breaking-class, changelog-covered) and forces full conformance-fixture regeneration.
     The code does not change when wording changes.
  3. Slot NAMES and TYPES are part of the pinned template. Adding, removing, or retyping a slot
     is a wording-class change under rule 2.
  4. Catalogue GROWTH (new codes) is expected in the growth phase and recorded per-release
     through the existing discipline; new codes accompany new language constructs or newly
     surfaced hard-error conditions and are additive only. Released codes are permanent.
- Slot notation in templates: `{name}` interpolates a value of the declared slot type,
  rendered per `appendix-rendering.md`. Braces are literal delimiters; a template never
  contains an unlisted slot. Slot types:
  - `string` — a PROSE insertion, rendered BARE (never quoted, never escaped): kind-names,
    field names, resolver identities, remediation commands, and other tool-composed text that
    reads as part of the sentence (e.g. `Expected record ... got array.`,
    `Run: strictspec migrate ...`). A `string` slot never carries a raw document-derived value;
    such values use the `value` type below.
  - `value` — a rendered DOCUMENT value, rendered per the A.1 diagnostic table (strings
    double-quoted, A.2-escaped, and A.4-truncated; floats from their source lexeme; etc.).
    Document-derived text — including a regex PATTERN carried in a diagnostic — is a `value`
    slot, so it renders double-quoted with A.2 escaping (see `appendix-rendering.md` A.7).
  - `path` — rendered per the Part B path grammar.
  - `int` — decimal digits (A.1 integer).
  - `code` — a bare `STRICTSPEC_*` string.
  - `identifier` — a schema-declared name, rendered bare.
  - `version` — an integer `format_version` or `meta_version`, rendered as decimal digits.
  - `list<T>` — the pinned truncated inline form (A.5).
- SLOT-RENDERING AMENDMENT (2026-07-27): `string` slots are PROSE and render BARE;
  document-derived values (including regex patterns) use the `value` type and render per A.1.
  The three regex-pattern slots (`STRICTSPEC_VALUE_STRING_REGEX`, `STRICTSPEC_VALUE_MAP_KEY_REGEX`,
  `STRICTSPEC_SCALAR_LEXEME`) are RE-TYPED from `string` to `value` (string-kinded) so they keep
  rendering double-quoted with A.2 escaping — preserving amendment A.7's intent without a
  string-slot special case. See `appendix-rendering.md` A.7 and Part D.

## 3. Closed area set

| Area | Domain |
|---|---|
| `PARSE` | Per-format lexical/syntactic parse failures (JSON, JSONL, TOML) |
| `SCHEMA` | Meta-schema violations — schema-authoring errors |
| `IMPORT` | Shared type-definition import violations (decision 21) |
| `ENUMSRC` | Enum-arms-sourced-from-document declaration and freshness (decision 32) |
| `GATE` | Document `format_version` gate errors |
| `METAGATE` | Schema `meta_version` gate errors |
| `KEY` | Unknown keys and duplicate keys |
| `TYPE` | Type mismatches per construct |
| `VALUE` | Value-constraint violations |
| `INTRA` | Intra-document constraint violations |
| `CROSS` | Cross-document constraint violations (incl. aggregates) |
| `UNION` | Union dispatch errors |
| `ALIAS` | Alias both-present |
| `DEPTH` | Recursion-depth-exceeded |
| `NUM` | `safe_integers` and `number`-scalar lexeme errors |
| `MIGRATE` | Migration engine errors (the 13-op set) |
| `SERIALIZE` | Write-path serialization refusals (producer-current-only invariant) |
| `CHANNEL` | Live-channel version-negotiation refusals (boundary invariant, leg 3) |
| `DIFF` | `diff` / certificate errors |
| `DOCDIFF` | `doc-diff` errors |
| `MANIFEST` | `strictspec.toml` / CLI errors |
| `SCALAR` | Custom-scalar lexeme and registration violations |

## 4. Parse errors (`STRICTSPEC_PARSE_*`)

Emitted before any schema is consulted; raw-text inputs only, so these always carry a source
position (line + byte offset, per `appendix-rendering.md`). All are per-format.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_PARSE_JSON_SYNTAX` | `JSON parse error at {path}: {detail}.` | detail: string | Malformed JSON token/structure. `{path}` is the byte position. |
| `STRICTSPEC_PARSE_JSON_TRAILING` | `Trailing content after the JSON document at {path}.` | — | Bytes after the top-level value. |
| `STRICTSPEC_PARSE_JSON_EMPTY` | `Empty input: expected a JSON document, found none.` | — | Empty or whitespace-only (see rendering appendix edge inputs). |
| `STRICTSPEC_PARSE_JSON_DUPLICATE_KEY` | `Duplicate key {key} in JSON object at {path}.` | key: string, path: path | Hard error in every backend (Python via object_pairs_hook). |
| `STRICTSPEC_PARSE_JSONL_LINE_SYNTAX` | `JSONL parse error on line {line} at {path}: {detail}.` | line: int, path: path, detail: string | Per-line; other lines still processed (one-pass per line). |
| `STRICTSPEC_PARSE_JSONL_TRAILING_CR` | `Line {line} ends with a carriage return; JSONL is LF-only.` | line: int | Trailing CR invalidates the line. |
| `STRICTSPEC_PARSE_JSONL_BLANK_LINE` | `Blank line {line} is not a valid JSONL document.` | line: int | Pinned edge case (rendering appendix item 9). |
| `STRICTSPEC_PARSE_JSONL_DUPLICATE_KEY` | `Duplicate key {key} on JSONL line {line} at {path}.` | key: string, line: int, path: path | Per-line duplicate-key rule. |
| `STRICTSPEC_PARSE_TOML_SYNTAX` | `TOML parse error at {path}: {detail}.` | path: path, detail: string | Lossless parser lexical/structural failure. |
| `STRICTSPEC_PARSE_TOML_DUPLICATE_KEY` | `Duplicate key {key} in TOML table at {path}.` | key: string, path: path | TOML redefinition. |
| `STRICTSPEC_PARSE_TOML_EMPTY` | `Empty TOML input: expected a document, found none.` | — | Empty-file edge case. |

## 5. Meta-schema violations (`STRICTSPEC_SCHEMA_*`)

Emitted while validating a schema (a document of the meta-schema) or the manifest. These abort
`gen`, `check`, and any read using the offending schema.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_SCHEMA_DEFAULT_KEY` | `The `default` key is not a construct of strictspec (field {path}); remove it. A typed value never carries data the author did not write.` | path: path | Decision 30. Dedicated diagnostic. |
| `STRICTSPEC_SCHEMA_TUPLE_ARRAY_BOUNDS` | `Field {path} declares both tuple form and array-length bounds; the two are mutually exclusive.` | path: path | Construct set exclusivity. |
| `STRICTSPEC_SCHEMA_ALIAS_ON_DISCRIMINATOR` | `Alias {alias} targets discriminator field {path}; a discriminator may not be aliased.` | alias: identifier, path: path | Union integrity. |
| `STRICTSPEC_SCHEMA_TOML_NULLABLE` | `A nullable union is reachable at {path}, but the target document syntax is TOML; TOML models absence as an optional field.` | path: path | Rejected at meta-schema time when syntax is known TOML. |
| `STRICTSPEC_SCHEMA_REGEX_UNSUPPORTED` | `Regex at {path} uses a feature outside the RE2-compatible subset: {detail}.` | path: path, detail: string | Vetted at schema-validation time. |
| `STRICTSPEC_SCHEMA_DATETIME_KIND_MISMATCH` | `Datetime range at {path} compares an offset scalar with a local scalar; comparisons must be same-kind.` | path: path | Cross-kind comparison ban. |
| `STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE` | `Opaque JSON leaf {path} declares neither `consumer_check` nor `unchecked`; one is required.` | path: path | Decision 29. |
| `STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON` | `Leaf {path} declares `unchecked = true` without the mandatory `unchecked_reason`.` | path: path | Decision 29. |
| `STRICTSPEC_SCHEMA_TS_WITHOUT_SAFE_INTEGERS` | `Schema {schema} declares a TypeScript target but omits `safe_integers = true`; a TS target requires it.` | schema: identifier | Hard error at `gen` time. |
| `STRICTSPEC_SCHEMA_UNKNOWN_META_KEY` | `Unknown meta-schema key {key} at {path}.{suggestion}` | key: string, path: path, suggestion: string | Unknown-key invariant applied to schemas; suggestion appended when within did-you-mean threshold. |
| `STRICTSPEC_SCHEMA_UNKNOWN_TYPE_REF` | `Field {path} references named type {name}, which is not declared or imported.` | path: path, name: identifier | Dangling type reference. |
| `STRICTSPEC_SCHEMA_NODE_KIND_UNION_AMBIGUOUS` | `Undiscriminated union at {path} has two arms of the same node kind ({kind}); same-kind arms require a discriminator.` | path: path, kind: string | Node-kind union rule (decision 15). |
| `STRICTSPEC_SCHEMA_MISSING_FORMAT_VERSION` | `Schema {schema} does not declare `format_version`.` | schema: identifier | Every schema pins the value its documents must carry. |
| `STRICTSPEC_SCHEMA_MISSING_META_VERSION` | `Schema {schema} does not declare `meta_version`.` | schema: identifier | Schema-language version. |

## 6. Import violations (`STRICTSPEC_IMPORT_*`)

Shared type-definition imports (decision 21): types only, no cross-file constraints, no
transitive imports.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_IMPORT_MISSING_TYPE_FILE` | `Type-definition file {file} imported by {schema} does not exist.` | file: string, schema: identifier | Missing type file. |
| `STRICTSPEC_IMPORT_UNKNOWN_TYPE` | `Type {name} is not defined in imported file {file}.` | name: identifier, file: string | Unknown named type in the import. |
| `STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT` | `Imported file {file} declares a constraint; type-definition files may declare types only, not constraints.` | file: string | Cross-file constraint rejection. |
| `STRICTSPEC_IMPORT_TRANSITIVE` | `Type-definition file {file} imports another file; transitive imports are not permitted.` | file: string | Transitive import rejection. |

## 7. Enum-sourcing errors (`STRICTSPEC_ENUMSRC_*`)

Enum arms sourced from a named document (decision 32), with toolchain-enforced freshness.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_ENUMSRC_MISSING_SOURCE` | `Enum at {path} sources arms from {source}, which does not exist.` | path: path, source: string | Source document missing. |
| `STRICTSPEC_ENUMSRC_STALE` | `Baked enum arms at {path} differ from source {source}; regenerate with `strictspec gen`. Baked: {baked}. Source: {actual}.` | path: path, source: string, baked: list\<string>, actual: list\<string> | Freshness hard error in `gen` and `check`. |
| `STRICTSPEC_ENUMSRC_SOURCE_NOT_STRINGS` | `Enum source {source} at {path} yields a non-string arm; sourced enum arms must be strings.` | source: string, path: path | Sourced arms must be string-typed literals. |
| `STRICTSPEC_ENUMSRC_BAD_SELECTOR` | `Enum-source selector {selector} at {path} does not resolve within {source}.` | selector: string, path: path, source: string | The selector is outside the pinned selector grammar (appendix-surface-syntax.md §7 — key steps and `[]` array-flatten only) or does not resolve within the source. |

## 8. Version-gate errors (`STRICTSPEC_GATE_*`)

The document `format_version` gate runs first, before structural validation. Every gate
diagnostic carries the STRUCTURED REMEDIATION PAYLOAD as slots.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_GATE_ABSENT` | `Document is missing `format_version`. Schema {schema} expects {expected}. Run: {invocation}` | schema: identifier, expected: version, invocation: string | Absent field. |
| `STRICTSPEC_GATE_WRONG_TYPE` | `Document `format_version` must be an integer; got {got}. Schema {schema} expects {expected}. Run: {invocation}` | got: value, schema: identifier, expected: version, invocation: string | Non-integer `format_version`. |
| `STRICTSPEC_GATE_UNSUPPORTED` | `Document `format_version` is {got}, but schema {schema} accepts exactly {expected} (migration set {migset}). Run: {invocation}` | got: version, schema: identifier, expected: version, migset: identifier, invocation: string | The three-message pattern's core case; `{invocation}` is the exact `strictspec migrate` command. |

The `{invocation}` slot is always the literal command an operator runs to remediate, e.g.
`strictspec migrate --schema <schema> --to <expected> <paths>`. No inference, no ranges, no
legacy modes.

## 9. meta_version-gate errors (`STRICTSPEC_METAGATE_*`)

The schema-language version gate on schema files (schemas are documents of the meta-schema).

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_METAGATE_ABSENT` | `Schema {schema} is missing `meta_version`. This strictspec release expects {expected}. Run: {invocation}` | schema: identifier, expected: version, invocation: string | Absent. |
| `STRICTSPEC_METAGATE_WRONG_TYPE` | `Schema `meta_version` must be an integer; got {got}.` | got: value | Non-integer. |
| `STRICTSPEC_METAGATE_UNSUPPORTED` | `Schema {schema} declares `meta_version` {got}, but this strictspec release supports {expected}. Run: {invocation}` | schema: identifier, got: version, expected: version, invocation: string | Meta-migrations upgrade schema files like any document. |

## 10. Unknown and duplicate keys (`STRICTSPEC_KEY_*`)

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_KEY_UNKNOWN` | `Unknown key {key} at {path}.{suggestion}` | key: string, path: path, suggestion: string | The language invariant. `{suggestion}` appends ` Did you mean {name}?` only within did-you-mean threshold; empty otherwise. |
| `STRICTSPEC_KEY_DUPLICATE` | `Duplicate key {key} at {path}.` | key: string, path: path | Structural duplicate detected post-parse (distinct from format-level `PARSE_*_DUPLICATE_KEY`, which fires at parse time; this covers tagged-value construction). |

Note: format-specific duplicate keys surface at parse time (`STRICTSPEC_PARSE_*_DUPLICATE_KEY`).
`STRICTSPEC_KEY_DUPLICATE` covers duplicates arriving through the tagged-value entry point,
where there is no parse phase.

## 11. Type mismatches (`STRICTSPEC_TYPE_*`)

One code per construct-level type mismatch. `{expected}` names the declared construct;
`{got}` renders the offending value's node kind or lexeme class.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_TYPE_MISMATCH` | `Expected {expected} at {path}, got {got}.` | expected: string, got: string, path: path | General scalar/record/array/map/tuple mismatch (catch-all for node-kind disagreement). |
| `STRICTSPEC_TYPE_NOT_INTEGER` | `Expected an integer at {path}, got {got}.` | path: path, got: string | Float lexeme where integer required (`3.0` vs `3`). |
| `STRICTSPEC_TYPE_NOT_FLOAT` | `Expected a float at {path}, got {got}.` | path: path, got: string | Integer lexeme where float required. |
| `STRICTSPEC_TYPE_NOT_BOOLEAN` | `Expected a boolean at {path}, got {got}.` | path: path, got: string | Booleans are not integers. |
| `STRICTSPEC_TYPE_NOT_STRING` | `Expected a string at {path}, got {got}.` | path: path, got: string | |
| `STRICTSPEC_TYPE_NOT_RECORD` | `Expected a record at {path}, got {got}.` | path: path, got: string | Closed record. |
| `STRICTSPEC_TYPE_NOT_ARRAY` | `Expected an array at {path}, got {got}.` | path: path, got: string | |
| `STRICTSPEC_TYPE_NOT_MAP` | `Expected a map at {path}, got {got}.` | path: path, got: string | Typed map. |
| `STRICTSPEC_TYPE_NOT_DATE` | `Expected a date at {path}, got {got}.` | path: path, got: string | Datetime scalar kind `date`. |
| `STRICTSPEC_TYPE_NOT_TIME` | `Expected a time at {path}, got {got}.` | path: path, got: string | Kind `time`. |
| `STRICTSPEC_TYPE_NOT_DATETIME` | `Expected a datetime at {path}, got {got}.` | path: path, got: string | Kind `datetime`; includes non-conforming RFC 3339 strings in JSON. |
| `STRICTSPEC_TYPE_DATETIME_KIND` | `Expected a {expected} datetime at {path}; got a {got} datetime.` | expected: string, got: string, path: path | Offset-vs-local mismatch at read time. |
| `STRICTSPEC_TYPE_NOT_LITERAL` | `Expected the literal {expected} at {path}, got {got}.` | expected: value, got: value, path: path | Literal constant. |
| `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` | `Value {got} at {path} is not one of {expected}.{suggestion}` | got: value, path: path, expected: list\<string>, suggestion: string | Enum membership; suggestion within did-you-mean threshold. |
| `STRICTSPEC_TYPE_MISSING_REQUIRED` | `Missing required field {key} at {path}.` | key: string, path: path | Emitted in schema-declaration order (traversal rule). |
| `STRICTSPEC_TYPE_TUPLE_ARITY` | `Tuple at {path} expects {expected} elements, got {got}.` | expected: int, got: int, path: path | Fixed-size tuple. |

## 12. Value-constraint violations (`STRICTSPEC_VALUE_*`)

Every value constraint in the construct set.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_VALUE_NUM_TOO_SMALL` | `Value {actual} at {path} is below the minimum {limit}.` | actual: value, path: path, limit: value | Inclusive lower bound. |
| `STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE` | `Value {actual} at {path} must be greater than {limit}.` | actual: value, path: path, limit: value | Exclusive lower bound. |
| `STRICTSPEC_VALUE_NUM_TOO_LARGE` | `Value {actual} at {path} is above the maximum {limit}.` | actual: value, path: path, limit: value | Inclusive upper bound. |
| `STRICTSPEC_VALUE_NUM_TOO_LARGE_EXCLUSIVE` | `Value {actual} at {path} must be less than {limit}.` | actual: value, path: path, limit: value | Exclusive upper bound. |
| `STRICTSPEC_VALUE_DATETIME_BEFORE` | `Datetime {actual} at {path} is before the minimum {limit}.` | actual: value, path: path, limit: value | Same-kind datetime range. |
| `STRICTSPEC_VALUE_DATETIME_AFTER` | `Datetime {actual} at {path} is after the maximum {limit}.` | actual: value, path: path, limit: value | Same-kind datetime range. |
| `STRICTSPEC_VALUE_STRING_TOO_SHORT` | `String at {path} has {actual} code points; minimum is {limit}.` | actual: int, path: path, limit: int | Code points, never bytes. |
| `STRICTSPEC_VALUE_STRING_TOO_LONG` | `String at {path} has {actual} code points; maximum is {limit}.` | actual: int, path: path, limit: int | Code points. |
| `STRICTSPEC_VALUE_STRING_EMPTY` | `String at {path} is empty; a non-empty value is required.` | path: path | Non-empty constraint. |
| `STRICTSPEC_VALUE_STRING_REGEX` | `String {actual} at {path} does not match the required pattern {pattern}.` | actual: value, path: path, pattern: value | Value regex. `{pattern}` is a `value` slot (string-kinded): double-quoted, A.2-escaped (A.7). |
| `STRICTSPEC_VALUE_MAP_KEY_REGEX` | `Map key {key} at {path} does not match the required key pattern {pattern}.` | key: string, path: path, pattern: value | Regex on map keys. `{pattern}` is a `value` slot (A.7). |
| `STRICTSPEC_VALUE_ARRAY_TOO_SHORT` | `Array at {path} has {actual} elements; minimum is {limit}.` | actual: int, path: path, limit: int | Array-length bound. |
| `STRICTSPEC_VALUE_ARRAY_TOO_LONG` | `Array at {path} has {actual} elements; maximum is {limit}.` | actual: int, path: path, limit: int | Array-length bound. |

## 13. Intra-document constraint violations (`STRICTSPEC_INTRA_*`)

Phase-2 diagnostics, ordered after phase 1. Decidable from document + schema alone.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED` | `Field {key} at {path} is required when {condition}.` | key: string, path: path, condition: string | Presence- or value-triggered. |
| `STRICTSPEC_INTRA_CONDITIONAL_VALUE` | `Field {key} at {path} must equal {expected} when {condition}; got {got}.` | key: string, path: path, expected: value, got: value, condition: string | Conditional literal value-equality (wavescript Pin; rlsbl preid/bump). |
| `STRICTSPEC_INTRA_EXACTLY_ONE_OF` | `Exactly one of {fields} must be present at {path}; found {actual}.` | fields: list\<string>, path: path, actual: list\<string> | |
| `STRICTSPEC_INTRA_AT_LEAST_ONE_OF` | `At least one of {fields} must be present at {path}; none were.` | fields: list\<string>, path: path | |
| `STRICTSPEC_INTRA_CO_PRESENCE` | `Fields {fields} at {path} must be present together or absent together; found {actual}.` | fields: list\<string>, path: path, actual: list\<string> | A iff B. |
| `STRICTSPEC_INTRA_MUTUAL_EXCLUSION` | `Fields {fields} at {path} are mutually exclusive; found {actual}.` | fields: list\<string>, path: path, actual: list\<string> | Field-level: at most one of a field set present. |
| `STRICTSPEC_INTRA_COLLECTIONS_DISJOINT` | `Arrays {fields} at {path} must share no element; {value} appears in both (normalization: {normalization}).` | fields: list\<string>, path: path, value: value, normalization: string | Element-level set disjointness (rlsbl include/exclude); normalization `none`/`case-fold`/`trim`. |
| `STRICTSPEC_INTRA_FORBIDDEN_WHEN` | `Field {key} at {path} is forbidden when {condition}.` | key: string, path: path, condition: string | |
| `STRICTSPEC_INTRA_UNIQUE_BY` | `Duplicate value {value} for unique-by {field} at {path} (normalization: {normalization}).` | value: value, field: string, path: path, normalization: string | `{normalization}` is `none`, `case-fold`, or `trim`. |
| `STRICTSPEC_INTRA_PAIRWISE_DISTINCT` | `Values at {path} must be pairwise distinct; {value} repeats (normalization: {normalization}).` | path: path, value: value, normalization: string | Same normalization set. |
| `STRICTSPEC_INTRA_RANGES_DISJOINT` | `Half-open ranges at {path} overlap: {value} intersects {actual}.` | path: path, value: value, actual: value | Half-open; missing/invalid bounds source is a hard error under SCHEMA. |
| `STRICTSPEC_INTRA_ORDERED_PAIR` | `Field {actual} at {path} must be less than sibling {value}.` | actual: string, path: path, value: string | a < b between siblings. |
| `STRICTSPEC_INTRA_REFERENCE_UNRESOLVED` | `Reference {value} at {path} does not resolve within the document.` | value: value, path: path | Intra-document references. |

## 14. Cross-document constraint violations (`STRICTSPEC_CROSS_*`)

Evidence supplied by named resolvers; executed by the constraint engine; same conformance
guarantee as structural checks. Includes the two new aggregate forms.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_CROSS_REFERENCE_UNRESOLVED` | `Reference {value} at {path} does not resolve in {source}.` | value: value, path: path, source: string | named-reference-must-resolve; `{source}` is the resolver-provided set identity. |
| `STRICTSPEC_CROSS_SET_COVERAGE` | `Element {value} of {source} is not covered by the collection at {path}.` | value: value, source: string, path: path | set-coverage. |
| `STRICTSPEC_CROSS_COLLECTION_UNIQUE` | `Value {value} at {path} also appears in {source}; it must be unique across the collection family.` | value: value, path: path, source: string | cross-collection-unique. |
| `STRICTSPEC_CROSS_COUNT_LIMIT` | `Collection at {path} has {actual} elements across {source}; the limit is {limit}.` | path: path, actual: int, source: string, limit: int | count-limit; LITERAL bound only (decision 23). |
| `STRICTSPEC_CROSS_SUM_LIMIT` | `Sum of {field} across {source} at {path} is {actual}; the limit is {limit}.` | field: string, source: string, path: path, actual: value, limit: value | sum-limit; LITERAL bound only. |
| `STRICTSPEC_CROSS_SUM_FIELD_MISSING` | `sum-limit at {path} over {source} requires numeric field {field} on every selected document; document {actual} lacks it or has a non-numeric value.` | path: path, source: string, field: string, actual: string | Heterogeneous/missing sum field is a HARD ERROR, never skip-or-zero (aggregates gap note). |
| `STRICTSPEC_CROSS_RESOLVER_UNAVAILABLE` | `Constraint at {path} requires evidence resolver {source}, which this environment cannot satisfy.` | path: path, source: string | Hard error at check-execution time — never a skip (decision 4/23). |

## 15. Union dispatch errors (`STRICTSPEC_UNION_*`)

Per the union-diagnostics section: non-matching arms are never validated.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_UNION_DISCRIMINATOR_MISSING` | `Missing discriminator {key} at {path}; expected one of {expected}.` | key: string, path: path, expected: list\<string> | No arm validated. |
| `STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN` | `Discriminator {got} at {path} is not one of {expected}.{suggestion}` | got: value, path: path, expected: list\<string>, suggestion: string | Enum-style; no arm validated; suggestion within threshold. |
| `STRICTSPEC_UNION_NODE_KIND` | `No union arm at {path} accepts a {got}; expected one of {expected}.` | got: string, path: path, expected: list\<string> | Node-kind union; one diagnostic naming accepted kinds. |

Note: when a discriminator is valid but the matched arm's body is invalid, the arm's own
diagnostics (from sections 11–14) are emitted at natural paths with the arm as context — there
is no separate union code for that case.

## 16. Alias, depth, number (`STRICTSPEC_ALIAS_*`, `STRICTSPEC_DEPTH_*`, `STRICTSPEC_NUM_*`)

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_ALIAS_BOTH_PRESENT` | `Both {alias} and canonical {canonical} are present at {path}; provide exactly one.` | alias: identifier, canonical: identifier, path: path | Aliases are exempt from unknown-key errors but both-present is an error. |
| `STRICTSPEC_DEPTH_EXCEEDED` | `Document nesting at {path} exceeds the maximum validation depth of {limit}.` | path: path, limit: int | Fires before CPython stack exhaustion; canonical diagnostic. |
<!-- The two pipes in `\|n\|` are ESCAPED so this row is valid markdown (a literal `|`
     inside a table cell must be `\|`). The rendered template text is `(|n| >= 2^53)`
     unchanged. Do not "un-escape" them. (formatting amendment 2026-07-27) -->
| `STRICTSPEC_NUM_SAFE_INTEGER` | `Integer {actual} at {path} exceeds the safe-integer range (\|n\| >= 2^53) required by `safe_integers`.` | actual: value, path: path | Schema-wide when declared; identical verdict across backends. |
| `STRICTSPEC_NUM_UNREPRESENTABLE` | `Lexeme {actual} at {path} cannot be represented exactly as float64; the `number` scalar refuses silent precision loss.` | actual: value, path: path | number-scalar unrepresentable lexeme. |
| `STRICTSPEC_NUM_INT_OVERFLOW` | `Integer lexeme {actual} at {path} overflows int64.` | actual: value, path: path | Integer lexeme beyond int64. |
| `STRICTSPEC_NUM_FLOAT_OVERFLOW` | `Float lexeme {actual} at {path} is beyond float64 range.` | actual: value, path: path | Float lexeme beyond float64. |
| `STRICTSPEC_NUM_NON_FINITE` | `Non-finite number {actual} at {path} is not permitted.` | actual: value, path: path | NaN / Infinity rejected. |

## 17. Migration errors (`STRICTSPEC_MIGRATE_*`)

The closed 13-op set: add_field, remove_field, rename_field, move_field, set_value,
set_value_where, remove_where, add_collection, drop_collection, append, merge_defaults,
wrap_in_array, unwrap_singleton. Ops move/rename/reshape/delete/inject literals — never
compute a value from a value.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_MIGRATE_TARGET_MISSING` | `Migration op {op} targets {path}, which is absent in the document.` | op: string, path: path | op-target missing. |
| `STRICTSPEC_MIGRATE_COLLISION` | `Migration op {op} would write {path}, which already exists.` | op: string, path: path | add onto existing, rename onto existing, etc. |
| `STRICTSPEC_MIGRATE_TYPE_MISMATCH` | `Migration op {op} at {path} expected {expected}, found {got}.` | op: string, path: path, expected: string, got: string | e.g. unwrap_singleton on a non-array. |
| `STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON` | `unwrap_singleton at {path} requires a single-element array; found {actual} elements.` | path: path, actual: int | partial-down failure surface; canonical hard error. |
| `STRICTSPEC_MIGRATE_ON_CURRENT` | `Document at {path} is already at the current `format_version` {expected}; nothing to migrate.` | path: path, expected: version | migrate-on-current. |
| `STRICTSPEC_MIGRATE_UNKNOWN_SET` | `No migration set {migset} is registered for schema {schema}.` | migset: identifier, schema: identifier | unknown migration set. |
| `STRICTSPEC_MIGRATE_REVALIDATION_FAILED` | `Migrated document at {path} does not validate at `format_version` {expected}; the migration is unsound.` | path: path, expected: version | Post-migration revalidation (all-or-nothing run aborts). |
| `STRICTSPEC_MIGRATE_PREDICATE_UNSUPPORTED` | `Predicate at {path} tests more than equality and presence; migration predicates are restricted.` | path: path | Admission-criterion enforcement. |
| `STRICTSPEC_MIGRATE_IRREVERSIBLE_DOWN` | `Op {op} at {path} is declared irreversible; a down-migration was requested.` | op: string, path: path | Reversibility taxonomy. |

## 17a. Write-path serialization refusals (`STRICTSPEC_SERIALIZE_*`)

The producer-current-only leg of the version-boundary invariant (decision 24; canonical
serialization appendix). The write path refuses to serialize any non-current `format_version`.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_SERIALIZE_NONCURRENT` | `Refusing to serialize document at {path}: its `format_version` is {got}, but schema {schema} serializes only the current {expected}. Migrate before writing.` | path: path, got: version, schema: identifier, expected: version | No conforming producer can create new staleness. |

## 17b. Live-channel version-negotiation refusals (`STRICTSPEC_CHANNEL_*`)

The live-channel leg of the version-boundary invariant (decision 24, leg 3): a channel agrees on
one `format_version` or refuses to open; a receiver that cannot speak the negotiated version
refuses with a structured "update the client" payload (browser runtimes never migrate).

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_CHANNEL_VERSION_REFUSED` | `Cannot open channel for schema {schema}: peer offers `format_version` {got}, this endpoint speaks only {expected}. Update the client to the paired strictspec release ({release}).` | schema: identifier, got: version, expected: version, release: string | Structured "update the client" payload; hard failure, no fallback. |

## 18. Diff / certificate errors (`STRICTSPEC_DIFF_*`)

The empirical engine (decision 25). The proof-carrying analyzer is unbundled; the `proven`
grade is reserved.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_DIFF_CORPUS_EMPTY` | `The corpus glob {source} resolved to zero documents; `diff` requires a non-empty corpus.` | source: string | corpus glob resolves nothing / corpus empty. |
| `STRICTSPEC_DIFF_VIOLATED` | `Claim {condition} is VIOLATED: corpus document {source} is a counterexample.` | condition: string, source: string | violated-grade finding; blocks release. |
| `STRICTSPEC_DIFF_NARROWING_UNBUMPED` | `Document {source} is valid under the old schema but invalid under the new schema at the same `format_version` {expected}; this narrowing requires a version bump.` | source: string, expected: version | same-version narrowing detection (decision 25 / 13). |
| `STRICTSPEC_DIFF_TAXONOMY_MISDECLARED` | `Op {op} is declared {expected} but the corpus shows it is {actual}.` | op: string, expected: string, actual: string | down-taxonomy mis-declaration; hard error. |
| `STRICTSPEC_DIFF_ADJUDICATION_MISSING` | `Claim {condition} is corpus-supported without a corpus, and no adjudication entry covers it.` | condition: string | No-corpus consumer failed to discharge via adjudication file. |
| `STRICTSPEC_DIFF_ADJUDICATION_INVALID` | `Adjudication file {source} does not validate against the adjudication schema: {detail}.` | source: string, detail: string | The adjudication file is a strictspec-schema'd document. |

## 19. doc-diff errors (`STRICTSPEC_DOCDIFF_*`)

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_DOCDIFF_SCHEMA_MISMATCH` | `Documents {source} and {actual} do not share `format_version`; `doc-diff` compares documents of one schema at one version.` | source: string, actual: string | Both operands must be the same schema+version. |
| `STRICTSPEC_DOCDIFF_INVALID_OPERAND` | `Operand {source} does not validate against the schema; `doc-diff` compares valid documents.` | source: string | doc-diff refuses invalid operands. |

## 20. CLI / manifest errors (`STRICTSPEC_MANIFEST_*`)

`strictspec.toml` is a document of a toolchain-shipped built-in schema; it is gated and
migrated like any document. General manifest-schema violations reuse `STRICTSPEC_SCHEMA_*` and
`STRICTSPEC_GATE_*`; the codes below are manifest-specific hard errors.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_MANIFEST_ALREADY_EXISTS` | `A `strictspec.toml` already exists at {path}; `strictspec init` refuses to overwrite it.` | path: path | init guard. |
| `STRICTSPEC_MANIFEST_UNKNOWN_STORE` | `Manifest declares store {name}, whose kind {got} is not a recognized store kind.` | name: identifier, got: string | Store declaration. |
| `STRICTSPEC_MANIFEST_UNKNOWN_RESOLVER` | `Manifest / schema references evidence resolver {name}, which is not in the resolver vocabulary.` | name: identifier | Closed resolver vocabulary. |
| `STRICTSPEC_MANIFEST_GENERATED_PATH_DIRTY` | `Generated path {path} has uncommitted local edits; regenerate before proceeding.` | path: path | `check` drift/regeneration guard. |
| `STRICTSPEC_MANIFEST_PAIRING_MISMATCH` | `Runtime version {actual} does not match generated-code version {expected}; regenerate against the paired release.` | actual: version, expected: version | Exact version pairing (decision 19). |
| `STRICTSPEC_MANIFEST_DRIFT` | `Generated output at {path} differs from a fresh generation; run `strictspec gen` and commit.` | path: path | Byte-compare drift gate (decision 18). |

## 21. Custom-scalar errors (`STRICTSPEC_SCALAR_*`)

Per `appendix-custom-scalars.md`. Registration lives in the manifest and travels with the
schema.

| Code | Template | Slots | Notes |
|---|---|---|---|
| `STRICTSPEC_SCALAR_LEXEME` | `Value {actual} at {path} does not match the {name} scalar's lexeme rule {pattern}.` | actual: value, path: path, name: identifier, pattern: value | Anchored-regex refinement failure. `{pattern}` is a `value` slot (string-kinded): double-quoted, A.2-escaped (A.7). |
| `STRICTSPEC_SCALAR_BASE_MISMATCH` | `Value at {path} is not a {expected} lexeme, which the {name} scalar refines.` | path: path, expected: string, name: identifier | Base lexeme class disagreement. |
| `STRICTSPEC_SCALAR_UNKNOWN` | `Field {path} uses custom scalar {name}, which is not registered in the manifest.` | path: path, name: identifier | Unregistered scalar reference. |
| `STRICTSPEC_SCALAR_NO_BINDING` | `Custom scalar {name} declares no binding for target {got}; every declared target requires a binding.` | name: identifier, got: string | Per-target binding obligation. |
| `STRICTSPEC_SCALAR_LENGTH` | `Value at {path} violates the {name} scalar's length bound ({actual}, limit {limit}).` | path: path, name: identifier, actual: int, limit: int | For length/non-empty refinements on opaque scalars (e.g. sql-expression). |

## 22. Cross-references

- Slot rendering, path grammar, truncation, and did-you-mean: `appendix-rendering.md`.
- Certificate and adjudication-file shapes referenced by `STRICTSPEC_DIFF_*`:
  `appendix-certificates.md`.
- Per-construct accepted-set semantics behind every `TYPE`/`VALUE`/`INTRA`/`CROSS` code:
  `appendix-semantics.md`.
- Custom-scalar registration behind `STRICTSPEC_SCALAR_*`: `appendix-custom-scalars.md`.
- How templates become per-target renderers: `appendix-emitter-ir.md`.
- The concrete surface that produces these conditions (enum selector, migration ops, constraint
  bodies): `appendix-surface-syntax.md`.
- The constitution: `DESIGN.md`.
