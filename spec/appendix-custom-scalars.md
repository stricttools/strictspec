# Appendix: Custom Scalar Registration (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). Custom scalar
> registration is part of the language design (decision 3; construct set). VERSIONED: any change
> to the registration surface is a breaking-class, changelog-covered release event.
>
> META NOTE: Under the soft-freeze regime, pre-release refinements to the registration surface
> are expected and recorded per-release. Build-sequencing note (decision 3): custom scalars are
> built AFTER the acceptance test — that is an implementation ordering, never a design exclusion.
> pgdesign is the first consumer.

A custom scalar is a NAMED refinement of a built-in base lexeme class, with a toolchain-
registered lexeme rule, per-target bindings, and rendering entries. Registration is DECLARATIVE
and lives in the consumer manifest (`strictspec.toml`), so it travels with the schema and is
gated and migrated like any document. strictspec has NO plugin API and NO embedded expression
language (decision 23): a custom scalar is a lexeme refinement plus bindings — never arbitrary
consumer code.

## 1. The registration surface

A custom scalar is registered by a declaration carrying exactly these parts:

| Part | Meaning | Rules |
|---|---|---|
| `name` | the scalar's identifier, used in schemas | unique within the manifest; `ident`-shaped; referencing an unregistered name is `STRICTSPEC_SCALAR_UNKNOWN` |
| `base` | the built-in lexeme class it REFINES | one of the scalar lexeme classes: `string`, `integer`, `float`, `number`, `boolean`, `date`, `time`, `datetime`. A value that is not a `base` lexeme is `STRICTSPEC_SCALAR_BASE_MISMATCH` |
| `lexeme_rule` | an ANCHORED RE2 regex over the SOURCE LEXEME | must compile under the RE2 subset (vetted at schema-validation time, `STRICTSPEC_SCHEMA_REGEX_UNSUPPORTED`); must be anchored (`^…$`); a value that does not match is `STRICTSPEC_SCALAR_LEXEME` |
| `length` (optional) | code-point length bound and/or non-empty flag | for effectively-opaque refinements where the regex cannot express the real grammar (e.g. sql-expression); violation is `STRICTSPEC_SCALAR_LENGTH` |
| `bindings` | per-target native type each target binds | one entry per DECLARED target of the schema; a declared target without a binding is `STRICTSPEC_SCALAR_NO_BINDING` |
| `rendering` | message-side and write-side rendering entries | see section 2 |

The lexeme rule refines the SOURCE LEXEME of a `base`-classed value — it is an additional filter
on top of the base scalar's own lexeme rules, never a replacement for them. A `pgtype` refining
`string`, for instance, is first required to BE a valid string lexeme, then required to match the
`pgtype` anchored regex.

## 2. Rendering entries

A custom scalar declares how its values render in two places, both cross-target normative and
both inheriting the base class's rules from `appendix-rendering.md`:

- MESSAGE rendering (diagnostic): a custom scalar renders exactly as its base class renders
  (`appendix-rendering.md` Part A), truncated per the string-truncation rule when its base is
  `string`. A custom scalar introduces NO new rendered form — refinement narrows the accepted
  set, it does not change how an accepted value prints.
- WRITE-SIDE rendering (constructed values): a constructed custom-scalar value serializes per
  its base class's write-side rule; an untouched value serializes byte-identically to its
  retained source lexeme (lexeme retention). A custom scalar never re-renders a lexeme it did not
  construct.

Because rendering is inherited from the base class, the `rendering` part exists to DECLARE the
inheritance explicitly (which base rendering applies) rather than to define novel formatting; a
declaration that attempted a novel form would be rejected at registration.

## 3. Error-code allocation

All custom-scalar diagnostics live under `STRICTSPEC_SCALAR_*` (see `appendix-error-codes.md`
section 21): `STRICTSPEC_SCALAR_LEXEME`, `STRICTSPEC_SCALAR_BASE_MISMATCH`,
`STRICTSPEC_SCALAR_UNKNOWN`, `STRICTSPEC_SCALAR_NO_BINDING`, `STRICTSPEC_SCALAR_LENGTH`. These
are `STRICTSPEC_*` codes with spec-pinned templates and full cross-target message identity — a
custom scalar's diagnostics are FIRST-CLASS toolchain diagnostics, not consumer-prefixed codes.
(Consumer-prefixed codes belong to consumer-native checks, which are a different thing entirely.)

