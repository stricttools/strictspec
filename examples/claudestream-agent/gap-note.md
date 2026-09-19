# Gap note — claudestream AgentDefinition (greenfield)

A greenfield strictspec schema for the at-rest `.claudestream/agents/<name>.agent.json`
documents. Field set read from `claudestream/_agent.py` (`AgentDefinition`) and `_options.py`
(`ToolSchema`, `Budget`, `Sandbox`, `McpOptions`, `StreamOptions`) + `policy.py` (`Sandbox`).
Documents are JSON.

## What the language expressed cleanly

- **The whole record graph** (AgentDefinition -> ToolSchema / Sandbox / Budget / McpOptions /
  StreamOptions) maps directly to closed records + named types. The `msgspec.Struct` field set
  translates field-for-field.
- **Optional-vs-required from msgspec defaults.** Fields with a source default (`description=""`,
  `tools=None`, `sandbox=None`, the `bool=False` sandbox flags) become **optional-absent**
  (decision 30 — no bind-time injection; claudestream's code already reads these as "None means
  fall back"). Fields with **no** default in a nested struct (`McpOptions.config_files`/`strict`,
  all five `StreamOptions` bools) are **required when the parent object is present** — exactly the
  required/optional split the language draws.
- **Non-negative budget thresholds** (`validate_budget`) map to `min = 0` on the array item
  scalars: `cost_thresholds` items are `number` (float, ≥0), `turn_thresholds`/`token_thresholds`
  items are `integer` (≥0).
- **The opaque JSON leaf (the headline stress).** `ToolSchema.input_schema: dict | None` is
  arbitrary JSON Schema — strictspec must never introspect it. Modeled as `type = "opaque"` with
  the mandatory stance `unchecked = true` + `unchecked_reason` (decision 29). `strictspec check`
  inventories it as a declared blind spot. The two meta-schema-rejection fixtures are included:
  - `fixtures/opaque-no-stance.reject.toml` -> `STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE`
  - `fixtures/unchecked-no-reason.reject.toml` -> `STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON`
  (`consumer_check = "<name>"` would be the alternative stance if a named consumer-native check
  validated the blob; here the blob is genuinely opaque, so `unchecked` is correct.)

## Findings

### FINDING 1 — meta-schema surface syntax still unpinned (shared with sibling drafts)
See the PixelWeaver gap note, Finding 1. Not repeated.

### FINDING 2 — three distinct "version" tokens coexist in one document (a naming clarity WIN)
The at-rest document carries BOTH the strictspec integer `format_version` gate (NEW) AND the
agent's own string `version` field ("Schema version of this agent definition"). The schema
additionally has `meta_version`. This is precisely the three-way disambiguation the spec pins
(document `format_version` vs schema `meta_version` vs a consumer's own semantic string): the
draft keeps `version` as an ordinary required string field and adds `format_version` as the gate.
No collision, no gap — recording it as evidence the token split reads cleanly on a real document
that happens to exercise all three.

### FINDING 3 — ZERO at-rest corpus; the deploy gate discharges via an adjudication file
claudestream has **no existing `.agent.json` corpus** (the schema is greenfield; agents are
authored fresh). `strictspec diff` requires a non-empty corpus (`STRICTSPEC_DIFF_CORPUS_EMPTY`),
and the format_version deploy gate's green light is `corpus-supported` **over the consumer's
declared at-rest corpus** — which here is empty. Per decision 25, a consumer with no corpus
discharges via a committed **adjudication file**: a strictspec-schema'd TOML document naming each
otherwise-unsupported claim with a signed manual justification (`STRICTSPEC_DIFF_ADJUDICATION_*`
police its validity). For claudestream's first release this is the intended path — the flagship
budget migration (see `../migrations`) is a conformance FIXTURE, not a live migration, precisely
because there are no documents to migrate. Recording the adoption sequence: ship the schema, gate
the first `format_version` bump via an adjudication file (there is no bypass).

### FINDING 4 — optional-absent vs nullable: chose optional-absent for `| None` fields
Every `X | None` field (`tools`, `sandbox`, `model`, `mcp`, `stream`, `input_schema`,
`Sandbox.tools`, `Sandbox.write_paths`) is modeled as **optional-absent**, not as a nullable
union. Both are legal here (documents are JSON, so `T | null` would be allowed), but optional-
absent matches msgspec's `None`-default semantics (the field is simply omitted in the serialized
`.agent.json`) and keeps the door open to a future TOML authoring surface for agents without
tripping `STRICTSPEC_SCHEMA_TOML_NULLABLE`. No document in practice writes an explicit `null`
for these. This is a modeling choice, not a gap — noting it so the choice is on record.

## Expected diagnostics (sample documents)

- `samples/reviewer.agent.json` — VALID (exercises tools+opaque input_schema, sandbox, budget,
  stream).
- `samples/missing-gate.agent.json` — the version gate runs FIRST:
  `STRICTSPEC_GATE_ABSENT` · schema `AgentDefinition` · expected `1` · invocation
  `strictspec migrate --schema AgentDefinition --to 1 <paths>` (structural checks do not run).
- `fixtures/opaque-no-stance.reject.toml` — `STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE` ·
  `$.types.ToolSchema.fields.input_schema`.
- `fixtures/unchecked-no-reason.reject.toml` — `STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON` ·
  `$.types.ToolSchema.fields.input_schema`.

## Verdict

FINDINGS: 4 — RESOLVED (Phase 3.3).

## RESOLUTION (Phase 3.3)

- **F1 (surface syntax unpinned)** — ADOPTED. Pinned in `.stricttools/docs/appendix-surface-syntax.md`. Draft
  and both reject fixtures normalized.
- **F2 (three version tokens)** — BOUNDARY-CONFIRMED. A naming-clarity win; no gap.
- **F3 (zero-corpus adjudication path)** — BOUNDARY-CONFIRMED, and VERIFIED. The adjudication file
  SHAPE is pinned in `.stricttools/docs/appendix-certificates.md` Part B (top-level + per-entry fields;
  validated by its shipped schema, `STRICTSPEC_DIFF_ADJUDICATION_*` police it). It is NOT unpinned
  — this note treats it as the intended path, consistent with the pinned shape. No gap.
- **F4 (optional-absent vs nullable)** — BOUNDARY-CONFIRMED. Modeling choice, on record.

VERDICT: RESOLVED (Phase 3.3).
