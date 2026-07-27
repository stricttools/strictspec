# Gap note — betterclaude-contracts (corpus-DRAFT source; primary SHARED-TYPE-FILE stress test)

> **DRAFTED FROM A DESIGN DOC OF AN UNSTARTED PROJECT — CONSTRUCT STRESS-TEST, NOT AN ADOPTION
> ARTIFACT.** These schemas are paper drafts from BetterClaude/DESIGN.md §3 (the `schemas/`
> contract sets) and its Phase 4, a phase that has NOT been started. BetterClaude's schemas do
> not exist yet; the concrete field shapes here are plausible reconstructions from the design
> prose, chosen to STRESS strictspec's constructs — above all the SHARED TYPE-DEFINITION FILE
> mechanism (decision 21, reopened). BetterClaude adopts generated validators LATER, when its
> schemas phase arrives (DESIGN.md — Migration roadmap, LATE §10; examples/DESIGN.md draft 11).
> Nothing here is a live contract.

This is the PRIMARY stress test for type imports: FIVE contract schemas (auth, profiles, usage,
models, transcript) plus a sixth PID-session schema, all importing named types from TWO shared
type-definition files.

## Files

- `types-claude-common.toml` — the primary shared type file: `Tier`, `ModelName`, `SessionId`,
  `Instant`, `ProfileName`, `ProjectPathCodec`.
- `types-http.toml` — a second shared type file: `HttpsUrl`, `OAuthClient`.
- `auth.toml` (imports from BOTH files), `profiles.toml`, `usage.toml`, `models.toml`,
  `transcript.toml`, `pid-session.toml`.
- Samples: `auth.valid.json`, `auth.invalid.json`, `transcript.valid.jsonl`, `models.valid.json`,
  `profiles.valid.json`.

## Import matrix (the reuse actually exercised)

| Type | file | auth | profiles | usage | models | transcript | pid-session |
|---|---|:-:|:-:|:-:|:-:|:-:|:-:|
| `Tier` | common | x | x | x | | | |
| `ModelName` | common | | x | x | x | | |
| `SessionId` | common | | | x | | x | x |
| `Instant` | common | x | | x | | x | x |
| `ProfileName` | common | | x | | | | |
| `ProjectPathCodec` | common | | | | | x | x |
| `OAuthClient` | http | x | | | | | |
| `HttpsUrl` | http | (via OAuthClient) | | | | | |

Every shared type is imported by ≥2 schemas except `ProfileName`/`OAuthClient` (each central to
one contract but genuinely shared vocabulary). `auth.toml` imports from TWO files at once.

## Clean — the shared-type mechanism holds under load

- **Multi-file imports.** `auth.toml` imports `OAuthClient` from `types-http.toml` AND `Tier`/
  `Instant` from `types-claude-common.toml`. Two `imports` entries, each naming one file and a
  type subset — no strain (decision 21; appendix-semantics 3.20).
- **Disjoint subsets per importer.** No importer takes the whole type file; the `types = [...]`
  list selects. `usage.toml` pulls four common types; `models.toml` pulls one. This is exactly
  the modularity decision 21 sanctions.
- **All three shareable kinds exercised across the set:** scalar-refinement (`ModelName`,
  `SessionId`, `Instant`, `HttpsUrl`, `ProfileName`, `ProjectPathCodec`, `Tier` as enum), record
  (`OAuthClient`), and — via `transcript.toml`'s root — a discriminated union built from imported
  scalar types. `Tier` as a named ENUM type is worth noting: an enum is a scalar-ish named type
  and imports cleanly.
- **Imports compose with every host construct:** imported types appear as record fields
  (`auth.oauth: OAuthClient`), typed-map values (`profiles.value.model: ModelName`), array item
  types, union-arm fields (`transcript` arms), and constraint operands (`usage` window
  `ordered-pair` over two imported `Instant`s). Because import is inlining (zero semantic effect
  beyond name resolution; appendix-semantics 3.20), composition is transparent.
