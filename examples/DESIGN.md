# examples/ — Real-World Target Schemas (design-phase pressure tests)

Draft strictspec schemas for real ecosystem formats, ON PAPER, before implementation. If a
schema can't be written cleanly here, the spec is wrong and gets fixed now. Drafts become
migration starting points, conformance corpus, and (character-preview) the acceptance-test
source.

STATUS (Phase 3.3, 2026-07-27): all fifteen drafts came back clean with their gap notes resolved;
every draft has been NORMALIZED to the single pinned concrete TOML surface
(.stricttools/docs/appendix-surface-syntax.md) — the notation each draft had to invent is superseded. Sample
documents are unchanged (the pinned surface governs schemas/type files/migration files, not the
documents they validate). A 16th construct-only exercise, `datetime-exercise/`, was added to close
the datetime-scalar coverage gap. The construct set is STABLE BUT GROWING (decision 3, growth
phase): the analyzed corpus settled it, and further additions go through the examples/ gap-note
process.

Drafting order: claudestream and PixelWeaver FIRST — they stress migrations, unions, and
numeric scalars, where spec gaps are most likely; write them before the comfortable ones. The
construct set is STABLE BUT GROWING in the growth phase (decision 3): amendments (semantics
corrections, error-code catalogue growth) are normal and expected, recorded per release, and a
new construct enters only through the gap-note process. The construct-set stability GATE is ALL of
the drafts below coming back clean PLUS the gap-note items resolved in the specification; released-surface
compatibility is governed by semver at release boundaries. Some drafts are drawn from corpus-DRAFT
sources (BetterClaude, imagine) — paper schemas that stress the construct set without being
adoption artifacts.

Every schema header carries both fields per the versioning rules: `meta_version` (the
schema-language version) and `format_version` (what its documents must carry). Expected
diagnostics in every draft are written as ordered code+path lists plus template slot values
(the cross-target conformance surface — message text renders from the spec-pinned templates,
so every draft doubles as a message-identity fixture).

## Planned drafts

