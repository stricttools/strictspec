# Union arms that match a set of discriminator values and share a base record

## Context

A discriminated union today names a discriminator field, and each arm is a
complete record that carries the discriminator as a single `literal` field
(`.stricttools/docs/appendix-surface-syntax.md`, the `discriminated-union`
entry). An arm therefore matches only one discriminator value, and fields
common to every arm are restated in each arm, because there is no way for arms
to share fields.

## Problem

Unions whose arms differ in one or two fields, across many discriminator
values, become large and repetitive. The motivating case is a migration record
in pgdesign: a column's `default` must be an integer when `type` is `int2`,
`int4`, or `int8`, a string when `type` is `text` or `varchar`, a boolean when
`type` is `bool`, and so on. Modelled today, that needs one arm per column
type (dozens), each a full copy of the record (`op`, `table`, `column`, `type`,
`nullable`, `default`, ...), differing only in the kind of `default`. That
duplication is what schemas exist to remove, so the format is left to validate
the default by hand instead (pgdesign stores defaults as strings and parses
them against the column type in its own code).

## Solutions

### A. Both: value-set arms and a shared base record (recommended)

- An arm declares the set of discriminator values it matches, for example
  `values = ["int2", "int4", "int8"]`, instead of one literal. The sets of all
  arms must be disjoint; overlap is a schema error naming both arms. A value in
  no arm's set is the existing "discriminator not a declared literal"
  diagnostic, listing every accepted value.
- The union declares a base record whose fields every arm inherits; an arm
  states only the fields it adds or whose type it narrows. Redeclaring a base
  field with a different type is allowed only as a narrowing and is otherwise a
  schema error.
- Pro: the migration case becomes one base record plus four small arms, each
  stating only the kind of `default`.
- Pro: every large union in the family shrinks the same way.
- Con: two features in the IR, the reader, three runtimes, the emitters, the
  generated bindings, the JSON Schema export, and the conformance cases.

### B. Value-set arms only

- Pro: half the work; the arm count drops from one per value to one per group.
- Con: each arm is still a full copy of the shared fields.

### C. Shared base record only

- Pro: removes the field duplication.
- Con: still one arm per discriminator value.

## Affected files

- `.stricttools/docs/appendix-surface-syntax.md` and `DESIGN.md`: the union
  construct, the diagnostics, and the path grammar for arm-matched values.
- `go/internal/ir/` and `go/internal/schema/`: arm value sets, the base record,
  the disjointness and narrowing checks.
- `go/internal/emit/` and the generated bindings, which must expose the shared
  fields on every arm.
- `go/internal/export/jsonschema.go`: `enum` inside `if`/`then` or `allOf` for
  the base record.
- The Python and TypeScript runtimes, in lockstep.
- `conformance/fixtures/`: cases for matching, overlap, unmatched values,
  narrowing, and a refused widening.

## Effort

Solution A: about a week for Go with conformance cases, plus a week for Python
and TypeScript parity. Solution B alone: about half of that.