## 4. Placement and travel

- Registration lives in the consumer manifest `strictspec.toml` (a document of a toolchain-
  shipped built-in schema). It is committed, versioned, and migrated exactly like any document.
- The registration TRAVELS WITH THE SCHEMA: any schema using a custom scalar is only meaningful
  alongside the manifest that registers it. `strictspec gen` reads both; a schema referencing an
  unregistered scalar hard-errors at generation time.
- Because registration is manifest-declared data (not code), the per-target renderers and
  validators for a custom scalar are GENERATED from the declaration, so all four targets enforce
  the identical lexeme rule and emit identical diagnostics — the same conformance guarantee as
  every built-in scalar.

## 5. Conformance obligations

- A registered scalar MUST ship conformance fixtures: positive lexemes (accepted), negative
  lexemes (each expected `STRICTSPEC_SCALAR_*` code + path), base-mismatch cases, and — where a
  `length` refinement is declared — length-boundary cases. Fixtures are hand-authored from this
  appendix (fixture-authoring discipline: never regenerated from a target).
- Resolver/evidence independence: custom scalars are pure lexeme refinements and require NO
  evidence resolver; their verdict is a function of (lexeme, rule) alone and is therefore
  trivially identical across targets.

## 6. Worked examples (pgdesign, the first consumer's set)

These are PAPER examples — declarations, not code — illustrating the honest range of regex
refinement, from a precise grammar to an effectively-opaque blob.

### 6.1 identifier
- Intent: a SQL/programming identifier.
- base: `string`.
- lexeme_rule: `^[A-Za-z_][A-Za-z0-9_]*$` (anchored, RE2).
- length: optional max (e.g. 63 code points, the common Postgres identifier limit) — expressible
  as a `length` bound; the regex alone does not cap length.
- bindings: Go `string`, Python `str`, TS `string` (a nominal/branded string type in TS if the
  consumer wants nominal typing, but the runtime binding is `string`).
- rendering: inherits `string` (quoted, escaped, truncated).
- Honest note: the regex FULLY captures the identifier grammar — this is the precise-refinement
  end of the spectrum.

### 6.2 pgtype
- Intent: a Postgres type name, possibly qualified and array-suffixed (e.g. `int4`,
  `pg_catalog.numeric`, `text[]`).
- base: `string`.
- lexeme_rule: an anchored RE2 regex over the type-name surface syntax (qualified name plus
  optional `[]` suffix). It captures the SHAPE of a type reference, not its EXISTENCE — whether
  `pg_catalog.frobnicate` is a real type is a domain fact, outside any regex.
- length: optional.
- bindings: Go `string`, Python `str`, TS `string`.
- rendering: inherits `string`.
- Honest note: the regex captures surface syntax; SEMANTIC validity (does the type exist in the
  target database) is a consumer-native check downstream, not a scalar refinement.

### 6.3 sql-expression
- Intent: an arbitrary SQL expression fragment (e.g. a CHECK constraint body, a default
  expression).
- base: `string`.
- lexeme_rule: `^.+$` (anchored, matches any non-empty single-or-multiline string) — the regex
  can only assert NON-EMPTINESS; SQL is not a regular language and no RE2 regex validates it.
- length: a `length` bound (non-empty; optional max) is the ONLY meaningful check.
- bindings: Go `string`, Python `str`, TS `string`.
- rendering: inherits `string` (truncated in messages per the 64-code-point rule).
- Honest note: this is the EFFECTIVELY-OPAQUE end of the spectrum. sql-expression demonstrates
  the honest LIMIT of regex refinement: strictspec validates only that the value is a non-empty
  string of bounded length. Grammaticality and semantic validity are NOT checked by strictspec —
  they belong to a consumer-native check (or the database itself). Registering sql-expression as
  a custom scalar makes that boundary explicit and on the record, rather than pretending a regex
  validates SQL.

## Cross-references

- The `STRICTSPEC_SCALAR_*` codes and templates: `appendix-error-codes.md`.
- Base-class rendering inherited by custom scalars: `appendix-rendering.md`.
- The accepted-set effect of a custom scalar (base ∩ anchored-regex ∩ length):
  `appendix-semantics.md` section 3.22, and its undecidability note (regex-pair-relationship
  class) in section 4.
- The constitution and the consumer-native-tail boundary: `DESIGN.md` (Domain checks;
  decision 23).
