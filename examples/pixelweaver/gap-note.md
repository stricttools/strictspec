# Gap note — PixelWeaver (constraint-manifest, part-manifest, character-preview)

Paper translation of the three real JSON Schemas under `PixelWeaver/schemas/`.
Documents are JSON, so nullable unions (`T | null`) are legal throughout.

## What the language expressed cleanly

- **All four cross-field x- keywords map to existing constraint-vocabulary forms**,
  exactly as the drafting obligation predicted:

  | Source x- keyword | strictspec form | Where used |
  |---|---|---|
  | `x-unique-field: "<f>"` | `unique-by` (field `<f>`, normalization `none`) | classes.id, parts.name, functions.id |
  | `x-pairwise-distinct: "case-insensitive"` | `pairwise-distinct` (normalization `case-fold`) | palette |
  | `x-range-nonoverlap: {start,len,boundsFrom}` | `ranges-disjoint` (half-open) | classes |
  | `x-less-than-sibling: {less,than}` | `ordered-pair` | BlinkFunction onMs < periodMs |

  The normalization token maps cleanly: JSON Schema's `"case-insensitive"` is strictspec's
  `case-fold` (the closed normalization set is `none`/`case-fold`/`trim`, primitives item 10).

- **Discriminated union** (`OscFunction` = oneOf[SineFunction, BlinkFunction], discriminated by
  the `type` const) is a textbook discriminated-union: each arm carries a `literal` `type` field
  (`"sine"`/`"blink"`) and the union declares `discriminator = "type"`. Union diagnostics
  (unknown discriminator, no arm validated) apply directly.

- **Nullable unions** (`variantIndex`, `paletteClassId`, `layerId`, `StripSet.indexed`,
  `Part.paletteClassId`) — legal because `document_syntax = "json"`. Modeled as `nullable = true`
  on the field. (If PixelWeaver ever emitted TOML, every one of these would become a meta-schema
  hard error `STRICTSPEC_SCHEMA_TOML_NULLABLE` and would have to be remodeled as optional-absent.)

- **Typed maps** — `character-preview.parts` (partKey -> PartSelection) and the nested weight
  maps (`variantWeights`: partKey -> stringified-index -> number) are typed maps, including the
  two-level nesting. `additionalProperties: false` records vs `additionalProperties: {schema}`
  maps is the record-vs-map distinction the construct set draws explicitly.

- **NUMBER scalar** — the seven fractional fields (`periodMs`/`phaseMs`/`onMs` on the two
  oscillators, and the two weight-map value positions) are `number` (accept int or float lexeme,
  bind float64). The integer-only fields (`seed`, `rollIndex`, `amplitudePx`, `variantIndex`)
  stay `integer`. `const` -> `literal`; JSON Schema `enum` -> `enum`.

- **`const` / bounded ints** — `alphaPolicy: const "binary"` -> `literal`; `seed` max
  `4294967295` -> `max` (well inside int64; no `safe_integers` needed unless a TS target is
  declared, which would then be mandatory).

## Findings

### FINDING 1 — the meta-schema TOML *surface syntax* is not pinned anywhere in the specification
`DESIGN.md` says schemas are authored in TOML and the notation core is "adapted from incantino
(header + per-field type/required/values/description)", but no appendix pins the actual TOML
grammar: how records/maps/unions/constraints/aliases/named-types are spelled. Every examples/
draft must invent one, and independent drafts risk diverging (this one, its siblings, and the
eventual `strictspec init` scaffold could each pick different spellings). Recommend a normative
"meta-schema surface syntax" appendix (or an explicit statement that the built-in meta-schema
`.toml` shipped with the toolchain *is* the pinned surface, by example). This is a real gap the
gap-note process should feed back — not a blocker for the constructs themselves.

### FINDING 2 — `x-lock` / `x-locks` do not exist as keywords; index-locks are structural
The drafting brief asked for a disposition of `x-lock`/`x-locks`. **They are not keywords in the
corpus.** `grep` across `PixelWeaver/` finds no `x-lock`/`x-locks` in any schema or in the
generator; the generator's docstring recognizes exactly three x- keywords
(`x-pairwise-distinct`, `x-unique-field`, `x-range-nonoverlap`). The *index-lock* concept is
modeled **structurally** as the `pairings` array of `Pairing` records (`partA`/`partB`) in
part-manifest — plain records, no special keyword. **Disposition: not a vocabulary form, not a
consumer-native check, not ignorable metadata — it is ordinary structural data** (an array of
two-string records), fully expressed by a closed record + array. The only semantic rule attached
to it, `partA < partB`, is an `ordered-pair` constraint (added in the draft; see Finding 4).

### FINDING 3 — `ranges-disjoint` covers non-overlap but NOT the in-bounds check (ambiguous in spec)
`x-range-nonoverlap` carries a `boundsFrom: "palette"` that does two things in the generator:
(a) ranges `[start, start+len)` must not overlap, and (b) each range must **fit within**
`len(palette)` (in-bounds). strictspec's `ranges-disjoint` (semantics 3.24; error
`STRICTSPEC_INTRA_RANGES_DISJOINT`) is defined only as "half-open ranges do not overlap … missing/
invalid bound source is a hard error." The **in-bounds-against-a-sibling-array-length** half of
`x-range-nonoverlap` is not clearly in scope. Two readings:
  - the `bounds_from` argument means only "where the numeric bound values come from" (source of
    start/len), and in-bounds is NOT checked; or
  - `ranges-disjoint` is intended to also enforce `start+len <= len(bounds_from)`.