- **A named type embedding a map** (`OAuthClient.headers` is an inline typed map) imports fine —
  the whole record inlines, map and all. A bare map is not itself a shared named type, but a
  record CONTAINING a map is shareable.
- **`Instant` as a scalar-refinement over a NON-string base** (datetime, kind offset) works and
  is reused three ways — demonstrating scalar-refinement is not string-only.
- **NUMBER scalar / safe_integers / ordered-pair / pairwise-distinct** all land: `usage.cost.
  total_cost_usd` is `number`; `usage`/`models` declare `safe_integers = true` (mandatory —
  both declare a TS target); each `usage` window carries `ordered-pair(start < end)`;
  `models.families[].name` is `pairwise-distinct`.

### Expected diagnostics — `auth.invalid.json` (traversal order, item 6)

1. `STRICTSPEC_VALUE_STRING_REGEX` · path `$.oauth.authorize_url` · slots `{actual:
   "http://claude.ai/oauth/authorize", pattern: "^https://[^\\s]+$"}` (imported `HttpsUrl`
   refinement inside imported `OAuthClient`; `http://` fails).
2. `STRICTSPEC_TYPE_DATETIME_KIND` · path `$.tokens.expires_at` · slots `{expected: "offset",
   got: "local"}` (`Instant` fixes kind offset; the string has no offset). Per appendix-semantics
   3.12 / item 11.
3. `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` · path `$.tier` · slots `{got: "max_50x", expected:
   ["free","pro","max_5x","max_20x","team","enterprise"], suggestion: "max_20x"}` (did-you-mean
   within threshold).
4. `STRICTSPEC_VALUE_STRING_TOO_LONG` · path `$.tier_display["free"].abbreviation` · slots
   `{actual: 5, limit: 4}` ("FREE!" exceeds `max_length = 4`).

