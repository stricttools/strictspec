# Gap note — rlsbl release file (`.rlsbl/releases/unreleased.toml`)

Source: `rlsbl/rlsbl/release_file.py` — `ReleaseConfig`, `_validate_release_config`.
Schema: `release-file.schema.toml`. Samples: `valid-01-infra.toml`,
`invalid-01-infra-preid.toml`.

## Expressed cleanly

- **Enums.** `bump` (VALID_BUMP_TYPES), `preid` (VALID_PREIDS), `targets.<name>.mode`
  (VALID_TARGET_MODES) map exactly onto the enum construct (spec appendix-semantics
  §3.13). Non-membership renders `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` with did-you-mean.
- **Required vs optional-absent.** `bump`/`include`/`exclude`/`description` are
  required; `context`/`preid`/`blog`/`targets` are optional. An unset optional
  field binds as ABSENT — exactly rlsbl's `data.get("context", "")` intent, minus
  the injected `""`. Modeling "no value" as an absent field (never a nullable
  union) is the correct TOML choice (spec DESIGN.md Construct set).
- **Typed map with a key regex.** `targets` is a `map` keyed `^[a-z][a-z0-9-]*$`
  to a `TargetConfig` record. Unknown per-target keys (rlsbl rejects any key but
  `mode`) fall out of the closed-record + unknown-key invariant for free.
- **Value-triggered forbidden-when.** `bump == "infra"` forbids `preid` — a clean
  `forbidden-when` (§3.24). This is the one coupling the vocabulary carries exactly.

## Awkward / partial

- **`preid == "stable"` requires `bump == "prerelease"`** is a value→value
  implication ("if sibling A equals literal x, sibling B must equal literal y").
  `conditional-required` only asserts the TARGET FIELD is PRESENT, not that it
  equals a literal (§3.24: "excludes trees satisfying the condition but missing
  the field"). `bump` is required and thus always present, so the constraint as
  written never fires — it cannot catch `preid="stable"` + `bump="minor"`. The
  vocabulary has no "conditional value-equality" form. Partial.
- **`targets.<name>` key must be in `include`.** Modeled as
  `intra-document-references` from a map key into the `$.include` array. The
  reference form is defined as "a reference resolves within the same document"
  (§3.24) — resolution against a sibling ARRAY of strings is a reasonable reading,
  but the spec's only cited origin is `orxtra dependencies` (record→record by id),
  so map-key-into-array resolution is an untested interpretation. Flagged for the
  spec to pin: does `intra-document-references` accept an array-of-scalars as the
  resolution target set?

## INEXPRESSIBLE (findings)

- **FINDING 1 — include/exclude element-level disjointness.** rlsbl enforces
  `set(include) & set(exclude) == {}` (no target in both lists). The constraint
  vocabulary has NO form for "two sibling arrays share no element":
  - `mutual exclusion` is field-level ("at most one of a FIELD SET present",
    §3.24) — both arrays are present, so it is the wrong shape.
  - `pairwise-distinct` / `unique-by` operate WITHIN one collection.
  - `ranges-disjoint` is about numeric half-open ranges, not set membership.
  spec DESIGN.md's cross-field table literally cites *"rlsbl include/exclude"* as
  the origin of the `mutual exclusion` form — that citation is a
  **mischaracterization**: rlsbl's rule is element-level set disjointness, which
  `mutual exclusion` does not express. Either the spec needs a new
  `collections-disjoint` intra-document form, or the DESIGN.md example origin
  should be corrected and this rule declared consumer-native. Recommend the
  former (it is a small, decidable, portable form).

- **FINDING 2 — the Flutter literal-element gate.** rlsbl: *if the string
  `"flutter"` is an element of `include`, then `targets.flutter` must exist and
  carry `mode`.* The trigger is "a specific LITERAL is a member of a sibling
  array" — `conditional-required`'s condition "tests presence/equality only" of a
  FIELD (§3.24), not literal membership in an array. Inexpressible; would become a
  consumer-native check. (A `collections-disjoint`-style extension does not help
  here; this needs an "array-contains-literal" condition predicate.)

## Notes that are NOT gaps

- **TOML-null hard error.** There is no way to write `context = null` — TOML has
  no null literal, so the "null value" case cannot arise from a source TOML
  document at all. A schema author who tried to type `context` as a nullable
  union would be rejected at meta-schema time with `STRICTSPEC_SCHEMA_TOML_NULLABLE`
  (the schema declares `syntax = "toml"`). Absence is the only "no value", and
  the schema models it as an optional field. Correct by construction.
- **Comment-carrying round-trip.** `valid-01-infra.toml` keeps inline comments and
  a triple-quoted `context` with line continuations; a read-then-write fixpoint
  preserves them byte-for-byte within a backend (spec Write path). No gap.
- **`description` strip().** rlsbl rejects whitespace-only `description` after
  `.strip()`. `non_empty` on the string construct rejects the empty string but
  NOT `"   "`. This is a minor semantic mismatch (whitespace-only passes
  `non_empty`); it is arguably a consumer-native refinement, not a spec gap.
  Flagged, not counted — a `pattern = "\\S"` value-regex closes it if wanted.
- **No datetime/temporal field.** examples/DESIGN.md row 5 anticipated this draft
  exercising DATETIME SCALARS (TOML natives, offset/local kind). The actual
  `ReleaseConfig` (and `RetryConfig`) carry no date/time/datetime field, so the
  datetime-scalar stress is simply not present in this source. Coverage note for
  whoever owns the datetime construct exercise — it is not gettable from here.

## Verdict

FINDINGS: 3
- F1: no `collections-disjoint` intra-document form (element-level array
  disjointness); spec DESIGN.md's cross-field table wrongly cites rlsbl
  include/exclude as the origin of `mutual exclusion`.
- F2: no conditional VALUE-EQUALITY form (preid=="stable" ⟹ bump=="prerelease");
  `conditional-required` only asserts presence of the target field.
- F3: no "array-contains-literal" condition predicate (the Flutter gate:
  "flutter" ∈ include ⟹ targets.flutter.mode required).
