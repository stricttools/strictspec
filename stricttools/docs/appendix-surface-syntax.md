+++
description = "The single concrete TOML surface syntax for every strictspec authoring artifact: schema files, type-definition files, manifest scalar and vocabulary declarations, and migration files."
+++
# Appendix: The Concrete TOML Surface Syntax (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). This appendix pins the
> single concrete TOML surface for every strictspec authoring artifact: schema files,
> type-definition files, the consumer manifest's custom-scalar and vocabulary-source declarations,
> and migration files. It is the SINGLE PINNED SPELLING — the toolchain-shipped built-in
> meta-schema (the schema of schemas) is authored in exactly this surface and MUST equal it.
> VERSIONED: any change to the surface is a breaking-class, changelog-covered release event that
> triggers full conformance-fixture regeneration.
>
> META NOTE: strictspec is in its GROWTH PHASE — this surface is STABLE BUT GROWING; refinements
> (key spellings, site shapes) are normal and expected, recorded per-release through the existing
> discipline, with released-surface compatibility governed by semver at release boundaries.

## 1. Why this appendix exists, and which notation won

`DESIGN.md` (Schema syntax; Meta-schema) says schemas are authored in TOML with a notation core
"adapted from incantino (header + per-field type/required/values/description)", but until now no
appendix pinned the actual grammar — how records, maps, unions, constraints, aliases, named types,
imports, enum sourcing, opaque leaves, and migration files are spelled. The three examples/ drafting
batches each invented a divergent notation, and the divergence itself was the largest
surface-notation finding (every PixelWeaver/claudestream/predraw/wavescript/rlsbl/betterclaude gap
note raised it).

The three invented notations were:

| Batch | Representative drafts | Notation shape |
|---|---|---|
| 1 | pixelweaver, claudestream, predraw | `[schema]` header block; `[root]` (`kind = "record"`); `[root.fields.<f>]`; `[types.<Name>]` (`kind = …`); array items `.items`; map values `.values`; constraints `[[root.constraints]]` / `[[types.<N>.constraints]]` |
| 2 | wavescript-score, rlsbl-release-file, rlsbl-config, rlsbl-changelog-entry | bare header keys (`schema =`, `syntax =`); records `[record.<Name>.<field>]` (root is `[record.ROOT.*]`); constraints `[[constraint]]` carrying a `scope` PATH string |
| 3 | betterclaude-contracts, imagine, shared-types, aggregates, enum-baking, orxtra, pgdesign | bare header keys (`name =`, `document_syntax =`, `imports =`); root `[fields.<f>]` (implicit, unnamed); KIND-TYPED named types `[types.<Name>]` (`kind = "record"|"enum"|"scalar"|…`); constraints `[[constraints]]` / `[[types.<N>.constraints]]` |

### 1.1 Selection (in the priority order `DESIGN.md` fixed for this decision)

Priority: (1) explicitness over brevity; (2) every field declaration self-contained and greppable;
(3) named forms, never positional; (4) uniform shape between root records, named types, and imports;
(5) no construct expressible two ways.

- **Winner: batch 3 (betterclaude).** Its KIND-TYPED named-type block (`[types.<Name>]` with an
  explicit category) is the most explicit and self-describing of the three, handles every complex
  kind (record, map, enum, scalar-refinement, union, tuple) with one uniform block shape, and its
  bare-header + `imports = [...]` form is greppable and named-not-positional. It is the spine of the
  pinned surface.
- **Corrected by batch 2's one good insight:** the ROOT is just a named record (`[record.ROOT.*]`).
  Batch 3's implicit unnamed root (`[fields.*]`) is NOT uniform with `[types.<Name>.fields.*]` —
  violating criterion 4. The fix: the root is a named type designated by a top-level
  `root = "<TypeName>"` key, and its fields live under `[types.<TypeName>.fields.*]` exactly like any
  other record. Root records and named types become byte-for-byte the same shape.