`auth.valid.json`, `profiles.valid.json`, `models.valid.json`, `transcript.valid.jsonl` all
validate clean (incl. `models`' `[1m]` suffix matching the `ModelName` grammar and
`transcript`'s first line `parentUuid: null` via the nullable arm field).

## Findings

### Finding 1 — map keys cannot be constrained to an ENUM's members (only to a regex)

The tier->display table (`auth.tier_display`) should be keyed by `Tier` members. But typed-map
keys are constrained by a REGEX (`key_pattern`), not by an enum (appendix-semantics 3.2). I
mirrored the enum as a regex alternation `^(free|pro|max_5x|max_20x|team|enterprise)$`, which
DUPLICATES the `Tier` definition and drifts if `Tier` changes. Two honest observations:
  - This is expressible today (the regex works), so NOT inexpressible.
  - But it is a DRY gap: there is no way to say "map keys must be members of enum `Tier`". A
    small, in-vocabulary enhancement would be to allow a map's key type to be a named enum type
    (keys validated by enum membership, reusing the imported `Tier`). Recommend the spec consider
    permitting `key_type = "<EnumType>"` as an alternative to `key_pattern`. Absent that, a
    consumer must keep the key regex and the enum in sync by hand (or bake the enum into the
    regex via the sourced-enum mechanism — heavyweight for this case).

### Finding 2 — `SessionId` reused as a generic UUID blurs intent

`transcript`'s `uuid`/`parentUuid`/`leafUuid` are message UUIDs, not session ids, but I imported
`SessionId` (a UUID refinement) for all of them to avoid defining a near-identical `Uuid` type.
This is fine mechanically (same accepted set) but semantically loose — the type NAME misleads.
Not a spec gap; a modeling note: the real BetterClaude schema should define a generic `Uuid`
scalar-refinement in the common type file and let `SessionId` be `Uuid` (or a distinct alias), so
names match intent. Recorded so the adoption phase does not inherit this shortcut.

### Finding 3 — the three session layouts are a locator concern, modeled as an enum

BetterClaude's "three session layouts (code/desktop/cowork)" describe three on-disk DIRECTORY
layouts where transcripts live, not three transcript-LINE shapes. I modeled the layout as an enum
field on the PID-session file (`pid-session.toml`), which is the document that actually needs to
know the layout. The transcript LINE schema is layout-agnostic (a discriminated union on `type`).
This is the correct decomposition; recorded because a naive reading might try to make the
transcript line a union over layouts, which would be wrong (the line shape does not vary by
layout).

### Finding 4 — opaque message payloads: `unchecked` vs `consumer_check`

`transcript` message bodies are the Anthropic API message shape — complex, API-owned, and not
this contract's job to validate. I used the `unchecked = true` + mandatory `unchecked_reason`
stance (decision 29) rather than `consumer_check`, because BetterClaude's transcript CONTRACT
does not itself run a message validator (claudetranscript normalizes downstream, but that is
adaptation, not validation of the API shape). Both stances are legal; the choice records that
this is a genuine blind spot the contract accepts, not a deferred check. `strictspec check` will
inventory it. (Contrast imagine-documents, where module payloads ARE validated on ingest, so
`consumer_check` is the right stance there — the two drafts deliberately exercise both stances.)

## Awkward

- **Deep nesting in `models.toml`** (`families[].models[].fields...`) makes the schema file
  verbose. This is inherent to the data shape, not a notation defect; a consumer could factor
  `Model` and `Family` into the common type file to flatten the schema. Recorded as an ergonomics
  observation — the flattening is a free choice, and factoring them out would ADD to the
  type-import reuse.

## Inexpressible

- **Nothing document-level is inexpressible.** All six contracts express with imported types plus
  in-vocabulary constructs. The one DRY shortfall (enum-constrained map keys, finding 1) has a
  working regex workaround today.

## Verdict

FINDINGS: 4

1. Map keys cannot be enum-constrained (only regex); consider allowing `key_type = "<EnumType>"`
   to reuse an imported enum. Working regex workaround exists today (DRY gap, not inexpressible).
2. Reusing `SessionId` as a generic UUID is a modeling shortcut; define a distinct `Uuid` in the
   common type file at adoption.
3. The three session layouts belong on the locator/PID-session document as an enum, not on the
   transcript line; recorded to prevent mis-modeling.
4. Opaque transcript payloads correctly use `unchecked` (contract runs no validator), exercising
   the other opaque-leaf stance from imagine-documents' `consumer_check`.

The shared-type-file mechanism (decision 21) came back CLEAN under the heaviest load in the
corpus: multi-file imports, disjoint subsets, all three shareable kinds, and composition with
every host construct. Only finding 1 suggests a possible small vocabulary enhancement
(enum-typed map keys); it is a DRY improvement, not a blocker, and everything expresses today.

## RESOLUTION (Phase 3.3)

- **F1 (enum-typed map keys / `key_type`)** — REJECTED. Single consumer; the `key_pattern` regex
  expresses it today (and enum sourcing can bake the enum into the key regex). Map keys remain
  regex-constrained only (`spec/DESIGN.md` — vocabulary rejection rationale;
  `appendix-surface-syntax.md` §11). Revisit on recurrence.
- **F2 (SessionId reused as generic UUID)** — BOUNDARY-CONFIRMED. Modeling note, not a spec gap.
- **F3 (session layouts as an enum on the PID-session doc)** — BOUNDARY-CONFIRMED. Correct
  decomposition.
- **F4 (opaque transcript payload `unchecked`)** — BOUNDARY-CONFIRMED. Both opaque stances legal
  (decision 29); this draft deliberately exercises `unchecked`, imagine exercises `consumer_check`.

VERDICT: RESOLVED (Phase 3.3). Draft normalized to the pinned surface (root-as-named-type,
kind-typed named types, `role = "..."`, uniform `.item`/`.value` sites).
