# `intra-document-references` parses `resolves_by` and then ignores it

## Context

A consumer schema was drafted against `spec/appendix-surface-syntax.md` §5.1, which lists
`intra-document-references` with the operand keys `reference`, `resolves_into`, `resolves_by`.
That consumer's document is a single root record holding several sibling collections — sources,
named effect stacks, time grids, style records — and **every one of them is keyed by a field
called `id`**, with cross-references from other parts of the document naming those ids. The
schema therefore declares fifteen constraints of the form:

```toml
[[types.Cut.constraints]]
form = "intra-document-references"
reference = "source_id"
resolves_into = "sources"
resolves_by = "field:id"
```

## Problem

`resolves_by` is read into the constraint model and never used again. The membership set is built
by a resolver that hardcodes the key field to `name`:

- `go/internal/ir/constraints.go` — `rootKeyset()`: for an array of records it does
  `entryOf(elem, "name")`; `intraReferences()` never looks at `c.ResolvesBy`.
- `python/src/strictspec/_ir.py` — same function, same hardcoded `_entry_of(elem, "name")`.
- `ts/src/ir.ts` — same, `entryOf(elem, "name")`.
- `go/internal/schema/read.go` + `model.go` (and the python/ts equivalents) parse `resolves_by`
  into the model, so the operand travels all the way to a resolver that discards it.

Consequences, in order of severity:

1. **False positives on valid documents.** A collection whose elements have no `name` field
   produces an EMPTY key set, so every reference into it is reported
   `STRICTSPEC_INTRA_REFERENCE_UNRESOLVED`. In the consumer above, each valid document draws
   between 20 and 66 spurious hard errors, which makes the generated validator unusable as a load
   gate — the exact use it exists for. There is no per-constraint opt-out; the only escape is
   `--structural-only`, which also disables every other phase-2 form.
2. **Silent non-checking in the other direction.** A schema that writes
   `resolves_by = "map-key"` or `"node-kind-union-key"` (both spellings appear in
   `conformance/fixtures/_schemas/` and `examples/`) gets whatever the hardcoded resolver happens
   to do, not what it declared. Where the shapes coincide the check passes for the wrong reason.
3. **The operand looks load-bearing and is not.** Three different values of `resolves_by` are
   already in the corpus; a reader has no way to tell that only one of them is honoured, because
   the fixtures that exercise it happen to key on `name`.

This is a documented-surface/implementation divergence, and by the project's own philosophy it is
the bad kind: an operand that is accepted, ignored, and produces a wrong verdict rather than an
error.

## Options

### A. Honour `resolves_by` in all three runtimes (recommended)

Make `rootKeyset` take the constraint and dispatch on the declared value:

- `field:<f>` — array of records, key set = the `<f>` field of each element (today's behaviour is
  the special case `field:name`).
- `map-key` — record/map node, key set = the entry keys.
- `node-kind-union-key` — array whose elements may be a bare string OR a record: bare strings join
  the set directly, records contribute their key field.

Make the operand REQUIRED and validate its spelling at schema-authoring time, so an unknown or
absent `resolves_by` is a hard error rather than a guess.

- Pros: matches the pinned surface; removes the false positives; makes the three corpus spellings
  mean what they say; no new vocabulary; the fix is one function per runtime plus its schema-time
  validation.
- Cons: making the operand required is a narrowing edit for any existing schema that omitted it,
  so it wants a `meta_version` consideration and a fixture regeneration pass. `field:<f>` needs a
  decision on what happens when an element lacks `<f>` (recommendation: skip that element, exactly
  as the current code skips a record without `name` — a missing key field is a structural error the
  record's own required-field check already reports).

### B. Honour `resolves_by` but keep it optional, defaulting to `field:name`

- Pros: no existing schema changes verdicts; smaller diff.
- Cons: a silent default for a value that decides whether a check is correct is exactly the
  implicit-default pattern the project bans elsewhere. A typo'd operand keeps resolving against
  `name` and keeps lying.

### C. Drop `resolves_by` from the surface and pin the key field to `name`

- Pros: honest about the implementation; smallest code change (delete the parse).
- Cons: makes the form unusable for any document that keys on `id`, `key`, `slug` … which is most
  of them; `map-key` cases in the current corpus would need a different form entirely. This is a
  large capability loss to fix a small bug.

### D. Leave it; tell consumers to key collections on `name`

- Pros: zero work.
- Cons: the schema language dictating a consumer's field names is backwards, and pre-existing
  formats cannot rename their key field without a breaking document migration.

## Affected files

- `go/internal/ir/constraints.go` (`rootKeyset`, `intraReferences`)
- `python/src/strictspec/_ir.py` (same two functions)
- `ts/src/ir.ts` (same two functions)
- `go/internal/schema/read.go`, `go/internal/schema/model.go` (+ `python/src/strictspec/_schema.py`,
  `ts/src/schema.ts`) if `resolves_by` becomes required/validated at authoring time
- `conformance/fixtures/` — new fixtures for `field:<f>` with `f != "name"`, for `map-key`, and for
  `node-kind-union-key`; the existing `field:name` fixtures pin only the coincidental case
- `spec/appendix-error-codes.md` if an authoring-time code for a bad/missing `resolves_by` is added
- `spec/appendix-semantics.md` §3.24, which currently states the form's semantics without saying
  how the target key set is derived

## Effort

Option A: roughly half a day. The resolver is ~25 lines per runtime and the three implementations
are line-for-line parallel; most of the time goes into the fixtures (three new resolution shapes ×
valid/invalid) and, if the operand becomes required, the authoring-time validation plus its
diagnostic.