- **Batch 2's constraint spelling is REJECTED.** `[[constraint]]` with a `scope = "$.a[*](arm).b"`
  path string is positional-ish, re-parses the path grammar, and lets one constraint float far from
  the record it governs (violating criteria 2 and 3). Batch 1/3's nesting-scoped
  `[[types.<Name>.constraints]]` — the constraint lives directly under the type whose siblings/
  collection it ranges over — wins: self-contained and greppable.
- **Batch 1's `[schema]` block and special-cased `[root]` are REJECTED** in favour of bare header
  keys and root-as-named-type (criteria 4, 5).
- **One key for every type site.** Batch 1/3 spell a named type's category with `kind` and a field's
  with `type`; that is one concept under two keys. The pinned surface uses `type` EVERYWHERE (a named
  type, a field, an array item, a map value, a union arm all carry `type = …`), so a named type and a
  field are the same shape. A scalar-refinement named type is therefore just a type site bound to a
  builtin scalar with refinements (`type = "string"`, `pattern = …`) — there is no separate
  `kind = "scalar"` wrapper (criterion 5).
- **One spelling per element site.** Batch 3 itself was internally inconsistent (scalar array
  elements via an `item_type =` key, record elements via an `.item` subtable). The pinned surface
  uses the SUBTABLE form uniformly: `.item` (array element), `.value` (map value), `.fields.<f>`
  (record field), `.arms.<a>` (union arm), `.elements` (tuple). `.items`/`.values`/`item_type`/`item`
  are all normalized away.

The result is one construct, one spelling; root, named types, fields, items, values, and arms are all
the same self-describing `type = …` block.

## 2. The type site — the one uniform unit

Everything with a type is a TYPE SITE. A type site is a TOML table whose body is EITHER a
**reference** or an **inline definition**, never both:

- REFERENCE: `type = "<name>"` where `<name>` is a builtin scalar, a registered custom scalar, or a
  declared/imported named type. Scalar references may carry refinements (section 3.1) and `required`.
- INLINE DEFINITION: `type = "<complex-kind>"` (`record`, `map`, `array`, `tuple`, `enum`, `literal`,
  `discriminated-union`, `node-kind-union`, `nullable`, `opaque`) plus that kind's member subtables /
  keys (sections 3.2–3.5).

A named type or the root is defined inline in the `[types.*]` namespace and REFERENCED elsewhere by
name; a single-use anonymous shape is defined inline at its site. Inline-vs-named is the one uniform
PLACEMENT dimension shared by every complex kind — it is not two spellings of one construct (an
inline definition has no name; a named type is referenced by name; you name a shape when it is reused,
recursive, or the root). Recursion requires a named type (a site cannot inline itself).

Universal type-site keys:

| Key | Applies to | Meaning |
|---|---|---|
| `type` | every site | the builtin scalar, complex kind, custom scalar, or named-type reference |
| `required` | field sites only | boolean; required vs optional-absent (there are NO defaults — decision 30) |
| `description` | every site | human-readable prose; feeds exported metadata (decision 27) |
| `aliases` | field sites | array of co-valid alternative spellings (decision: aliases); may not target a discriminator |

## 3. Kinds

### 3.1 Scalars and refinements

`type` is one of the builtin scalar names: `string`, `integer`, `float`, `number`, `boolean`, `date`,
`time`, `datetime`; or a registered custom-scalar name (section 8). Refinement keys (each optional,
each attaches to the scalar):

| Key | Scalars | Meaning |
|---|---|---|
| `min` / `max` | integer float number | inclusive numeric bound |
| `exclusive_min` / `exclusive_max` | integer float number | exclusive numeric bound |
| `min_length` / `max_length` | string | code-point length bound (never bytes) |
| `non_empty` | string | boolean; forbids the empty string |
| `regex` | string | anchored RE2 value pattern (RE2-subset, vetted at schema-validation) |
| `datetime_kind` | date time datetime | `"offset"` or `"local"` (datetime only; a mismatch is a hard error) |