The spec phrase "missing/invalid bounds source = hard error" reads like the former (it is about a
*schema-authoring* error, not a per-document range-past-end error). **Recommend the spec state
explicitly whether `ranges-disjoint` includes an in-bounds leg.** If it does not, PixelWeaver's
in-bounds requirement (`start+len <= len(palette)`) falls to a consumer-native check — a real
expressiveness gap for this corpus. Drafted here with `bounds_from = "palette"` under the
optimistic reading; flagged for resolution.

### FINDING 4 — two source rules live only in prose / anyOf, not in an enforced keyword
- `Part.zOrder` "unique across parts" — description-only in the source, **no** `x-unique-field`,
  and the generator does not enforce it. It IS expressible as `unique-by` and is added as a real
  strictspec check in `part-manifest.toml`. Net effect: strictspec strengthens the schema (a
  narrowing relative to today's generator behavior — would be a bump if this were an edit to an
  already-shipped strictspec schema).
- `Pairing.partA < partB` "lexicographically" — description-only; expressible as `ordered-pair`,
  added as a real check.
Both are *widening of enforcement* the source only documented. Worth noting because it changes
the accepted set vs the current generator (which accepts documents violating these prose rules).

### FINDING 5 — cross-record conditional (`blinkMode`) is genuinely inexpressible declaratively
`OscAssignment.blinkMode` is "required iff the referenced function is a blink; forbidden for
sine" (source: "validated at command time"). This is a conditional keyed on the **discriminator
of a different record** resolved through `functionId` -> `functions[].type`. strictspec's
`conditional-required`/`forbidden-when` test "presence/equality only" **within the containing
record** (semantics 3.24), and intra-document references resolve existence, not a resolved
record's field value. There is no intra-document form that says "field X required-iff the record
referenced by sibling Y has discriminator == Z." **Disposition: consumer-native check** over the
typed values (exactly where PixelWeaver puts it today — "at command time"). This is consistent
with decision 23's bespoke tail and is NOT a gap to close (it is the tail working as designed),
but it is worth recording that the "required iff referenced-record-kind" shape recurs (predraw
has analogues) — if it recurs across enough consumers it is a vocabulary-evolution conversation,
not an escape hatch.
  - Same disposition for `PartSelection.variantIndex` null being "legal only for optional parts"
    (also "validated at command time"): needs the part registry, so consumer-native.

### FINDING 6 — `character-preview` is not wired into the current generator (expected)
The generator (`scripts/generate-manifest-types.py`) config processes constraint-manifest and
part-manifest only; `x-less-than-sibling` (used only by character-preview) is not in its
recognized-keyword set. This matches character-preview being "the future acceptance-test schema."
The translation here is complete and standalone, ready to be the acceptance-test source.

## Expected diagnostics (sample documents)

`samples/constraint-manifest.invalid-overlap.json` — three phase-2 violations, emitted in
check-declaration order (all attach at the root record):
1. `STRICTSPEC_INTRA_PAIRWISE_DISTINCT` · path `$.palette` · value `"#ff0000"` · normalization `case-fold`
2. `STRICTSPEC_INTRA_UNIQUE_BY` · path `$.classes` · value `"skin"` · field `id` · normalization `none`
3. `STRICTSPEC_INTRA_RANGES_DISJOINT` · path `$.classes` · value `[1, 3)` intersects `[0, 2)`

`samples/character-preview.invalid-union-and-order.json` — phase 1 before phase 2:
1. `STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN` · path `$.oscillators.functions[0]` · got `"wobble"` · expected `["sine", "blink"]` (no arm validated for functions[0])
2. `STRICTSPEC_INTRA_ORDERED_PAIR` · path `$.oscillators.functions[1]` · actual `onMs` · value `periodMs` (onMs 200.0 must be < periodMs 100.0)

## Verdict

FINDINGS: 6 — RESOLVED (Phase 3.3).

## RESOLUTION (Phase 3.3)

- **F1 (surface syntax unpinned)** — ADOPTED. The concrete TOML surface is now pinned in
  `.stricttools/docs/appendix-surface-syntax.md` (batch-3 kind-typed named types + root-as-named-type; the
  single pinned spelling). This draft is normalized to it.
- **F2 (x-lock is structural, not a keyword)** — BOUNDARY-CONFIRMED. Ordinary structural data
  (a closed record + array); the only rule on it (`partA < partB`) is `ordered-pair`. No
  vocabulary form.
- **F3 (ranges-disjoint in-bounds ambiguity)** — ADOPTED/pinned. `ranges-disjoint` is pinned as:
  each range FIRST well-formed per ordered-pair (start < end), THEN half-open disjointness. The
  in-bounds-against-a-sibling-array-length leg (`start+len <= len(palette)`) is NOT part of the
  form — it is consumer-native (`.stricttools/docs/DESIGN.md` ranges-disjoint clarification;
  `appendix-semantics.md` 3.24). The draft's `bounds_from` operand is dropped in normalization.
- **F4 (prose-vs-enforced widening)** — BOUNDARY-CONFIRMED. strictspec strengthens the schema
  (unique-by on zOrder, ordered-pair on pairings); a narrowing vs the current generator, recorded.
- **F5 (cross-record conditional blinkMode)** — REJECTED (consumer-native). Reference-target /
  discriminator-of-referenced-record predicates are in the rejection list
  (`.stricttools/docs/DESIGN.md` — vocabulary rejection rationale); the bespoke tail, as designed.
- **F6 (character-preview wiring)** — BOUNDARY-CONFIRMED. Observation only.

VERDICT: RESOLVED (Phase 3.3).
