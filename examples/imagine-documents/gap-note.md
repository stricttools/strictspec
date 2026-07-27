# Gap note — imagine-documents (corpus-DRAFT source; examples/DESIGN.md draft 12)

> CORPUS-DRAFT SOURCE, NOT AN ADOPTION ARTIFACT. These six schemas are drafted from imagine's
> DESIGN.md to STRESS the strictspec construct set against a JSONL event log, discriminated-union
> journals, and module-opaque payloads. imagine is independent; it is evaluated as a possible
> consumer LATER (DESIGN.md — Migration roadmap, LATE §11). Nothing here is a live imagine schema.

Six documents (imagine §5/§6.1): `project.json`, `document.json`, `events.jsonl`,
`sequence.json`, `inflight.json`, `bookmarks.json`. Stresses: JSONL per-line versioning +
rotation, discriminated unions, module-opaque leaves, and imagine's closed-error philosophy.

## Files

- `types-imagine-common.toml` — shared scalar-refinement types (Ulid, DocRef, PrincipalId,
  NodeRef).
- `schema-project.toml`, `schema-document.toml`, `schema-events.toml`, `schema-sequence.toml`,
  `schema-inflight.toml`, `schema-bookmarks.toml`.
- Samples: `project.valid.json`, `events.valid.jsonl`, `events.invalid.jsonl`,
  `sequence.valid.json`, `inflight.valid.json`.

## Clean

- **Principal registry map (project.json).** A typed map `PrincipalId -> { type: enum, name }`
  expresses the registry directly (§2). Regex-keyed map + enum arm, no strain.
- **JSONL event log with per-line versioning (events.jsonl).** `document_syntax = "jsonl"`; each
  line is one event record carrying its own `format_version`; readers stream line-by-line, each
  line independently gated and validated (DESIGN.md — Document model; decision 13 per-line). The
  `events.invalid.jsonl` sample is the poster child: line 1 valid, lines 2–4 each fail
  independently (below) — exactly the "all-errors-in-one-pass per line, memory bounded by the
  largest line" behavior.
- **Discriminated-union journal (sequence.json).** `open` is a nullable discriminated union on
  `kind` with four arms (agent-turn / restore / reconciliation / rename), matching §6.9's per-kind
  restart rules. Same-kind disambiguation is unnecessary — each arm has a distinct literal `kind`.
  The `inflight.json` op is the same pattern discriminated on `type` over the file-op vocabulary
  (§6.3).
- **Module-opaque leaves (events.data, events.context.view, bookmarks values).** These are the
  engine's blackbox boundary (§5 principle 5): the engine stores/transports module-typed values
  and never interprets them. Modeled as `opaque_json` leaves with the `consumer_check` stance
  (decision 29) naming the module's ingest/view validator — the boundary is ON RECORD and printed
  by `strictspec check`'s blind-spot inventory. This is the intended use of the opaque leaf: a
  typo inside a module payload is invisible to strictspec BY DECLARATION, not by accident.
- **Nullable unions used correctly (JSON only).** `sequence.open` and `inflight.op` are `T | null`
  (§6.9 "absent/null when nothing is in flight"). Legal because these schemas are JSON, where
  nullable unions are fully available (appendix-semantics 3.7). See finding 3 for the TOML
  contrast.

### Expected diagnostics — `events.invalid.jsonl` (per line, independent)

- Line 1: valid.
- Line 2: `STRICTSPEC_VALUE_STRING_REGEX` · path `$.id@L2:...` · slots `{actual: "not-a-ulid",
  pattern: "^[0-9A-HJKMNP-TV-Z]{26}$"}` (the `Ulid` refinement; path carries the JSONL line
  suffix per appendix-rendering Part B).
- Line 3: `STRICTSPEC_TYPE_NOT_INTEGER` · path `$.cursor@L3:...` · slot `{got: "string"}`
  (`"3"` is a string, not an integer lexeme).
- Line 4: `STRICTSPEC_TYPE_MISSING_REQUIRED` · path `$@L4:...` · slot `{key: "data"}`.

## Findings

### Finding 1 — imagine's engineVersion / moduleVersion overlap strictspec's format_version

imagine ALREADY version-gates content: `document.json.moduleVersion` mismatch on open is "a hard
error pointing at `imagine migrate` — never a silent shim" (§6.1), and `engineVersion` is
analogous for the project. This is EXACTLY strictspec's `format_version` gate discipline
(DESIGN.md decision 13), independently invented. Under a real adoption, `moduleVersion` and
`engineVersion` would COLLAPSE onto strictspec's `format_version` — the document would carry one
integer version field, gated once, with `imagine migrate` becoming `strictspec migrate`. In these
paper schemas I kept BOTH (a strictspec `format_version` for the gate AND `engineVersion`/
`moduleVersion` as ordinary content integers) to stay faithful to the current design, but the
duplication is an artifact of paper-drafting a not-yet-consumer, not a strictspec gap. Recorded so
the adoption checkpoint (roadmap §11) knows to unify them.

### Finding 2 — field-level immutability is not a strictspec construct (correctly consumer/op-layer)