A scalar-refinement NAMED type is a `[types.<Name>]` block with `type = <builtin scalar>` plus
refinements — no wrapper kind (e.g. `[types.ModelName]` → `type = "string"`, `regex = "…"`).

### 3.2 Record, map, array, tuple

- **record**: `type = "record"`; each field is a subtable `[<site>.fields.<f>]` (itself a type site).
- **map**: `type = "map"`; `key_pattern = "<anchored RE2>"` (map keys are regex-constrained only — see
  §11 on the rejected `key_type`); `order = "semantic" | "incidental"`; the value site is the single
  subtable `[<site>.value]` (a type site).
- **array**: `type = "array"`; `min_len` / `max_len` optional (mutually exclusive with `tuple`); the
  element site is the single subtable `[<site>.item]` (a type site).
- **tuple**: `type = "tuple"`; `elements = ["<TypeRef>", …]` — an ordered list of element type names
  (builtin, custom-scalar, or named). A complex tuple element must be a named type. `tuple` and
  array-length bounds are mutually exclusive (`STRICTSPEC_SCHEMA_TUPLE_ARRAY_BOUNDS`).

### 3.3 Enums and literals

- **enum**: `type = "enum"`; ONE of:
  - `values = ["a", "b", …]` — an inline literal arm set; or
  - a SOURCED enum: a `[<site>.source]` subtable (`document = "<path>"`, `selector = "<selector>"` —
    grammar in section 7) PLUS a `baked = [...]` array (the arms baked at `gen` time). Sourced arms
    must be string-typed literals; the toolchain hard-errors on staleness (decision 32).
- **literal**: `type = "literal"`; `value = <literal>` (used as discriminator values and constants).

### 3.4 Unions and nullable

- **discriminated-union**: `type = "discriminated-union"`; `discriminator = "<field name>"`; arms are
  subtables `[<site>.arms.<armname>]`, each a type site (an inline record or `type = "<Ref>"`). Each arm
  carries the discriminator as a `literal` field. Arm names are keyed identifiers (named, not
  positional); an arm identifier appears in the path grammar as `(armname)`.
- **node-kind-union**: `type = "node-kind-union"`; arms are subtables `[<site>.arms.<armname>]`, each a
  distinct node kind (scalar vs record vs array). No discriminator; the input node kind selects the arm.
- **nullable**: `type = "nullable"`; the inner type is the subtable `[<site>.inner]` (a type site). T |
  null. Legal only for JSON/JSONL documents; a reachable nullable union against a TOML document is a
  canonical hard error (`STRICTSPEC_SCHEMA_TOML_NULLABLE`).

### 3.5 Opaque leaves

`type = "opaque"`; EXACTLY one stance (decision 29):

- `consumer_check = "<name>"` — a named consumer-native check owns the blob (strictspec never runs it);
- `unchecked = true` PLUS `unchecked_reason = "<why>"` — a declared, justified blind spot.

Omitting the stance is `STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE`; omitting the reason is
`STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON`. An opaque DOMAIN STRING (a string whose interior a named
consumer check validates) is a `string` site carrying `consumer_check = "<name>"`.

## 4. Header and file roles

Every schema and type-definition file opens with BARE top-level keys (before any table header):

| Key | Files | Meaning |
|---|---|---|
| `name` | all | the artifact identifier (ident-shaped) |
| `meta_version` | all | schema-LANGUAGE version (schemas/type files are documents of the meta-schema) |
| `format_version` | all | the value this artifact's documents must carry |
| `document_syntax` | all | `"json"` \| `"toml"` \| `"jsonl"` — the target document syntax |
| `role` | all | `"schema"` \| `"type-definitions"` (explicit discriminator — see §11) |
| `description` | all | prose |
| `root` | schema files | names the `[types.<Name>]` that is the document root |
| `targets` | schema files | codegen targets, e.g. `["python", "go", "ts"]` |
| `safe_integers` | schema files | optional `true`; mandatory when `"ts" ∈ targets` |
| `imports` | files that import | array of `{ file = "<path>", types = ["<T>", …] }` (section 6) |

