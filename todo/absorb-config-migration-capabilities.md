# Migration engine: absorb runtime config-migration capabilities

## Context

strictspec already has schema format-version migrations (`*.migration.toml`:
`from_format_version`/`to_format_version`, ops, down-ops with total/partial
classification, diff adjudication). Elsewhere in the ecosystem there is a
separate, mature runtime config-migration tool for TOML files with an
overlapping op vocabulary but its own engine. Ecosystem decision: strictspec
supersedes it entirely, and it is retired once supersession is complete and
its consumers are migrated. No compat period, no dual op vocabularies.

Decision provenance: deliberate user decision ("supersede and retire"), going
beyond the recommended convergence option.

## Problem

For strictspec's migration system to be the only one, it must absorb the
capabilities the runtime tool earned, none of which strictspec's migration
layer currently has:

1. **Comment- and format-preserving execution.** Migrating a deployed TOML
   file must not destroy comments or formatting. Mature comment-preserving
   TOML editing libraries exist for Go and Python; **no comment-preserving
   TOML editor is known to exist for TypeScript** — this is the hardest open
   design point (options: implement one in the ts target, scope the ts target
   to non-executing validation of migrations, or drive ts-side execution
   through one of the other targets).
2. **An expression language for value transforms.** The superseded tool uses
   CEL. Requirement: one expression language with consistent semantics across
   all three targets (CEL has implementations of varying maturity per
   language; adopting a subset or an alternative is part of the design).
3. **Multi-file transactional writes** (all-or-nothing across files).
4. **Dry-run preview** and **rollback via down-ops** (strictspec already
   classifies down-ops total/partial; execution semantics must honor it).
5. **Conformance porting.** The superseded tool ships a numbered conformance
   suite covering its ops (structure ops, data ops, matching, rollback,
   round-trip byte expectations). Port these cases into strictspec's
   conformance corpus rather than rediscovering the edge cases they encode.

Retirement sequencing (per ecosystem supersession policy): design + implement
here → port conformance → migrate the tool's consumers → delete the tool.
Retirement itself is out of scope for this todo; it happens in the superseded
project once this work is demonstrably complete.

## Solutions

**Option A — full absorption into the migration op language + targets
(decided direction).** One op vocabulary (strictspec's, extended where the
ported conformance cases expose gaps), execution implemented per target.
Pros: single migration language ecosystem-wide. Cons: largest scope; ts
execution question must be settled.

**Option B — absorption with execution scoped to Go and Python targets
initially.** ts target validates migrations but does not execute them until a
comment-preserving ts editor exists. Pros: unblocks supersession without
inventing a ts editor. Cons: asymmetric targets; must be an explicit
documented limitation, not a silent one.

## Affected files

- `spec/` (migration op semantics, expression language, execution model)
- `conformance/fixtures/` + ported conformance cases
- `go/`, `python/`, `ts/` targets (execution engines)

## Effort

Large — the biggest single item in the profile adoption. Design round first
(expression language, ts stance, op-vocabulary gap analysis against the
ported cases), then per-target execution engines.
