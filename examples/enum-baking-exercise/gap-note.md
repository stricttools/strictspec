# Gap note — enum-baking-exercise (construct exercise: enum arms sourced from a document, decision 32)

Construct-only exercise drafted from the spec (examples/DESIGN.md draft 14). Deliverable is this
gap note. Exercises the sanctioned data→schema dependency edge: an enum whose arms are SOURCED
from a named document and BAKED into the generated code, with toolchain-enforced freshness.

## Files

- `sounds.registry.json` — the source-vocabulary document (the "sound-name registry").
- `schema-drum-pattern.toml` — the consuming schema; `$.hits[].sound` is a sourced enum baked
  from `sounds[].name`.
- `baked-result-sketch.md` — what `gen` bakes.
- `pattern.valid.json` / `pattern.invalid.json` — documents.

## Clean

- **Declaring a sourced enum.** `type = "enum"` + a `source = { document, selector }` block +
  the `baked = [...]` list expresses the construct with no strain. The accepted set is exactly
  `baked` (appendix-semantics 3.21: "the accepted set is a function of the schema text, not of
  the live source"), so runtime validation never touches the source document — the edge is
  build-time only. Confirmed: `pattern.valid.json` passes; `pattern.invalid.json`'s `"cowbell"`
  is a plain enum miss.
- **Baked enum is indistinguishable from a literal enum at runtime.** Same accepted set, same
  code, same diagnostic — see `baked-result-sketch.md`.

### Expected diagnostics — `pattern.invalid.json`

1. `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` · path `$.hits[1].sound` · slots `{got: "cowbell", expected:
   ["kick", "snare", "hat", "clap", "tom"], suggestion: ""}` (no arm within did-you-mean
   threshold of "cowbell"; "clap"/"tom" are too far — tie-break/threshold per appendix-rendering
   Part C, so `suggestion` renders empty).

## The lifecycle walkthrough (widening vs narrowing vs staleness)

### Vocabulary ADDITION → widening → NO bump

Add `{ "name": "rim", "hz": 400 }` to `sounds.registry.json`, then re-`gen`. The re-baked arm
set becomes `["kick","snare","hat","clap","tom","rim"]`. Adding an enum member WIDENS the
accepted set (appendix-semantics 3.13, "adding a member WIDENS"; 3.21, "adding an arm WIDENS").
Per the decision-13 bump rule, a widening edit does NOT obligate a `format_version` bump. Every
document valid before is still valid; `"rim"` simply becomes newly acceptable. Mechanics: the
freshness gate now expects `baked` to include `rim`; the author must re-run `strictspec gen` so
`baked` is refreshed and re-committed (generated files must be committed — Projects/CLAUDE.md).
Between editing the source and re-baking, `gen`/`check` HARD-ERROR on staleness (below) — there
is no window where the schema silently tracks the source.

### Vocabulary REMOVAL → narrowing → bump + flip-scan catch

Remove `"tom"` from the registry and re-`gen`. The re-baked set becomes
`["kick","snare","hat","clap"]`. Removing an arm SHRINKS the accepted set (appendix-semantics
3.13/3.21, "removing SHRINKS"; DESIGN.md decision 13, "removing an enum arm (including a
sourced-enum arm removed or gone stale)" obligates a bump). Any existing pattern using
`"sound": "tom"` now flips valid→invalid.

- **Bump obligation:** `schema-drum-pattern.toml` must bump `format_version` 1 → 2, and a
  migration must be authored for at-rest documents that used the removed arm (e.g.
  `set_value_where` to remap `tom`→`kick`, or the consumer accepts the documents become
  invalid and migrates them out of that value).
- **Mechanical enforcement:** `strictspec diff`'s SAME-VERSION FLIP-SCAN (decision 25) replays
  the corpus against old and new schema at the SAME `format_version`. A document with
  `"sound":"tom"` flips valid→invalid ⇒ `STRICTSPEC_DIFF_NARROWING_UNBUMPED` (a `violated`-grade
  certificate claim; appendix-certificates A.5), a HARD ERROR that blocks release until the bump
  lands. This is exactly the "same-version flip-scan mechanically catches an un-bumped
  arm removal" clause (DESIGN.md Construct set / decision 32).

### STALENESS → hard error at gen AND check

If the source document changes (either direction) but the schema's `baked` list is NOT
refreshed, the baked arms differ from the resolved source. This is STALE:
`STRICTSPEC_ENUMSRC_STALE` · path `$.hits[].sound` · slots `{source: "sounds.registry.json",
baked: [...], actual: [...]}` — a HARD ERROR in BOTH `strictspec gen` and `strictspec check`
(appendix-error-codes 7; appendix-semantics 3.21). Semantics that matter:

- The staleness gate runs at BUILD/CHECK time, never at document-validation time. Validation
  reads only the baked list, so a stale schema still produces deterministic verdicts — it just
  cannot be (re)generated or pass `check` until refreshed. This preserves "the accepted set is a
  function of the schema text" while keeping the source edge honest.
- Related build-time codes on this edge: `STRICTSPEC_ENUMSRC_MISSING_SOURCE` (source document
  absent), `STRICTSPEC_ENUMSRC_BAD_SELECTOR` (selector does not resolve),
  `STRICTSPEC_ENUMSRC_SOURCE_NOT_STRINGS` (a resolved arm is not a string). Sourced arms must be
  string-typed literals — the registry's `hz` integers could never be an arm source.

## Awkward / inexpressible

- **Nothing inexpressible.** The construct, its freshness gate, its four build-time error codes,
  and the bump-rule interaction (widen/narrow/stale) are all pinned and compose cleanly.
- **Observation (not a gap):** the `selector` mini-language (`sounds[].name`) is a path-like
  projection. The spec pins the codes for a bad selector but does not, in the read material, pin
  the selector GRAMMAR itself. For the drafts here a JSONPath-ish `field[].field` projection is
  enough, but the selector grammar deserves its own pinned mini-spec (which projections are
  legal — array-wildcard, nested, filters?) so `STRICTSPEC_ENUMSRC_BAD_SELECTOR` has a precise
  boundary. Recorded as a spec-completeness item, not a construct gap — every draft here stays
  within a trivially-pinnable subset (single array-wildcard + terminal field).

## Verdict

FINDINGS: 1

1. The enum-source `selector` grammar is unpinned in the read material. Recommend pinning a
   closed selector mini-grammar (minimally: `field`, `field[].field`, nesting) in a spec
   appendix so `STRICTSPEC_ENUMSRC_BAD_SELECTOR`'s accept/reject boundary is normative. No new
   construct; a completeness fix to the decision-32 surface.
