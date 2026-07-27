# Gap note — rlsbl project config (`.rlsbl/config.json`)

Source: `rlsbl/rlsbl/config.py` (`validate_config_schema`,
`validate_pipelines_config`, `validate_pipeline_target_links`) and
`rlsbl/rlsbl/external_checks.py` (`validate_external_checks`).
Schema: `config.schema.toml`. Samples: `valid-01.json` (rlsbl's own config),
`invalid-01-launcher.json`.

## Expressed cleanly

- **`publish_mode` required enum, no default.** rlsbl's "required-read, no
  silent default" (`get_publish_mode` raises on absence) is exactly a required
  enum field: `STRICTSPEC_TYPE_MISSING_REQUIRED` on absence,
  `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` on a bad value. Correct.
- **Pipelines as a typed map.** `pipelines` maps a name regex to a `Pipeline`
  record; mandatory `type`/`local` are required fields; a bad `type` is caught
  by the enum. Clean.
- **`type` enum sourced from `PIPELINE_TYPES`.** Modeled via the sourced-enum
  construct (decision 32): arms baked from `rlsbl.pipelines.PIPELINE_TYPES` at
  gen time, freshness enforced (`STRICTSPEC_ENUMSRC_STALE`). This is exactly the
  sanctioned data→schema edge — rlsbl grows a pipeline type, `strictspec gen`
  re-bakes, a stale bake is a hard error. Clean and a good fit.
- **Value-triggered conditional-required.** `type=="npm" ⟹ provenance`,
  `type=="go" ⟹ artifact`, `artifact=="launcher" ⟹ wraps + binary_source`,
  `assets==true ⟹ max_asset_size_mb` all map onto `conditional-required` (§3.24).
  These are the bread-and-butter of the config and the vocabulary carries them.
- **External checks as a discriminated union on `kind`.** `structured` vs
  `freeform` arms, cross-kind field forbidding (a `command` on a structured entry,
  a `tool` on a freeform entry) handled by closed-record + unknown-key for free.
  `tool` enum, `paths` non-empty array, `name` regex + `unique-by` — all clean.
  This is a textbook discriminated union and the strongest positive in the draft.
- **`target` as a nullable union (JSON).** `pipeline.target` is `string | null`;
  because the schema declares `syntax = "json"` the nullable union is legal
  (unlike the TOML release file). null short-circuits the reference check. Good
  demonstration that the nullable construct is usable exactly where the document
  syntax permits it.

## Awkward / partial

- **`pipeline.target` resolves into `ROOT.targets`.** `targets` is a node-kind
  union (bare string OR `{name,path}`), so the resolution set is "the scalar, or
  the record's `name`". `intra-document-references` is defined as resolving a
  reference "within the same document" (§3.24) but does not pin HOW a heterogen-
  eous target collection contributes its key set. Expressible in spirit; the spec
  should pin whether a reference may resolve against a node-kind-union collection
  by a per-arm key extractor.
- **Empty `targets` list.** rlsbl hard-errors on `targets: []` with a specific
  remediation ("use publish_mode: none"). `min_len = 1` on the array rejects it,
  but the strictspec diagnostic is the generic `STRICTSPEC_VALUE_ARRAY_TOO_SHORT`,
  losing rlsbl's remediation prose. Not a gap in expressiveness — a gap in
  domain-specific messaging, which strictspec deliberately does not carry (the
  remediation would live in a consumer-native wrapper). Noted, not counted.
- **Removed `private` key.** rlsbl emits a migration hint when it sees `private`.
  strictspec treats `private` as a plain unknown key
  (`STRICTSPEC_KEY_UNKNOWN` + did-you-mean "publish_mode"? — edit distance is >2,
  so no suggestion). The rename is properly a `rename_field` migration
  (private→publish_mode) with a value remap, but that remap is value-COMPUTING
  (false→"ci", true→"none"), which the closed op set forbids. So the migration is
  a `remove_field` + `add_field` with a literal, per document — expressible only
  if the two are done as a one-time conversion script. Noted; consistent with the
  "no value-computing ops" rule.

## INEXPRESSIBLE (findings)

- **FINDING 1 — reference JOIN predicate (`wraps` → sibling.artifact=="binary").**
  A launcher's `wraps` must name a sibling pipeline AND that sibling's `artifact`
  must be `"binary"`. `intra-document-references` checks only that the reference
  RESOLVES (§3.24: "excludes trees with a dangling in-document reference"). It
  carries no predicate on the RESOLVED target's fields. The `target_predicate`
  key in the schema is fictional — flagged inline. This is the reference-vocabulary
  gap the task asked about: references resolve, but "resolve AND the target
  satisfies P" is a join the vocabulary cannot express. A launcher pointing at a
  `library` pipeline resolves fine and passes. Would need either a new
  "reference-with-target-predicate" form or a consumer-native check.

- **FINDING 2 — cross-scope existential forbidden-when (launcher under
  publish_mode==none).** rlsbl forbids ANY launcher pipeline when the ROOT
  `publish_mode=="none"`. Two problems for the vocabulary:
  1. the condition (`publish_mode` at ROOT) and the forbidden thing (a nested map
     entry's `artifact` value) live at different scopes;
  2. the forbidden thing is an EXISTENTIAL over a map ("∃ pipeline with
     artifact==launcher"), not a field presence.
  `forbidden-when` "tests presence/equality only" of a field in the containing
  record (§3.24). The `existential` schema key is fictional. Inexpressible;
  consumer-native.

- **FINDING 3 — `depends_on` references an OPEN namespace.** An external check's
  `depends_on` lists check NAMES, which may be other `external_checks` entries OR
  BUILT-IN rlsbl checks (`test-suite`, `lint`, …) that are not in the document at
  all. So neither `intra-document-references` (the built-in names never resolve
  in-document — false positives) nor `named-reference-must-resolve` against a
  closed resolver (there is no resolver enumerating rlsbl's built-in check
  registry) fits. The reference target set is partly in-document and partly in the
  consuming tool's runtime. Either a bespoke resolver
  (`rlsbl-registered-checks`) is added to the closed evidence vocabulary, or
  `depends_on` validation stays consumer-native. This is the sharpest
  reference-vocabulary finding: the vocabulary assumes a reference resolves within
  ONE known set, and rlsbl's is a union of a document set and a runtime set.

## Notes

- **Shared arm fields duplicated.** `name`/`tag`/`depends_on`/`cwd` are repeated
  on both `StructuredCheck` and `FreeformCheck` because discriminated-union arms
  are whole closed records with no shared base. Not incorrect, but verbose;
  type-definition imports (decision 21) help only for whole named types, not for
  "these fields common to all arms". Minor ergonomics note, not a finding.
- **Closed-record vs the real config's extra keys.** rlsbl's live config also
  carries `env_file`, `push_timeout`, `batch_limits`, `services`, `test`, … The
  `ROOT` record here models a subset; the unknown-key invariant means a full
  schema must enumerate every key rlsbl tolerates. That is the intended
  discipline (no silent-ignore), just larger than this draft. `valid-01.json`
  omits the unmodeled keys so it validates.

## Verdict

FINDINGS: 3
- F1: `intra-document-references` cannot carry a predicate on the resolved target
  (the `wraps`→binary join).
- F2: no cross-scope existential `forbidden-when` (launcher under
  publish_mode==none).
- F3: `depends_on` resolves against an OPEN namespace (in-document entries ∪
  tool-runtime built-in checks); no single form or resolver covers it.
