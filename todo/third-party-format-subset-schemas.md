# Subset-schemas for third-party TOML formats

## Context

Ecosystem decision (stricttoml profile): every `*.toml` file in every repo is
either schema-bound or explicitly waived. That includes files authored in the
ecosystem but whose format is owned by third parties: `pyproject.toml` (PyPA
spec + tool-owned `[tool.*]` tables), `.gitleaks.toml` (gitleaks), and Gradle
version catalogs (`libs.versions.toml`). Decision: **subset-schema all of
them** rather than waiving — schema the keys the ecosystem relies on, declare
tool-owned tables as opaque leaves with recorded stances (the existing
opaque-with-stance mechanism, used exactly as designed).

Decision provenance: recommended-pick (weakly held).

## Problem

Three subset-schemas to author, of which pyproject is the valuable one:

1. **pyproject.toml.** Beyond shape-validation, this schema can mechanically
   enforce standing ecosystem policies that today exist only as prose rules:
   - `[tool.uv.sources]` entries using local `path = ...` references are
     banned in committed files (registry-pure lockfiles policy)
   - internal ecosystem dependencies must be unpinned (no version
     constraints on them)
   - `[project]` required keys present; `[tool.*]` tables opaque with
     `consumer_check` stances
   Turning prose policy into hard checks is the main payoff of this todo.
2. **.gitleaks.toml.** Small schema: allowlist structure, rule shape; rest
   opaque.
3. **Gradle version catalogs.** Small schema: `[versions]`, `[libraries]`,
   `[plugins]` shapes.

Constraint: subset-schemas validate the ecosystem's *usage* of these formats;
they must never reject files that are valid to the owning tool but merely use
features the schema doesn't model — unmodeled regions are declared opaque
with stances, not omitted (omission would make the schema a lie).

## Solutions

**Option A — all three now (decided direction).** Full coverage, no waiver
holes. Pros: complete; pyproject policy enforcement lands. Cons: two of the
three schemas are low-value ceremony.

**Option B — pyproject now, waive the other two with reasons.** Pros: nearly
all the value for a third of the work. Cons: permanent waiver entries; was
considered and not chosen.

## Affected files

- New schemas (location per the validation-by-owner model: these ship with
  whatever tool claims these files in repo manifests; if no owner tool fits,
  they ship here in strictspec as ecosystem-common schemas)
- `conformance/fixtures/` (valid + reject pairs, incl. a path-source-ban
  reject fixture)

## Effort

pyproject schema: medium (the policy constraints need the constraint
vocabulary from the CUE-gap audit — cross-field/conditional rules; sequence
after it). The other two: small.