A `role = "type-definitions"` file declares TYPES ONLY: it carries `[types.*]` blocks, no `root`, no
`targets`, and NO constraints (a constraint in a type file is
`STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT`). A `role = "schema"` file carries a `root` and MAY carry
constraints. `role` is a required, declared literal — the type-file-vs-schema distinction is never
inferred from shape (resolves the shared-types-exercise notation-ambiguity finding).

## 5. Named types, the root, and constraints

- Named types and the root live in the `[types.*]` namespace: `[types.<Name>]` (a type-site body),
  `[types.<Name>.fields.<f>]`, `[types.<Name>.item]`, `[types.<Name>.value]`,
  `[types.<Name>.arms.<a>]`, `[types.<Name>.source]`, `[types.<Name>.inner]`, and deeper. Every header
  is a fully-qualified, greppable path.
- The ROOT is the `[types.<Name>]` named by the top-level `root = "<Name>"`. It is not special-cased.
- CONSTRAINTS attach at the record/type scope that owns the fields or collection they range over:
  `[[types.<Name>.constraints]]` (an array-of-tables). A constraint is never expressed with a floating
  `scope` path.

### 5.1 Constraint bodies

`form = "<vocabulary form>"` plus operand keys, all named (never positional). Intra-document forms:

| `form` | Operand keys |
|---|---|
| `conditional-required` | `field`, `when` |
| `forbidden-when` | `field`, `when` |
| `conditional-value` | `field`, `equals_literal`, `when` |
| `exactly-one-of` / `at-least-one-of` / `co-presence` / `mutual-exclusion` | `fields = [...]` |
| `collections-disjoint` | `left`, `right`, `normalization` |
| `unique-by` | `collection`, `field`, `normalization` |
| `pairwise-distinct` | `collection`, `normalization` |
| `ranges-disjoint` | `collection`, `start`, `length` |
| `ordered-pair` | `less`, `than` |
| `intra-document-references` | `reference`, `resolves_into`, `resolves_by` |

Cross-document forms (each carries a resolver-backed `selection`/`source`):

| `form` | Operand keys |
|---|---|
| `named-reference-must-resolve` | `reference`, `source` |
| `set-coverage` | `collection`, `source` |
| `cross-collection-unique` | `field`, `source` |
| `count-limit` | `selection`, `compare`, `limit` |
| `sum-limit` | `selection`, `sum_field`, `compare`, `limit` |

`compare` is `"<="` or `">="`; `limit` is a bare LITERAL (integer or number) written in the schema —
never an expression, never a computed bound (decision 23). `selection` / `source` name a closed
evidence resolver, e.g. `selection = "documents-in(services/*.toml)"`. `documents-in` globs are
anchored at the MANIFEST ROOT and resolved in LEXICOGRAPHIC order (determinism); a `sum-limit`
selection containing a document that lacks the summed field or whose value is non-numeric is a HARD
ERROR (`STRICTSPEC_CROSS_SUM_FIELD_MISSING`), never a skip-or-zero.

### 5.2 The condition object (`when`) — closed predicate set

Every gated form (`conditional-required`, `forbidden-when`, `conditional-value`) takes a `when`
condition object over a sibling gate field. The condition KINDS are a CLOSED SET of six:

```toml
when = { field = "<f>", predicate = "present" }
when = { field = "<f>", predicate = "absent" }
when = { field = "<f>", predicate = "equals",     value  = <literal> }
when = { field = "<f>", predicate = "not-equals", value  = <literal> }
when = { field = "<f>", predicate = "in",         values = [<literal>, …] }
when = { field = "<f>", predicate = "not-in",     values = [<literal>, …] }
```

