# stricttoml profile: spec document + check families

## Context

The ecosystem has adopted, as official policy, a TOML usage profile named
**stricttoml**: standard TOML 1.0 syntax, standard parsers, with strictspec
enforcing a set of conventions as hard checks. strictspec keeps its name (an
explicit rename decision was considered and rejected — strictspec validates
JSON-syntax documents too); the profile is a named rule set living in this
project's `spec/`.

Decision provenance: profile-as-policy was a recommended-pick (weakly held);
depth rule N=4 and spec-first canonical form were deliberate user choices.

## Problem

The profile currently exists only as a session decision record. It needs a
spec document and enforcing check families before any other project can adopt
or be assessed against it.

## Decided scope (all hard errors, no warnings, no bypass flags)

1. **TOML 1.0 syntax pin.** Documents must use 1.0-only syntax. TOML 1.1
   (released 2025-12: multi-line inline tables, trailing commas in inline
   tables, `\xHH`/`\e` escapes, secondless datetimes, non-ASCII bare keys) is
   rejected. Rationale: mixed parser fleet — some ecosystem parsers accept 1.1
   silently, stdlib `tomllib` rejects it; TOML files carry no version
   declaration, so pinning can only be enforced externally. 1.1 adoption is
   possible later only as a deliberate project if a concrete need appears.
2. **Canonical form, written spec first.** Author the canonical formatting
   specification (table style standard-vs-inline policy, key ordering,
   datetime precision — always write seconds, string style, indentation) in
   `spec/`; formatters elsewhere in the ecosystem then conform to it. The spec
   is the authority; a byte-level "file equals canonical form" check follows.
3. **Depth rule, N=4.** A schema with recursive types or nesting deeper than
   4 levels in a TOML-syntax document is a meta-schema error; such schemas
   must declare `document_syntax = "json"`. TOML for flat-ish declarations,
   JSON syntax for trees, one schema language over both.
4. **Coverage manifest + waivers, per repo.** Every `*.toml` in a repo must
   either match a manifest entry (glob → schema or owning validator) or appear
   in a waiver with a mandatory reason; anything unclaimed is a hard error.
   The manifest/waiver pattern already exists in the conformance acceptance
   corpus — promote it to a first-class per-repo mechanism with a check
   command. Validation-by-owner: each format-owning tool imports strictspec
   targets and validates its own files; the coverage check only verifies every
   file is claimed. Foreign checkouts are excluded by directory scope, not
   per-file waivers.

The optionality ban and first-class none construct are a separate todo (they
change the meta-schema itself).

## Solutions

**Option A — spec document first, then checks in one batch (decided
direction).** Write the full profile spec in `spec/`, then implement the four
check families against it across all targets with valid/reject fixture pairs.
Pros: checks verifiable against a written authority; coherent. Cons: largest
single effort.

**Option B — checks incrementally, spec grows alongside.** Land syntax pin
first (smallest), then coverage, then depth, then canonical form. Pros:
earlier partial enforcement. Cons: spec-vs-check drift risk during the
rollout; canonical form blocked on the spec anyway.

## Affected files

- `spec/` (new profile spec document; canonical-form spec)
- `conformance/fixtures/` (valid + reject pairs per check family)
- `go/`, `python/`, `ts/` targets (check implementations)

## Effort

Spec authoring: 1–2 sessions (canonical form is the bulk). Checks: syntax pin
small; depth rule small (meta-schema level); coverage medium (new manifest
format + command); canonical-form check small once the spec exists. Fixtures
throughout.
