# First-class none construct; ban `required = false`; remove `nullable`

## Context

Ecosystem decision (part of the stricttoml profile adoption): optionality by
omission is banned. TOML has no null, and absence-as-default conflates
"deliberately no value" with "forgot the key." The remedy is stronger than
null: every key is required, and "no value" is a typed, schema-checked,
explicit value.

Decision provenance: the outright ban and the first-class construct were
recommended-picks confirmed by the user; the user additionally directed that
ecosystem migration impact be assessed per-project (separate effort, not this
todo).

## Problem

Three coupled meta-schema changes:

1. **First-class none construct.** The meta-schema gains a native
   explicit-absence type: a union-branch construct with a fixed surface form
   in documents (e.g. `retry = { kind = "none" }` — exact surface form is a
   design detail of this work, but it must be one fixed spelling, work for
   every value type, and allow variants to carry payloads). Validators know it
   natively; it is not a per-schema convention.
2. **Ban `required = false`.** Optional fields become a meta-schema error
   (a `STRICTSPEC_SCHEMA_*` diagnostic with a reject fixture). Absence of any
   schema-declared key in a document is a validation error, always.
3. **Remove the `nullable` type.** With first-class none, `nullable` is a
   second way to say "no value" — exactly the ambiguity being eliminated. Per
   the no-backward-compat policy for pre-stable projects: delete it and update
   every schema and fixture that uses it (currently used in conformance
   fixtures, including JSON-syntax documents — those migrate to none unions).

Note: existing fixtures use `required = false` (e.g. the meta-schema rejection
fixtures) and `nullable`; this work includes migrating all in-repo schemas and
fixtures. No transition period, no dual recognition.

## Solutions

**Option A — one batch (decided direction, most correct).** Design the none
surface form, land construct + ban + nullable removal + full fixture migration
together across meta-schema and all three targets. Pros: the meta-schema never
passes through an inconsistent intermediate state; single pass per file. Cons:
large changeset.

**Option B — construct first, ban later.** Ship none, migrate schemas
opportunistically, flip the ban when clean. Pros: smaller steps. Cons: an
interim state where both optionality styles are legal — the exact multi-pass
pattern the ecosystem's refactoring policy forbids.

## Affected files

- Meta-schema surface (`spec/appendix-surface-syntax.md` and the shipped
  meta-schema artifact)
- `conformance/fixtures/` (every schema using `required = false` or
  `nullable`; new valid/reject pairs for none and for the ban)
- `go/`, `python/`, `ts/` targets

## Effort

Surface-form design: small but consequential (one session including the
decision record). Implementation: medium per target. Fixture migration:
mechanical but wide.
