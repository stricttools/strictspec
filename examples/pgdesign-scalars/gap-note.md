# Gap note — pgdesign-scalars (custom scalar registration; appendix-custom-scalars.md)

Paper registration of pgdesign's three custom scalars — `identifier`, `pgtype`,
`sql-expression` — plus a small table-definition schema fragment using them. Source material:
`pgdesign/internal/parse/types.go` (`RawTable`, `RawColumn`, `RawCheck` field types). pgdesign
is the first custom-scalar consumer (DESIGN.md donor inventory; decision 3).

## Files

- `strictspec.toml` — manifest fragment registering the three scalars (registration lives in
  the manifest and travels with the schema; appendix-custom-scalars §4).
- `schema-table.toml` — table-definition fragment: `name: identifier`, columns keyed by
  identifier → `{ type: pgtype, nullable?, default_expr: sql-expression?, check: sql-expression? }`,
  checks keyed by identifier → `{ expr: sql-expression }`.
- `account.valid.toml` / `account.invalid.toml` — documents.

## Clean

- **The registration surface fits all three scalars.** Each declares exactly the parts
  appendix-custom-scalars §1 pins: `name`, `base` (all three refine `string`), an anchored RE2
  `lexeme_rule`, an optional `length` bound, per-target `bindings` (one per declared target), and
  a `rendering` entry declaring inheritance from the base class (§2). No part strains.
- **The three points on the honesty spectrum are faithfully represented** (appendix-custom-scalars
  §6):
  - `identifier` — PRECISE: `^[A-Za-z_][A-Za-z0-9_]*$` fully captures the grammar; `length.max =
    63` adds the Postgres identifier cap the regex cannot express.
  - `pgtype` — SURFACE: the regex captures the SHAPE of a type reference (optional schema
    qualification, optional `(p)`/`(p,s)` params, optional `[]` array suffix) but not EXISTENCE.
  - `sql-expression` — OPAQUE: `^.+$` asserts only non-emptiness; `length` is the sole meaningful
    check.
- **The lexeme rule refines on top of the base**, never replacing it (§1): a `pgtype` value is
  first required to be a valid string lexeme, then required to match the `pgtype` regex. A
  non-string in a `pgtype` field is `STRICTSPEC_SCALAR_BASE_MISMATCH`, not a lexeme failure.

### Expected diagnostics — `account.invalid.toml` (traversal order, item 6)

1. `STRICTSPEC_SCALAR_LEXEME` · path `$.name` · slots `{actual: "9lives", name: identifier,
   pattern: "^[A-Za-z_][A-Za-z0-9_]*$"}` (leading digit).
2. `STRICTSPEC_TYPE_MISSING_REQUIRED` · path `$` · slot `{key: "comment"}` (all tables require a
   comment; emitted in schema-declaration order).
3. `STRICTSPEC_SCALAR_LEXEME` · path `$.columns["id"].type` · slots `{actual: "bi gint", name:
   pgtype, pattern: "..."}` (embedded space breaks the surface syntax).
4. `STRICTSPEC_SCALAR_LENGTH` · path `$.columns["note"].check` · slots `{name: sql-expression,
   actual: 0, limit: 1}` (empty string violates the `length.min = 1` non-emptiness bound).

`account.valid.toml` validates clean: `numeric(12,2)` and `text[]` match the `pgtype` surface
regex; `"balance >= 0"` and `"email ~ '...'"` are non-empty `sql-expression`s.

## The honest limits of regex refinement for `sql-expression` (the requested focus)

This is the crux of registering `sql-expression` as a custom scalar — and the reason doing so is
CORRECT rather than a pretense:

- **SQL is not a regular language.** No RE2 regex (RE2 by construction has no backreferences and
  no recursion) can validate balanced parentheses, operator precedence, or subquery nesting in a
  CHECK body or default expression. The `^.+$` rule is not a weak first attempt to be improved
  later — it is the STRONGEST honest regex: it asserts non-emptiness and nothing more. Any regex
  that appeared to validate more (e.g. requiring a comparison operator) would REJECT valid SQL
  (`TRUE`, `fn(x)`) and ACCEPT invalid SQL (`a >< b`) — worse than honest non-emptiness.
- **`length` is the only additional meaningful check**, and it is a real one: it bounds message
  truncation and catches the empty-string typo (fixture 4 above). appendix-custom-scalars §1
  introduces `length` specifically "for effectively-opaque refinements where the regex cannot
  express the real grammar (e.g. sql-expression)".
- **Grammaticality and semantic validity are consumer-native, downstream, by declaration**
  (DESIGN.md — Domain checks; the consumer-native tail). pgdesign ALREADY owns exactly this: its
  `sqlexpr` package (recursive-descent parser, 9 precedence levels) and `sqlparse`
  (WASM go-pgquery) validate expression grammar and deparse — that is consumer code over the
  strictspec-validated typed values, NOT a scalar refinement. Registering `sql-expression` as a
  custom scalar makes the boundary EXPLICIT and on the record (appendix-custom-scalars §6.3:
  "makes that boundary explicit and on the record, rather than pretending a regex validates
  SQL"). The undecidability catalogue's `regex-pair-relationship` class
  (appendix-semantics §4) records that anchored-scalar refinements over external lexeme grammars
  are genuinely outside the regular fragment — `sql-expression` is the canonical instance.

Net: the custom-scalar mechanism lets strictspec validate what IS regular (non-emptiness,
length) and cede — visibly, at a named boundary — what is not.

## Awkward — `pgtype` surface regex is genuinely partial

`pgtype`'s regex is the interesting middle case and deserves an honest caveat.

- **What it captures:** `int4`, `bigint`, `pg_catalog.numeric`, `numeric(10,2)`, `varchar(255)`,
  `text[]`. Good coverage of the common surface.
- **What it does NOT capture, by design:**
  - EXISTENCE — `pg_catalog.frobnicate` matches the shape but is not a real type. This is a
    domain fact (does the type exist in the target DB / declared extensions), a consumer-native
    check; pgdesign's `typeinfo`/`semtype`/`extregistry` already own it.
  - RICHER surface forms — multi-dimensional arrays (`int[][]`), array size hints (`int[3]`),
    `time(3) with time zone`, `character varying`. My regex accepts single `[]` and a single
    parenthesised numeric param group only. Extending the regex to cover more surface forms is
    possible but every extension trades false-negatives for a longer, less legible rule. The
    honest position: the `pgtype` regex captures the COMMON surface; anything it rejects that is
    nonetheless a real Postgres type is a case for either broadening the (still-regular) regex or
    ceding to `typeinfo` — a per-consumer judgement, not a strictspec gap.

This is not a spec gap — appendix-custom-scalars §6.2 explicitly frames `pgtype` as capturing
"surface syntax, not existence". It IS worth recording that the surface regex is a deliberate
subset and its exact coverage is a consumer decision.

## Inexpressible

- **Nothing the custom-scalar surface should express is missing.** The three scalars, their
  length bounds, their per-target bindings, and their five `STRICTSPEC_SCALAR_*` codes cover the
  fragment. The genuinely non-regular parts (SQL grammar, type existence) are correctly OUTSIDE
  the scalar surface and inside pgdesign's existing consumer-native packages.

## Verdict

CLEAN
