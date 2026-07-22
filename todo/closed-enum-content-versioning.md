# Closed-enum baking: covering data-content evolution with format_version

## Context

strictspec versions document **formats**: `format_version` gates the shape of a document
(which fields exist, their types), with an exact-match gate and declarative migrations at
every version boundary. It does not, by itself, version the **content** of the data inside
a conforming document.

A consumer project designing its machine-readable outputs as strictspec-schema'd formats
hit this distinction. Its configuration defines a vocabulary of identifiers (an enumerable
set of names, defined in one strictspec-schema'd TOML document) that downstream consumers
of its JSON output must interpret. Adding or removing a vocabulary entry is a data change —
the document still validates at the same `format_version` — yet downstream consumers need
to gate on exactly this kind of change.

## Problem

Enumerable identifier sets that are *data* in one document but *semantics* for consumers
of other documents evolve invisibly to strictspec's version gate. Out of the box, the
consumer project must hand-roll a parallel content-version field plus its own governance
(diff discipline, bump validation) — duplicating exactly the kind of versioning machinery
strictspec exists to own.

## The pattern

**Bake the vocabulary into the output schema as closed enums.** Instead of declaring the
identifier-bearing fields of the output format as open strings, generate their enum arms
from the vocabulary document. Then any vocabulary content change *changes the output
schema*, which is a format change, which bumps the output format's `format_version` — and
strictspec's exact-match gate, structured mismatch diagnostics, and migration checkpoints
govern content evolution end to end. Content drift between producer and consumers becomes
structurally impossible.

Trade-off (accepted, arguably desirable): every content addition, however harmless,
forces consumers through the version boundary. This is exact-match philosophy applied to
data semantics — stricter than semver-style "additive is compatible" tolerance, and
consistent with the ecosystem's exact-pairing / fix-forward rules.

## What strictspec could do about it

1. **Document the pattern** (smallest): an authoring-guide section in `spec/` describing
   closed-enum baking as the sanctioned idiom for content-versioned vocabularies, including
   the trade-off above and when open strings + a consumer-owned content-version field is
   the better fit.
   - Pros: zero language change; turns an ad-hoc trick into a citable pattern.
   - Cons: the enum-generation step itself remains consumer-owned and unspecified.
2. **Support the generation step** (larger): schemas are single-file with no
   imports/composition by locked decision, so enum baking implies a consumer-side generator
   that rewrites the schema file's enum arms from a data source. strictspec could specify
   how a *generated schema file* interacts with the drift gate (`check`), e.g. recognized
   generated-schema markers, or a first-class "enum arms sourced from document X" construct.
   - Pros: makes the pattern mechanical and drift-gated instead of convention.
   - Cons: touches the single-file/no-composition ruling; a language change with meta-schema,
     migration, and conformance cost.
3. **Do nothing**: leave it as consumer technique. The pattern works today with no
   strictspec change; this todo then only warrants the doc section (option 1) or closure.

## Affected

- `spec/` — authoring guide / patterns section (option 1); schema language + meta-schema
  (option 2).
- `examples/` — a paper example of a baked-enum schema pair (vocabulary document + output
  format) would make the pattern concrete.

## Effort

- Option 1: small — documentation only.
- Option 2: medium-large — language design, meta-schema change, conformance fixtures.