`present`/`absent` test field presence; `equals`/`not-equals`/`in`/`not-in` test the WRITTEN value
against literals (no effective/default value — decision 30). NUMERIC COMPARISON predicates (`> k`,
`>= k`, `< k`, `<= k`) are NOT in the set — REJECTED (single-consumer demand, expressible via the
literal-value predicates when the field's domain makes it so; see `DESIGN.md` — Cross-field
vocabulary rejection rationale). `not-equals`/`not-in` give the negative-polarity conditions directly,
so no complement enumeration is needed.

`conditional-value` asserts: when `when` holds, `field` (when present) must equal `equals_literal`.

## 6. Imports

`imports = [ { file = "<relative path>", types = ["<T>", …] }, … ]` — a top-level array. Each entry
names ONE type-definition file and the subset of its named types this artifact imports. Types only; no
cross-file constraints; no transitive imports (a type-definition file that itself declares `imports`
is `STRICTSPEC_IMPORT_TRANSITIVE`). Importing across incompatible `meta_version` values is a metagate
error on the imported file. An imported type is referenced exactly like a local named type
(`type = "<T>"`).

## 7. Enum-sourcing selector grammar (pinned)

A sourced enum's `[<site>.source]` block carries `selector = "<selector>"`. The selector is a
RESTRICTED projection path over the source document, EBNF:

```ebnf
selector   = step , { step } ;
step        = key-step | array-step ;
key-step    = "." , ident | ident ;      (* a leading dot is optional on the first step *)
array-step  = "[" , "]" ;                 (* array traversal — flatten every element *)
ident       = ( letter | "_" ) , { letter | digit | "_" | "-" } ;
```

Only key steps and `[]` array-flatten steps are legal. There are NO wildcards over keys, NO index
selection, and NO filtering/predicates. Example: `sounds[].name` projects the `name` of every element
of the top-level `sounds` array. The selector must resolve to a flat sequence of string-typed leaves;
anything else is `STRICTSPEC_ENUMSRC_BAD_SELECTOR` (a selector outside this grammar, or one that does
not resolve within the source) or `STRICTSPEC_ENUMSRC_SOURCE_NOT_STRINGS` (a resolved leaf is not a
string). This grammar IS the accept/reject boundary for `STRICTSPEC_ENUMSRC_BAD_SELECTOR`.

## 8. Custom-scalar registration (manifest side)

Custom scalars are registered in the consumer manifest `strictspec.toml` as an array-of-tables (their
semantics are in `appendix-custom-scalars.md`):

```toml
[[scalars]]
name = "identifier"
base = "string"                       # a builtin scalar lexeme class it refines
lexeme_rule = "^[A-Za-z_][A-Za-z0-9_]*$"   # anchored RE2 over the source lexeme
length = { max = 63 }                 # optional: { min?, max?, non_empty? }
bindings = { go = "string", python = "str", ts = "string" }   # one per declared target
rendering = { inherits = "string" }   # declares base-class rendering inheritance (§2 of custom-scalars)
```

`name` is ident-shaped and unique within the manifest; a schema referencing an unregistered name is
`STRICTSPEC_SCALAR_UNKNOWN`; a declared target without a binding is `STRICTSPEC_SCALAR_NO_BINDING`. The
manifest ALSO declares `[[stores]]` and `[[channels]]` for boundary-checkpoint generation and the
manifest's own header keys; those follow the same type-site/header conventions above.

## 9. Migration files

A migration file is a document of a toolchain-shipped schema, gated and migrated like any document:

```toml
[migration]
schema = "AgentDefinition"
from_format_version = 1
to_format_version = 2
migration_set = "agent-budget"
description = "…"

[[ops]]
op = "rename_field"                   # one of the closed 13-op set
from = "$.budget.max_cost_usd"        # op targets addressed via the read-side path grammar
to = "$.budget.cost_thresholds"

[[ops]]
op = "wrap_in_array"
path = "$.budget.cost_thresholds"

# The DOWN direction is AUTHOR-SUPPLIED, never engine-derived (decision: migration reversibility).
down = "partial"                      # "total" | "partial" | "irreversible" — the declared taxonomy
down_partial_reason = "unwrap_singleton is a hard error on a multi-element cost_thresholds."

[[down_ops]]                          # author-supplied inverse ops, executed for a down-migration
op = "unwrap_singleton"
path = "$.budget.cost_thresholds"

[[down_ops]]
op = "rename_field"
from = "$.budget.cost_thresholds"
to = "$.budget.max_cost_usd"
```

The `down` taxonomy (`total`/`partial`/`irreversible`) is DECLARED by the author; `[[down_ops]]` are
AUTHORED explicitly; the engine never derives down ops. `strictspec diff`'s DOWN-TAXONOMY VERIFICATION
checks the DECLARATION against the corpus (a mis-declared taxonomy is
`STRICTSPEC_DIFF_TAXONOMY_MISDECLARED`). Op keys per op: `from`/`to` (rename/move), `path` (positional
ops), `path`/`value` (add/set — `add_field`/`set_value` address the target field via the SAME full
read-side `path` grammar the positional ops use, and carry the injected literal in `value`),
`where = { field = …, predicate = …, value/values = … }` plus a `field` sub-field target for
`set_value_where` (the `set_value_where`/`remove_where` predicate is restricted to the §5.2 closed
condition set — predicates test equality and presence only).

## 10. Path grammar reuse

Op targets, constraint `scope`-free operands, and diagnostic paths all use the ONE path grammar pinned
in `appendix-rendering.md` Part B (`$`, `.key`, `[i]`, `["mapkey"]`, `(arm)`, JSONL `@Lline:byte`).
The surface never re-defines paths.

## 11. Rejected surface features (rationale)

- **`key_type = "<EnumType>"` (enum-constrained map keys).** REJECTED — single consumer (betterclaude
  tier→display), and a working `key_pattern` regex expresses it today (the enum can also be baked into
  the key regex via enum sourcing). Map keys are regex-constrained only. Revisit on recurrence.
- **`[[constraint]]` + `scope` path string (batch 2).** REJECTED in favour of nesting-scoped
  `[[types.<Name>.constraints]]` (criteria 2, 3).
- **`[schema]` header block and special-cased `[root]` (batch 1).** REJECTED in favour of bare header
  keys and root-as-a-named-type (criteria 4, 5).
- **`kind` as a separate key, and `item_type`/`.items`/`.values`/`item` element spellings.** REJECTED
  in favour of the single `type` key and the uniform `.item`/`.value`/`.fields`/`.arms`/`.elements`
  subtable sites (criterion 5).

## Amendment log

- 2026-07-27 — §9: PINNED the `add_field`/`set_value` op-key convention to `path`/`value`. Earlier
  prose read `field`/`value` for add/set, but the shipped engine (and the migrable donor it mirrors)
  addresses the add/set target via the SAME full read-side `path` grammar as the positional ops —
  `field` is the sub-field selector for `set_value_where` alone. This closes an unpinned-convention gap
  between the appendix and the implementation; the corpus-locking fixture is the
  `migration-add-set` toolchain fixture.

## Cross-references

- The constructs each site declares, and their accepted-set semantics: `appendix-semantics.md`.
- Value/path rendering the surface reuses: `appendix-rendering.md`.
- Error codes for every surface violation: `appendix-error-codes.md`.
- Custom-scalar semantics behind `[[scalars]]`: `appendix-custom-scalars.md`.
- The meta-schema, self-hosting, and the built-in surface: `DESIGN.md` (Meta-schema).
- The constitution: `DESIGN.md`.
</content>
</invoke>
