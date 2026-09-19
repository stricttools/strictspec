# Gap note — predraw (scene, config)

Paper translation of `predraw/predraw/schema/scene.schema.json` and `config.schema.json`.
Documents are JSON.

## What the language expressed cleanly

- **Recursion + depth limit.** `Element` is a discriminated union; `groupElement.elements`
  (and `defs` map values) are arrays of `Element`, so the type is directly self-referential.
  strictspec's recursive named references with the pinned max validation depth cover it exactly
  (`STRICTSPEC_DEPTH_EXCEEDED` fires before stack exhaustion). No workaround needed.
- **Node-kind union — the motivating case (decision 15).** `fillOrGradient` = a color **string**
  (scalar arm) or a **Gradient** object (record arm). Distinct node kinds, no discriminator,
  deterministic dispatch: `FillOrGradient` with `kind = "node-kind-union"`, `arms = ["string",
  "Gradient"]`. This is the exact field the node-kind-union construct exists for; a non-matching
  kind is `STRICTSPEC_UNION_NODE_KIND`.
- **Tuples.** `transform.translate` / `scale` are `minItems:2, maxItems:2` arrays of number ->
  fixed-arity **tuples** (`elements = [{number}, {number}]`), NOT array-length bounds. The two
  are mutually exclusive by construct (`STRICTSPEC_SCHEMA_TUPLE_ARRAY_BOUNDS`); modeling them as
  tuples is the correct choice and sidesteps that exclusivity cleanly.
- **NUMBER scalar (47 of 48 numeric fields).** Every dimensional/positional/opacity field
  (`width`, `height`, `x`, `y`, `opacity`, `offset`, `angle`, `gap`, font `size`, …) is `number`
  (accepts int or float lexeme, binds float64). The lone integer field is `font.weight`
  (100-900). `quality` in config is also integer. The unrepresentable-lexeme hard error is
  demonstrated in `samples/scene.invalid-unrepresentable-number.json`.
- **`enum`, `const`, numeric ranges** all map 1:1 (`anchor`/`axis`/`mode`/`format`/gradient
  `type` -> enum; the element `type`/`action` consts -> `literal`; `minimum`/`maximum` -> `min`/
  `max`).
- **`anyOf(below/above/left/right)` on placeStep -> `at-least-one-of`** intra-document form,
  directly.

## Findings

### FINDING 1 — meta-schema surface syntax still unpinned (same as sibling drafts)
See the PixelWeaver gap note, Finding 1. The concrete TOML spelling of records/maps/unions/
tuples/aliases/named-types is invented per draft; the specification pins the language but not the authoring
surface. Recommend a normative surface-syntax appendix (or "the shipped built-in meta-schema is
the pinned surface, by example").

### FINDING 2 — predraw has NO `format_version` today (net-new gate)
The source scene/config documents carry no version field of any kind. Adoption adds
`format_version` as a **net-new** integer gate (value 1 here). Per the bootstrap contract
(decision 13/34): a one-time per-consumer conversion script must STAMP `format_version: 1` into
every existing scene/config file (stamp, never reshape, refuse ambiguous inputs) before
strictspec first reads them. Until stamped, every existing document hard-errors at the gate
(`STRICTSPEC_GATE_ABSENT`). This is expected and correct — recording it as the adoption
precondition, not a language gap.

### FINDING 3 — the source `default` keys are inexpressible by design (decision 30) — and must be removed
The source schema uses `default` extensively (`transform.translate` default `[0,0]`,
`font.weight` default `400`, `opacity` default `1.0`, `anchor` default `"start"`, `x`/`y`/`gap`
default `0`, …). strictspec has **no defaults**: a `default` key in a schema is a dedicated
meta-schema hard error (`STRICTSPEC_SCHEMA_DEFAULT_KEY`, decision 30). Every such field becomes
**optional-absent**; the default value moves into predraw's own rendering code, visibly. This is
a deliberate exclusion, not a gap — but it is the single largest mechanical change in the
translation and the adoption note must call it out: predraw's renderer must apply the legacy
defaults itself when a field is absent.

### FINDING 4 — the `Element` union is not uniformly discriminated (useElement makes `type` optional)
Five of six element arms require `type` (a `const`); **`useElement` makes `type` optional** —
its `required` is `["use"]`, and a use element is identified by the presence of `use`, with
`type` defaulting to `"use"`. strictspec discriminated unions require the discriminator field to
be **present and literal** (a missing discriminator is `STRICTSPEC_UNION_DISCRIMINATOR_MISSING`;
node-kind union does not apply — all arms are records). The clean translation makes `type`
**required** on `useElement` (spelled `"use"`), which is a **narrowing** (it rejects source
documents that omitted `type` on a use element). Disposition: acceptable narrowing, declared in
`scene.toml`; it obligates a `format_version` bump only if it were an edit to an already-shipped
strictspec schema (here it is the initial translation). Alternative — inferring the arm from the
presence of `use` — is not a construct the language offers, and adding "discriminate by which
field is present" would be a new union form (vocabulary-evolution conversation, not needed for
one field in one consumer).