§6.1 requires `engineVersion`/`moduleVersion` be IMMUTABLE via ops (the new value must equal
HEAD's). strictspec validates a document in isolation; it has no notion of "the previous version
of this field" — immutability is a relation between two documents (before/after an op), which is
imagine's OP-LAYER concern (`immutable-field` in its §2.1 enum), not a single-document schema
predicate. This is the right boundary: strictspec's `doc-diff` could REPORT the change, but
enforcing "this field may not change" belongs to imagine's commit path. Not a gap; a boundary
note. (Format evolution's own version bump is the one sanctioned way a version field changes —
via migration, decision 9.)

### Finding 3 — module payloads are opaque, so the engine cannot discriminate event/module types

`events.type` mixes core event types (turn.handoff, travel, ...) with module event types (select,
draw.2d, ...), and `events.data` is module-owned and opaque (§8.1: payloads validated on ingest by
the MODULE). A strictspec ENGINE-LEVEL schema therefore CANNOT model `type`→`data` as a
discriminated union with typed arms — the arm bodies live in module schemas the engine never
imports (§5 blackbox). Modeling `data` as an `opaque_json` consumer_check leaf is the faithful and
correct choice: it records the boundary and defers payload validation to the module's ingest check
(consumer-native, downstream). This is not a limitation to fix — it is the cartridge boundary
working as designed. Worth recording: a future imagine module COULD ship its OWN strictspec schema
for its event payloads, and the engine schema would still see them as opaque — the two layers
compose without the engine schema ever discriminating module types.

### Finding 4 — the closed error-enum philosophy aligns with strictspec, with one boundary

imagine's §2.1 is a CLOSED error enum ("adding a code is a schema change; every refusal names one
of these codes") and principle 6 is "hard errors, no silent degradation." This is philosophically
identical to strictspec's closed `STRICTSPEC_*` catalogue (permanent codes, no warnings, no
severity — DESIGN.md decision 16 / Unknown-key policy). The BOUNDARY: imagine's enum spans errors
strictspec would NOT own — `stale-write`, `session-conflict`, `travel-gated`, `render-timeout` are
CONCURRENCY / LIFECYCLE / RENDER conditions, not document-validation verdicts. Only imagine's
`validation` family (`schema-invalid`, `structure-violation`, `version-mismatch`,
`immutable-field`, `module-unknown`) overlaps strictspec's surface, and even there `immutable-field`
(finding 2) and `module-unknown` (a directory-structure rule, not a document rule) sit outside.
Net: the philosophies match; the enums intersect only on genuine document-validation errors, and
that intersection maps cleanly onto `STRICTSPEC_SCHEMA_*` / `STRICTSPEC_GATE_*` / `STRICTSPEC_TYPE_*`.
No gap; a scoping observation for the adoption checkpoint.

### Finding 5 — JSONL rotation and pruning are a filesystem lifecycle, not a schema property

events.jsonl rotates into `events/<turn>.jsonl` at each tag and prunes beyond 50 turns (§8.1). The
strictspec schema validates the CONTENT of each line; rotation/pruning/cursor-derivation are
imagine's runtime lifecycle over the file family. strictspec's per-line gate and streaming reader
serve the reading side; the write-side single-writer O_APPEND discipline (DESIGN.md decision 20)
matches imagine's "one pen." Nothing to express in the schema; recorded so the boundary is on the
record.

## Awkward

- **Union-arm keys with dots** (`op.arms."file.create"`). The inflight op discriminates on
  `type = "file.create"` etc., whose arm NAMES contain a dot. In the schema notation these arm
  tables need quoting (`[fields.op.arms."file.create".fields...]`). This works but is visually
  heavy. Not a spec gap — the discriminator VALUES are literals and legal; only the schema-file
  ergonomics are slightly awkward. A consumer could name arms `create`/`update` and map them to
  the dotted literal via the `value` field, decoupling arm-name from literal-value. Recorded as a
  minor notation ergonomics observation.

## Inexpressible

- **Nothing document-level is inexpressible.** Every field of all six documents expresses.
  The genuinely out-of-scope items (immutability across ops, cross-document referential integrity
  at tag boundaries, rotation lifecycle, the concurrency/render error families) are correctly
  consumer/engine concerns, not single-document validation.

## Verdict

FINDINGS: 5

1. engineVersion/moduleVersion overlap strictspec's format_version — unify at adoption (roadmap
   §11); duplication here is a paper-drafting artifact.
2. Field-level immutability is an op-layer relation, correctly outside single-document validation.
3. Module payloads are opaque by the cartridge boundary; the engine schema cannot discriminate
   module event types — `opaque_json` + consumer_check is the faithful modeling.
4. imagine's closed error enum matches strictspec's philosophy; the enums intersect only on
   genuine document-validation errors.
5. JSONL rotation/pruning is a filesystem lifecycle, not a schema property.

All five are scoping/boundary observations for a FUTURE adoption evaluation, not construct gaps.
No new construct is required: JSONL per-line versioning, discriminated unions (incl. nullable),
regex-keyed maps, and module-opaque leaves all express the imagine document family cleanly.
