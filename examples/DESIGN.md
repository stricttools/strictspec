# examples/ — Real-World Target Schemas (design-phase pressure tests)

Draft dclrbl schemas for real ecosystem formats, ON PAPER, before implementation. If a schema
can't be written cleanly here, the spec is wrong and gets fixed now. Drafts become migration
starting points, conformance corpus, and (character-preview) the acceptance-test source.

Drafting order: claudestream and a private project FIRST — they stress migrations, unions, and
numeric scalars, where spec gaps are most likely; write them before the comfortable ones.
Drafts 11–13 (demobl, F, step) are MANDATORY BEFORE THE CONSTRUCT SET FREEZES: they were
absent from the original sizing audit, and the construct set's sufficiency claim is verified
against them, never assumed.

Every schema header carries both fields per the versioning rules: `dclrbl_spec_version` (the
schema-language version) and `format_version` (what its documents must carry). Expected
diagnostics in every draft are written as ordered code+path lists plus template slot values
(the cross-target conformance surface — message text renders from the spec-pinned templates,
so every draft doubles as a message-identity fixture).

## Planned drafts

| # | Schema | Source project | What it stresses |
|---|---|---|---|
| 1 | agent definition + budget migration | claudestream | opaque JSON leaf (tool input_schema, declared stance: `unchecked = true` with mandatory `unchecked_reason`, or `consumer_check` — a missing-stance or missing-reason schema is the meta-schema-rejection fixture), optional extension fields — and the FLAGSHIP MIGRATION: `max_cost_usd: float` -> `cost_thresholds: [float]` as rename_field + wrap_in_array, down declared PARTIAL (unwrap of a multi-element list is the pinned hard-error case) |
| 2 | constraint-manifest, part-manifest, character-preview | a private project | the hard trio: nested typed maps, discriminated unions (full union-diagnostics section), nullable unions, NUMBER scalar (7 of 10 numeric fields), ordered-pair, ranges-disjoint, pairwise-distinct. character-preview's hand translation IS the acceptance-test source |
| 3 | schedule config | wakethemup | version gate (integer `format_version`, exact match, structured remediation payload), the always-error unknown-key invariant (a typo'd key at any nesting level yielding the canonical diagnostic), regex on map keys (env table) — the floor |
| 4 | score | tunebox | discriminated unions keyed on an enum (per-instrument params), opaque domain strings with named consumer checks (consumer-native code over the typed record, declared via `consumer_check` and inventoried by `dclrbl check`), exclusive bounds, unique-by, cross-field rules |
| 5 | agent definition | toolstream | the second, separate agent schema: the small strict subset, validation greenfield |
| 6 | changelog entry (JSONL) | rlsbl | per-line integer `format_version`, mixed-version stream reading, mode-parameterized shape (commit vs changeset-file), conditional-required, the forgotten enum; the set-coverage CROSS-DOCUMENT form (every commit in range covered by an entry — evidence via the commits-in-range resolver); bootstrap-contract sketch (the one-time stamping script's shape-detection rules, as documentation) |
| 7 | release file | rlsbl | comment-carrying TOML round-trip (within-backend fixpoint), enums, cross-field rules (include/exclude disjoint, preid/bump coupling), optional fields modeling "no value" as absence (never a nullable union — unusable in TOML), DATETIME SCALARS (TOML natives; offset/local kind declaration; lexeme retention), TOML-null hard-error case |
| 8 | scene | predraw | recursion up to the depth limit, NODE-KIND UNION (fill: string-or-gradient — the construct exists because of this field), NUMBER scalar (47 of 48 numeric fields, incl. the unrepresentable-lexeme hard error), aliases (8 co-valid pairs), optional-absent fields (predraw's legacy defaults become consumer-side absence handling — defaults are not in the language, decision 30), tuples |
| 9 | workflow (stretch) | orxtra | recursive self-referential types, discriminated execution unions, intra-document dependency references — if it fits, everything fits |
| 10 | migration files x2 | claudestream, rlsbl | the closed op set: the shape-op flagship (draft #1, down: partial) and the pure-rename chain (dev_node: two rename_field migrations, down: total); collision semantics and where-predicates exercised; migrations are documents too — and double as `dclrbl migrate --dry-run` structured-diff fixtures AND `dclrbl diff` empirical certificate fixtures (corpus round-trip: soundness `corpus-supported` for the rename chain; the flagship's partial-down failure surfaces as a corpus-witnessed violation) |
| 11 | presentation material | demobl | CONSTRUCT-FREEZE GATE: timings and durations (does the datetime scalar set suffice, or is a duration scalar needed? — gap note feeds spec/), asset references (intra-document and cross-document reference forms), ordered semantic sections |
| 12 | animation | F | CONSTRUCT-FREEZE GATE: keyframe tuples, easing curves (tuple-heavy structures), timeline offsets (numeric vs duration — gap note), node-kind unions under animation targets, recursion in composed animations |
| 13 | wizard/workflow definition | step | CONSTRUCT-FREEZE GATE: step-flow branching — the strongest known pressure toward conditional constructs (if/then/else is excluded; can flow branching be modeled with discriminated unions + intra-document references + conditional-required, or is the exclusion wrong? — the gap note here is the single most consequential one), cross-step references, terminal-state validation |
| 14 | custom-scalar schema | pgdesign | custom scalar registration (part of the language design; build-sequenced after the acceptance test): a named scalar with toolchain-registered lexeme rule, binding, and rendering; the 2,100-line walker collapses to ~350-line schema, domain analysis stays consumer code |

## Method

Each draft: the schema in dclrbl TOML, two or three real documents from the source project
(one valid, one or two invalid with expected ordered code+path diagnostics — including at
least one write-side case where applicable: a migration output or canonicalized alias), and a
gap note — anything the spec could not express, fed back into spec/DESIGN.md either as a
change or as an explicit exclusion. For drafts 11–13 the gap note is the deliverable: the
construct set freezes only after all three come back clean or the spec has absorbed their
findings.
