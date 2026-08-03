# A dangling named-type reference is silently accepted by validate, gen and check

## Context

`spec/appendix-semantics.md` §3.3 says: "A dangling name is `STRICTSPEC_SCHEMA_UNKNOWN_TYPE_REF`."
The code exists in the catalogue. Nothing emits it.

## Problem

`go/internal/schema/read.go` — `parseType()` — resolves a `type = "<x>"` site by looking `<x>` up
in `complexKinds`; anything not found becomes `KindRef` with `Ref = "<x>"`. No pass afterwards
checks that the reference resolves against `s.Types`, the builtin scalars, or the registered custom
scalars. A misspelled or never-declared type name is therefore treated as a valid reference to a
type that does not exist, and:

- `strictspec validate` reports **OK** for a document whose field at that site holds literally
  anything — a nested table, an array, a string where an integer was meant. The site is not
  validated at all.
- `strictspec gen` emits a Go binding for the field typed **`string`**, silently, whatever the
  document actually holds at that path.
- `strictspec check` reports the generated file as **fresh**, with no blind-spot entry.

Minimal reproduction (a 15-line schema, verified against 0.1.0 built from source):

```toml
name = "Mini"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Root"
targets = ["go"]
description = "unknown type ref probe"

[types.Root]
type = "record"
[types.Root.fields.a]
type = "NoSuchType"
required = true
[types.Root.fields.b]
type = "integer"
required = true
```

```
$ printf 'format_version = 1\na = { anything = "goes", nested = [1,2,3] }\nb = 5\n' > doc.toml
$ strictspec validate mini.schema.toml doc.toml --with-domain-checks
doc.toml: OK
$ strictspec gen && grep -A3 'type Root struct' mini_gen.go
type Root struct {
	A string
	B int64
}
$ strictspec check
mini.schema.toml -> mini_gen.go: fresh
```

Why this matters more than an ordinary missing check: it is a SILENT, LOCAL loss of validation.
The schema still validates every other field, the tool still exits 0, the generated code still
compiles — so nothing signals that one field's entire accepted set has become "anything". A schema
author's typo, or a type deleted during a refactor while its references stay behind, turns a
strict field into an unchecked one, and the strictest reading of the project's own rules ("hard
errors, not warnings"; "no silent degradation") says this must fail at authoring time.

The same shape of hole is worth checking for at the same time: an inline site with a `type` naming
a complex kind that does not exist (`type = "recrod"`), an arm whose `type` names nothing, and a
custom-scalar reference whose registration is missing (that one DOES have a code,
`STRICTSPEC_SCALAR_UNKNOWN` — worth confirming it fires, since it lives in the same lookup path).

## Options

### A. Resolve-and-report pass after `ReadSchema` / `ResolveImports` (recommended)

Walk every type site of the schema (fields, items, values, arms, inner, tuple elements) after
imports are merged, and emit `STRICTSPEC_SCHEMA_UNKNOWN_TYPE_REF` for every `KindRef` whose name is
not a builtin scalar, a registered custom scalar, or a declared/imported named type. It runs where
the other authoring diagnostics already surface (`emit.LoadForEmit` → `check`/`gen`) and where the
embedded-schema compile surfaces them at runtime.

- Pros: closes the hole everywhere at once (validate, gen, check, embedded compile); one place;
  matches the documented code; needs no surface change.
- Cons: schemas in the wild that currently rely on the silent acceptance (a stub type name used as
  a to-do marker) start failing — which is the point, but it will surface as noise on first run.
  Custom-scalar resolution must be part of the same pass so the registry is consulted, not just
  `s.Types`.

### B. Report it only in `gen`/`check`, keep `validate` permissive

- Pros: smaller blast radius.
- Cons: `validate` is exactly where a wrong verdict is most damaging, and an embedded compiled
  schema in a generated validator would keep the hole at runtime.

### C. Emit a blind-spot inventory entry instead of an error

- Pros: visible without failing anything.
- Cons: a warning that consumers will not read; the project's stated position is that a warning is
  a bug in disguise.

## Affected files

- `go/internal/schema/read.go` (site walk; `parseType` already knows when it fell through to
  `KindRef`), `go/internal/schema/model.go` if the walk needs a shared visitor
- `go/internal/emit/load.go` (`LoadForEmit`) — where the new diagnostics join `sdiags`
- `python/src/strictspec/_schema.py`, `ts/src/schema.ts` — the parallel readers, which have the
  same fall-through
- `go/internal/emit/emit.go` — `goType()` currently falls back to `string` for an unresolvable ref;
  once the ref is an error this fallback should become an emitter-bug panic rather than a quiet
  default (same for the python and ts emitters)
- `conformance/fixtures/meta-schema/` — a rejection fixture per dangling-reference shape (field,
  item, value, arm, tuple element), plus one for the custom-scalar case
- `spec/appendix-error-codes.md` — confirm the code's slots/message match what the pass emits

## Effort

Half a day to a day. The walk itself is small; the cost is doing it identically in three runtimes
and adding the rejection fixtures. Worth pairing with an audit of the other "parsed but never
checked" schema-authoring keys, since this one was found by accident.