| # | Schema | Source project | What it stresses |
|---|---|---|---|
| 1 | agent definition + budget migration | claudestream | opaque JSON leaf (tool input_schema, declared stance: `unchecked = true` with mandatory `unchecked_reason`, or `consumer_check` — a missing-stance or missing-reason schema is the meta-schema-rejection fixture), optional extension fields — and the FLAGSHIP MIGRATION: `max_cost_usd: float` -> `cost_thresholds: [float]` as rename_field + wrap_in_array, down declared PARTIAL (unwrap of a multi-element list is the pinned hard-error case). Note: claudestream has ZERO at-rest corpus, so the deploy gate discharges via the first real adjudication file; the budget rename is a conformance FIXTURE, not a live migration |
| 2 | constraint-manifest, part-manifest, character-preview | PixelWeaver | the hard trio: nested typed maps, discriminated unions (full union-diagnostics section), nullable unions, NUMBER scalar (7 of 10 numeric fields), ordered-pair, ranges-disjoint, pairwise-distinct. The x- keyword MAPPING OBLIGATION: x-unique-field→unique-by, x-range-nonoverlap→ranges-disjoint, x-pairwise-distinct→pairwise-distinct, x-less-than-sibling→ordered-pair; x-lock/x-locks disposition is a gap note (to be determined in the draft). character-preview's hand translation IS the acceptance-test source |
| 3 | score | wavescript | JSON, integer `format_version` 2 already live; the REGISTRY GATE / conditional-applicability system — the strongest conditional-required stress in the corpus; enum-keyed unions (per-instrument params); the 47-score corpus (conformance seed + the 158-pair golden render-hash manifest as regression oracle). Synthesis engine/specgen stay consumer-side |
| 4 | changelog entry (JSONL) | rlsbl | per-line integer `format_version`, mixed-version stream reading, mode-parameterized shape (commit vs changeset-file — both `coverage_unit` modes), `release_type` + `packages` fields, conditional-required, the forgotten enum; the set-coverage CROSS-DOCUMENT form (every commit in range covered by an entry — evidence via the commits-in-range resolver); bootstrap-contract sketch (the one-time stamping script's shape-detection rules, as documentation) |
| 5 | release file | rlsbl | comment-carrying TOML round-trip (within-backend fixpoint), enums, cross-field rules (include/exclude disjoint, preid/bump coupling), optional fields modeling "no value" as absence (never a nullable union — unusable in TOML), DATETIME SCALARS (TOML natives; offset/local kind declaration; lexeme retention), TOML-null hard-error case |
| 6 | config | rlsbl | the `.rlsbl/config.json` shape: nested typed maps, enums, external-checks list (name/command/tag/depends_on/cwd records), install_paths, optional-absent fields; cross-field coupling within external-check entries |
| 7 | scene | predraw | recursion up to the depth limit, NODE-KIND UNION (fill: string-or-gradient — the construct exists because of this field), NUMBER scalar (47 of 48 numeric fields, incl. the unrepresentable-lexeme hard error), aliases (8 co-valid pairs), optional-absent fields (predraw's legacy defaults become consumer-side absence handling — defaults are not in the language, decision 30), tuples |
| 8 | workflow (stretch) | orxtra | recursive self-referential types, discriminated execution unions, intra-document dependency references, tagged-value entry point for the compose flow — if it fits, everything fits |
| 9 | custom-scalar schema | pgdesign | custom scalar registration (part of the language design; build-sequenced after the acceptance test): registers identifier/pgtype/sql-expression scalars with toolchain-registered lexeme rules, bindings, and rendering; the 2,100-line walker collapses to ~350-line schema; pgdesign's pkg/diagnostic (severities/suppression) stays consumer-side, fed by strictspec diagnostics |
| 10 | migration files x2 | claudestream, rlsbl | the closed op set: the shape-op flagship (draft #1, down: partial) and the pure-rename chain (dev_node: two rename_field migrations, down: total); collision semantics and where-predicates exercised; migrations are documents too — and double as `strictspec migrate --dry-run` structured-diff fixtures AND `strictspec diff` empirical certificate fixtures (corpus round-trip: soundness `corpus-supported` for the rename chain; the flagship's partial-down failure surfaces as a corpus-witnessed violation) |
| 11 | contract set: auth, profiles, usage, models, transcript | BetterClaude | FIVE contracts sharing named types via dedicated type-definition files — the primary stress-test for SHARED TYPE FILES (decision 21, reopened). A corpus-DRAFT source: these are construct stress-tests drafted from a design doc, not adoption artifacts. Adopts generated validators later, when its schemas phase arrives |
| 12 | document set: project.json, document.json, events.jsonl, sequence.json, inflight.json, bookmarks.json | imagine | a JSONL EVENT LOG (events.jsonl, per-line `format_version`) plus several JSON journals (inflight/bookmarks); mutate-then-revalidate journals via tagged-value constructors. A corpus-DRAFT source; imagine is independent — evaluate as a consumer later |
| 13 | shared-types exercise | (construct) | decision 21 (reopened): schemas importing named types from a dedicated type-definition file — types only, NO cross-file constraints, NO transitive imports. Positive draft plus the two meta-schema-rejection cases (a cross-file constraint reference; a transitive import) |
| 14 | enum-baking exercise | (construct) | enum arms sourced from a named document with toolchain-enforced freshness (first-class construct): a schema declaring enum arms baked from a source document; the stale-baked-arms hard error in gen/check; the sanctioned data→schema dependency edge |
| 15 | aggregates exercise | (construct) | count-limit and sum-limit (decision 23): aggregate constraint forms with literal bounds, positive and negative, at multiple scopes |
| 16 | datetime exercise | (construct) | Phase 3.3 coverage closure: `date` / `time` / `datetime` (offset and local) scalar kinds + a same-kind datetime range; closes the gap batch 2 flagged (release-file has no datetime field) |

## Method

Each draft: the schema in strictspec TOML, two or three real documents from the source project
(one valid, one or two invalid with expected ordered code+path diagnostics — including at
least one write-side case where applicable: a migration output or canonicalized alias), and a
gap note — anything the spec could not express, fed back into .stricttools/docs/DESIGN.md either as a
change or as an explicit exclusion. The construct set is considered settled only after every draft
comes back clean or the spec has absorbed its findings; thereafter additions go through this same
gap-note process (growth phase, decision 3), and released-surface compatibility is governed by
semver at release boundaries. Construct-only exercises (drafts 13–15) are drafted from the
spec itself rather than a source project; their gap notes are the deliverable.
