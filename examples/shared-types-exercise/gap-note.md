# Gap note — shared-types-exercise (construct exercise: type-definition files, decision 21)

Construct-only exercise drafted from the spec itself (examples/DESIGN.md drafts 13–15). The
deliverable is this gap note. It exercises the ONE sanctioned modularity edge — named-type
imports from a dedicated type-definition file — including its two rejection cases and the
`meta_version`/`format_version` bump interaction.

## Files

- `types-geometry.toml` — the single type-definition file. Declares `PositiveInt` and
  `ColorName` (scalar-refinement types), `Point` (record), `Shape` (discriminated union).
- `schema-canvas.toml` — imports `Point`, `Shape`, `ColorName`.
- `schema-palette.toml` — imports `ColorName`, `PositiveInt` (a DIFFERENT subset of the same
  file; the intended reuse pattern).
- `INVALID-types-with-constraint.toml` — rejection fixture: a constraint in a type file.
- `INVALID-types-transitive.toml` — rejection fixture: a type file importing another file.
- `canvas.valid.json` / `palette.valid.json` — valid documents.
- `canvas.invalid.json` — an invalid document (diagnostics below).

## Allowed (CLEAN)

- **Named record / union / scalar-refinement types in a type file.** All three kinds sit
  cleanly in `types-geometry.toml`. Scalar-refinement types (`PositiveInt` = `integer`
  exclusive_min 0; `ColorName` = `string` + anchored RE2 pattern) are ordinary named types with
  a `base` scalar plus value constraints — semantics are "identical to inlining the imported
  type definitions" (appendix-semantics 3.20), so importing has zero semantic effect beyond name
  resolution.
- **Two importers, disjoint subsets, one shared type.** `ColorName` is imported by both schemas;
  `PositiveInt` and `Point`/`Shape` are imported by only one each. No import needs to name the
  file's whole type set — the `types = [...]` list selects.
- **Imported types compose with local constructs.** `schema-palette.toml` uses imported
  `ColorName`/`PositiveInt` as a typed-map value-record's fields and mirrors `ColorName`'s
  grammar in the map's `key_pattern`; `schema-canvas.toml` uses the imported `Shape` union as an
  array `item_type`. Composition is transparent because import is inlining.

### Expected diagnostics — `canvas.invalid.json` (ordered, per traversal rule, item 6)

1. `STRICTSPEC_SCALAR_LEXEME` — wait: `ColorName` is a scalar-refinement type, not a manifest
   custom scalar, so its failure is a value-regex failure, not a `SCALAR_*` code:
   `STRICTSPEC_VALUE_STRING_REGEX` · path `$.background` · slots `{actual: "Midnight", pattern:
   "^[a-z][a-z0-9-]{0,31}$"}` (leading capital fails the anchored pattern).
2. `STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE` · path `$.shapes[0].radius` · slots `{actual: 0,
   limit: 0}` (`PositiveInt` requires `> 0`).
3. `STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN` · path `$.shapes[1].kind` · slots `{got: "hexagon",
   expected: ["circle", "rect"], suggestion: ""}` (no arm; non-matching arms never validated,
   Union diagnostics).

## Forbidden — the two meta-schema rejections (INVALID companions)

- **Cross-file constraint** (`INVALID-types-with-constraint.toml`): the file declares an
  `ordered-pair` constraint on a record type. Type-definition files declare TYPES ONLY.
  Expected: `STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT` · slot `{file:
  "geometry-types-bad-constraint.toml"}` (appendix-error-codes 6). Rationale (DESIGN.md
  Unknown-key policy / decision 21): constraints are properties of a SCHEMA's accepted set, not
  of a reusable type; allowing them would smuggle cross-file coupling into the "types only" edge.
- **Transitive import** (`INVALID-types-transitive.toml`): the file itself declares `imports`.
  Type files are leaves in the import graph. Expected: `STRICTSPEC_IMPORT_TRANSITIVE` · slot
  `{file: "geometry-types-transitive.toml"}`. Rationale: one hop only keeps name resolution
  decidable and the dependency surface flat — no diamond resolution, no import cycles.

Two more import errors exist in the catalogue and are worth noting as the remaining rejection
surface, though not exercised with a dedicated file here: `STRICTSPEC_IMPORT_MISSING_TYPE_FILE`
(a named file that does not exist) and `STRICTSPEC_IMPORT_UNKNOWN_TYPE` (a `types = [...]` entry
naming a type the file does not define).

## The versioning story (the bump-rule interaction — walked)