### FINDING 5 — aliases: 8 co-valid pairs, and both-present becomes a HARD ERROR (behavior change)
The 8 alias pairs (camelCase canonical, snake_case alias): `strokeWidth`/`stroke_width`,
`strokeDasharray`/`stroke_dasharray`, `strokeLinecap`/`stroke_linecap`, `strokeLinejoin`/
`stroke_linejoin`, `strokeOpacity`/`stroke_opacity`, `letterSpacing`/`letter_spacing`,
`charStyles`/`char_styles`, and `elements`/`children` (group). strictspec `alias` models these
as permanently co-valid spellings with **one canonical spelling on write**. Two consequences:
  - **Both-present is a hard error** (`STRICTSPEC_ALIAS_BOTH_PRESENT`). In the source JSON
    Schema these are *separate optional properties*, so a document may carry BOTH `strokeWidth`
    and `stroke_width` (predraw's code picks one by precedence). Under strictspec that document
    is rejected. This is a real, intended behavior change — the ambiguity strictspec forbids.
  - **Write-side canonicalization** rewrites `children`->`elements`, `stroke_width`->
    `strokeWidth`, etc., preserving attached comments. Demonstrated by
    `samples/scene.alias-canonicalize.json` (valid input using snake_case + `children`;
    canonical output uses camelCase + `elements`).

### FINDING 6 — no record composition / field-block mixins (minor duplication)
The stroke block (`stroke`, `strokeWidth`, `strokeDasharray`, `strokeLinecap`, `strokeLinejoin`,
`strokeOpacity` + their aliases) repeats across all five drawable element arms — because
strictspec named types are referenced as **field types**, not spread as **field blocks** into a
record (there is no mixin/composition beyond named-type import, which is also type-level).
The source JSON Schema duplicates the same block, so this is not a regression, but it is the one
spot where the language forces repetition the author might want to factor. Not proposing a mixin
construct (it edges toward composition, deliberately excluded); recording it as an observed
ergonomic cost.

## Expected diagnostics (sample documents)

- `samples/scene.invalid-alias-both.json` — `STRICTSPEC_ALIAS_BOTH_PRESENT` · path
  `$.elements[0](rectElement)` · alias `stroke_width` · canonical `strokeWidth`.
- `samples/scene.invalid-unrepresentable-number.json` — `STRICTSPEC_NUM_UNREPRESENTABLE` · path
  `$.width` · actual `9007199254740993` (integer lexeme = 2^53+1; the `number` scalar refuses a
  lexeme float64 cannot represent exactly).
- `samples/scene.alias-canonicalize.json` — VALID; write-side output canonicalizes `children`->
  `elements` and `stroke_width`/`stroke_opacity`-> `strokeWidth`/`strokeOpacity`, comments
  preserved (within-backend byte fixpoint on the untouched values).

## Verdict

FINDINGS: 6 — RESOLVED (Phase 3.3).

## RESOLUTION (Phase 3.3)

- **F1 (surface syntax unpinned)** — ADOPTED. Pinned in `.stricttools/docs/appendix-surface-syntax.md`. Draft
  normalized.
- **F2 (net-new format_version gate)** — BOUNDARY-CONFIRMED. Bootstrap contract (decision 13/34);
  adoption precondition, not a gap.
- **F3 (source `default` keys removed)** — BOUNDARY-CONFIRMED. Deliberate exclusion (decision 30);
  defaults move to consumer rendering code.
- **F4 (non-uniform discriminator, `useElement`)** — BOUNDARY-CONFIRMED. Acceptable narrowing
  (`type` made required); discriminate-by-presence is NOT added (single field, single consumer).
- **F5 (alias both-present hard error)** — BOUNDARY-CONFIRMED. Intended behavior change; the
  ambiguity strictspec forbids.
- **F6 (no record composition / mixins)** — BOUNDARY-CONFIRMED. Composition beyond named-type
  import is deliberately excluded; ergonomic cost recorded.

VERDICT: RESOLVED (Phase 3.3).
