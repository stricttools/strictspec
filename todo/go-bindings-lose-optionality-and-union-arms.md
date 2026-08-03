# Generated Go bindings cannot carry a consumer's in-memory model: absence collapses, union arms are untyped

## Context

A consumer with an existing hand-written Go loader evaluated replacing its document types with
`strictspec gen --targets go` output. Its format has two properties that are common and that the
project itself endorses: **no defaults anywhere** (an absent field means something different from
its zero value, by design — decision 30), and **discriminated unions everywhere** (four of its
collections are unions, and one of them nests unions two deep). The generated bindings could not be
adopted, for reasons that are about the emitter rather than about that format.

## Problem

### 1. Optional scalar fields bind to plain values, so absence is unrepresentable

`go/internal/emit/emit.go` — `recordType()`/`goType()` — emits every scalar field as its bare Go
type regardless of `required`. A `bindPtr` helper is emitted in every file and is not used for this
case. So a record with

```toml
[types.Sfx.fields.gain_db_x10]
type = "integer"
required = false
min = -600
max = 120
```

binds as `GainDbX10 int64`, and a document that omits the field is indistinguishable from one that
writes `gain_db_x10 = 0` — while in the schema those mean "unity, emit no gain range at all" and
"an explicit 0 dB range". The same collapse hits every optional boolean (`PopIn bool`,
`Smooth bool`) and every optional bounded integer whose range excludes 0 only by luck.

This directly contradicts the language's own no-defaults stance: the validator is scrupulous about
"the WRITTEN value only, no effective/default value", and then the binding it generates erases the
distinction it just enforced. A consumer reading the binding cannot even ask whether the key was
present.

### 2. Union-typed fields bind to `strictspec.Value`

A field or array item whose type is a `discriminated-union` or `node-kind-union` binds as raw
`strictspec.Value`:

```go
type Project struct {
	Sources  []strictspec.Value
	Timeline []strictspec.Value
	Captions []strictspec.Value
	...
}
```

The arms' own records ARE emitted as typed structs, and the discriminator literal is known at
generation time, so the type information exists — it is just not connected. Every consumer of a
union-dense document must therefore hand-write the arm switch and the per-arm bind call, i.e.
exactly the code generation was supposed to remove. For a document whose root is mostly unions, the
generated struct set is thinner than the hand-written one it would replace.

### 3. There is no generated write side

`go/internal/write` implements comment-preserving splice/serialize, but it is internal: neither the
public `go/strictspec` package nor the generated file exposes a way to render a validated document
back to its syntax. A consumer that edits documents (any authoring tool) keeps a hand-written
writer, which is the half most likely to drift from the schema — the read side is at least gated by
the validator, the write side by nothing.

Net effect: the generated artifact is excellent as a VALIDATOR (the shape gate, the diagnostics,
the byte-identical verdicts) and unusable as a MODEL. That is a fine outcome if it is the stated
one, but the emitter's doc comment advertises "immutable typed frozen structs per named record
type" as the binding surface, which reads as a model.

## Options

### A. Emit pointers for optional scalars; emit a typed arm sum for unions (recommended)

- Optional scalar field `f` of type `T` binds as `*T`, bound through the already-emitted `bindPtr`.
  Required fields keep their bare type, so presence is legible from the type.
- A discriminated union emits an arm struct plus a typed accessor set: an `Arm` string (the
  discriminator value) and `As<Arm>() (*<ArmType>, bool)` per arm, or a small interface with one
  implementation per arm record. A node-kind union emits the same shape keyed on node kind.
- Pros: the binding finally carries what the schema states; consumers stop hand-writing arm
  switches; presence and absence stay distinguishable, which is the whole point of having no
  defaults.
- Cons: breaking change to every generated file and every consumer of one (this is pre-1.0 and the
  regeneration is mechanical, but the diff is wide); the same change is owed to the python and ts
  emitters to keep the four-target story honest; `With*` helpers need pointer-aware variants.

### B. Optional-scalar pointers only; leave unions untyped

- Pros: fixes the semantic bug (absence) without touching the union emission; small diff.
- Cons: leaves the biggest ergonomic gap open; union-dense consumers still cannot adopt.

### C. Document the bindings as validator-only and stop calling them a model

- Pros: zero code; honest.
- Cons: gives up the codegen value proposition for exactly the formats that most need strictness.
  The information needed to do better is already in the schema.

### D. Add a generated write side (orthogonal, can follow A)

Expose `write.Serialize`/`Splice` through `go/strictspec` and emit a `RenderBytes` entry point, so
a consumer's save path is gated by the same schema as its load path. Pros: closes the drift hole
noted above. Cons: pulls the write-side format-version refusal rule (`spec/DESIGN.md` — Producers)
into the generated surface and needs its own conformance fixtures.

## Affected files

- `go/internal/emit/emit.go` — `recordType`, `goType`, `bindExpr`, `bindHelpers`, the `With*`
  emission
- `python/src/strictspec/…` and `ts/src/…` emitters — the parallel treatment, if verdict/binding
  parity across targets is to hold
- `go/strictspec/value.go`, `program.go` — if the arm accessors or a write door become public API
- `go/internal/write/*` — only for option D
- `conformance/fixtures/construct-coverage/` — fixtures asserting the generated shape for an
  optional scalar and for both union kinds; `go/internal/emit/targets_test.go`
- `go/DESIGN.md` / the generated-API-contract section of `spec/DESIGN.md` — the binding surface is
  described there and would change

## Effort

Option B alone: a few hours. Option A across the Go emitter with fixtures: one to two days; add
roughly the same again per additional target if parity is required now rather than later. Option D
is a separate, larger piece and should not be bundled with A.