A type-definition file carries its OWN `meta_version` and `format_version` and is gated/migrated
like any document (appendix-semantics 3.20 interaction note). Two independent version axes meet
here, and keeping them distinct is the whole point of the `meta_version` vs `format_version`
split (DESIGN.md decision 13):

1. **`meta_version` (schema-LANGUAGE version).** If `types-geometry.toml` and an importing
   schema disagree on `meta_version`, the import is a metagate error on the imported file
   (appendix-semantics 3.20: "importing across incompatible `meta_version` values is a metagate
   error"). Concretely this surfaces as `STRICTSPEC_METAGATE_UNSUPPORTED` against the type file.
   Meaning: you cannot import a type file written for a different strictspec language version;
   run `strictspec migrate` to bring both to the same `meta_version` first. This is what makes
   type files safe to share across a fleet — the language version is checked, never assumed.

2. **`format_version` (the value a schema's DOCUMENTS carry) and the BUMP RULE.** Because import
   is inlining (zero semantic effect), a change to a type in the type file propagates into EVERY
   importing schema's accepted set as if the change had been made inline. So the decision-13 bump
   rule applies at each importer, judged by whether the importer's accepted set shrank:

   - **Widening a shared type** (e.g. loosening `ColorName` to allow 64 code points, or ADDING an
     arm to `Shape`) accepts strictly more documents at every importer ⇒ NO `format_version` bump
     obligated anywhere (appendix-semantics 3.1 / 3.5, "adding an optional field / adding an arm
     WIDENS").
   - **Narrowing a shared type** (e.g. tightening `PositiveInt` to `exclusive_min = 1` — no-op —
     or to `min = 10`; REMOVING the `rect` arm of `Shape`; making `Point.x` stricter) shrinks the
     accepted set at every importer that uses the changed type ⇒ each such importer MUST bump its
     `format_version`. The type file's own `format_version` is not what documents carry; the
     obligation lands on the importing SCHEMAS whose document sets shrank.
   - **The enforcement mechanism is the same one used everywhere:** `strictspec diff`'s
     SAME-VERSION FLIP-SCAN (decision 25). Replay each importer's corpus against the old and new
     resolved (import-inlined) schema at the SAME `format_version`; any document flipping
     valid→invalid is an un-bumped narrowing and a HARD ERROR
     (`STRICTSPEC_DIFF_NARROWING_UNBUMPED`). A shared-type narrowing that forgot to bump a
     downstream importer is caught mechanically, per importer.

   Subtlety worth recording: a single edit to the type file can obligate a bump at SOME importers
   and not others — only those importing the changed type, and only if their document set
   actually shrank. The flip-scan is run per importer against its own corpus, so this asymmetry
   is handled correctly by construction; there is no "bump everything that touches the file"
   over-approximation.

No new construct or spec change is needed for any of the above — the existing bump rule, the
metagate, and the same-version flip-scan compose to cover type-file evolution exactly.

## Awkward / inexpressible

- **Nothing inexpressible.** The three rejection-worthy shapes (cross-file constraint,
  transitive import, cross-`meta_version` import) each have a dedicated code, and the bump
  interaction is fully covered by existing machinery.
- **Minor notation ambiguity (not a spec gap):** a type-definition file and a schema share the
  same header (`name`/`meta_version`/`format_version`/`description`). This draft distinguishes
  them structurally (a type file has `[types.*]` and no `root`/`[fields.*]`; a schema has a root
  record). That is a fine convention, but the meta-schema should PIN it explicitly — e.g. a
  required `role = "type-definitions"` vs `role = "schema"` discriminator — so the distinction is
  a declared literal rather than an inferred-from-shape heuristic. Filed as a notation
  observation, not a construct gap; either resolution (structural inference or explicit
  discriminator) is expressible today.

## Verdict

CLEAN — RESOLVED (Phase 3.3).

## RESOLUTION (Phase 3.3)

- **Notation observation (schema-vs-type-file discriminator)** — ADOPTED. The meta-schema now pins
  an explicit `role = "schema" | "type-definitions"` header key; the distinction is a declared
  literal, never inferred from shape (`spec/appendix-surface-syntax.md` §4). A `type-definitions`
  file carries `[types.*]` only — no `root`, no `targets`, no constraints.
- Everything else (all three shareable kinds, the two rejection cases, the metagate and bump-rule
  interactions) came back CLEAN and is confirmed. Draft normalized to the pinned surface.

VERDICT: RESOLVED (Phase 3.3).
