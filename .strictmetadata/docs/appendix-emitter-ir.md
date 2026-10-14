+++
description = "The shared emitter IR: the single intermediate representation the reference interpreter and the generated Go validators both execute, with its node set, generation scheme, and stability policy."
+++
# Appendix: The Shared Emitter IR (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). The IR is the
> mechanism behind the identity guarantee (decision 16; decision 23). VERSIONED: any change to
> the IR node set or the generation scheme is a breaking-class, changelog-covered release event
> that triggers a full review of the validation fixtures.
>
> META NOTE: strictspec is in its GROWTH PHASE — the IR node set is STABLE BUT GROWING;
> refinements are normal and expected, recorded per-release through the existing discipline, with
> released-surface compatibility governed by semver at release boundaries.

The shared emitter IR is the single intermediate representation the reference interpreter and
the generated Go validators both execute. This appendix sketches its purpose, node set, generation
scheme, and — importantly — what is deliberately EXCLUDED from it.

## 1. Purpose and the identity argument

strictspec guarantees ordered VERDICT + CODE + PATH + MESSAGE-TEXT identity between the
generated Go validators and the internal interpreter. The IR is HOW that guarantee is
mechanically achieved:

- ONE template source: every diagnostic's prose lives once, in `appendix-error-codes.md`.
- ONE rendering pin: every slot value renders per `appendix-rendering.md` (value rendering, path
  grammar, did-you-mean), pinned once.
- ONE shared IR: the interpreter and every generated validator compile the schema into the same
  IR and run the same executor, so they execute the same checks in the same order and emit the
  same codes at the same paths with the same rendered messages.

The identity argument: single template source + pinned rendering + shared IR ⇒ byte-identical
messages. There is no hand-written message string anywhere and no hand-written check-ordering.

### 1.1 The common-mode-bug risk and its countermeasure

A shared IR carries a specific risk: a BUG IN THE IR ITSELF (or in the shared rendering pin)
would make the interpreter and the generated validators AGREE ON A WRONG ANSWER — a
common-mode defect that comparing them cannot catch, because both are derived from the same
faulty source. The countermeasure, the fixture-authoring discipline of the validation fixtures
(`go/testdata/fixtures/`):

- Expected fixture outcomes are HAND-AUTHORED FROM THE SPEC, never regenerated from the
  toolchain and never from the IR.
- A spec-derived, hand-authored expectation is the only check that can catch a wrong answer
  everything IR-derived agrees on. This is why fixtures carry codes, paths, and slot values (not
  embedded prose) and why the fixture runner renders the expected message from the pinned
  template independently.

## 2. The IR node set

The IR is a small, closed set of nodes. Two families: SCHEMA-SHAPE nodes (what to check) and
DIAGNOSTIC-EMISSION nodes (what to report). Nodes are pure descriptions — they contain no target
code and no expressions (section 4).

### 2.1 Schema-shape nodes

| Node | Role |
|---|---|
| record-open / record-close | enter/exit a closed record scope; record-close enforces the unknown-key invariant and required-field presence |
| key-presence | test presence/absence of a declared field (required, optional, alias-aware) |
| type-dispatch | dispatch on a value's node kind / lexeme class to the declared scalar or container check |
| union-dispatch | select a union arm (discriminated: by discriminator literal; node-kind: by node kind); non-matching arms are never entered |
| scalar-check | validate a scalar's lexeme class, datetime kind, safe-integer bound, number representability, and any custom-scalar lexeme rule |
| constraint-eval | evaluate one constraint-vocabulary form (intra- or cross-document) over the typed containing record and, for cross-document forms, resolver evidence. The form is selected from the CLOSED vocabulary — including the gated forms conditional-required / forbidden-when / conditional-value (each over the closed six-kind condition set: present, absent, equals-literal, not-equals-literal, in-literal-set, not-in-literal-set), the set forms mutual-exclusion (field-level) and collections-disjoint (element-level), unique-by / pairwise-distinct / ranges-disjoint / ordered-pair, and the cross-document forms including count-limit / sum-limit — never an expression tree |
| depth-guard | enforce the pinned maximum validation depth on recursive descent |
| alias-resolution | canonicalize an alias spelling; detect both-present |
| version-gate | run the `format_version` gate (documents) or `meta_version` gate (schemas) FIRST, before structural checks |
| import-resolution | resolve imported named types (no transitive imports, no cross-file constraints) |
| enum-arm-table | test enum membership against the BAKED arm set (document-sourced arms are baked at gen time) |

### 2.2 Diagnostic-emission nodes

| Node | Role |
|---|---|
| emit(code, slot-bindings) | append one diagnostic: a `STRICTSPEC_*` code plus its slot bindings, from which the target's generated renderer produces the pinned message text |
| path-push / path-pop | maintain the current path (key steps, index steps, map-key steps, arm steps) as traversal descends and ascends, per the path grammar |
| one-pass-accumulate | accumulate diagnostics in emission order within a phase (all-errors-in-one-pass per phase); phase-2 diagnostics follow phase-1, ordered by traversal then check-declaration order |

Ordering is a PROPERTY OF THE IR, not of any caller: the IR fixes the traversal and emission
order once, so the interpreter and every generated validator accumulate diagnostics in the
identical order (fixture-asserted emission order; renderers may not reorder).

## 3. The generation scheme

- The templates in `appendix-error-codes.md` are COMPILED into a renderer table: for each
  `STRICTSPEC_*` code, an entry that takes the slot bindings and produces the pinned message
  text, using the implementation of the rendering pin (`appendix-rendering.md`).
- WHERE THE RENDERER TABLE LIVES: the renderer table is part of the RUNTIME, generated ONCE by
  the toolchain's OWN codegen (tools/gencodes) from the catalogue (`appendix-error-codes.md`)
  when the runtime is built — NOT emitted per consumer `gen` run. A consumer's generated
  validator IMPORTS the renderer table from the runtime it is paired to (the generated-code
  format pairing guard); it does not carry its own copy. This keeps every consumer of a given
  release byte-identical in rendering (the renderer is shared, not re-derived) and is why the
  whole catalogue can change with a release without touching a single consumer's generated code.
- A generated validator embeds its schema and compiles it into the IR when it loads: record
  scopes, dispatches, scalar checks, constraint evals, and the gate, in the IR's fixed order.
- NO hand-translated message strings exist ANYWHERE — not in the runtimes, not in generated
  code, not in the interpreter. Every message is rendered by a generated renderer table entry
  from a template. This is the concrete meaning of "generators emit each runtime's renderer FROM
  the templates" (decision 16).
- The internal interpreter is driven from the SAME IR, so it runs the identical checks.
- Determinism: the emitter is strictspec's own canonical formatter (decision 18); regenerating
  twice is byte-identical (asserted by the emitter's determinism test).

## 4. What is deliberately NOT in the IR

The IR's smallness is a design guarantee, not an accident. Excluded, by declaration:

- NO EXPRESSIONS. The IR has no arithmetic, no computed values, no general boolean expression
  language. Constraint forms are a CLOSED vocabulary of named nodes (`constraint-eval` selects
  one), never an expression tree; a gate CONDITION is one of the closed six kinds (present /
  absent / equals-literal / not-equals-literal / in-literal-set / not-in-literal-set) over
  literals, never a computed predicate — which is exactly why numeric comparison predicates were
  rejected (they would need an arithmetic/relational expression node). This mirrors the migration
  engine's admission criterion (ops never compute a value from a value) and the language's
  rejection of CEL-class open computation (decision 23).
- NO CONSUMER HOOKS. There is no plugin node, no callback node, no registration surface in the
  IR. Consumer-native checks run DOWNSTREAM of validation in consumer code — they are never
  compiled into the IR and are outside the identity guarantee by declaration.
- NO TARGET-SPECIFIC NODES. Every node is language-neutral; a node whose behavior depended on
  where it ran would defeat the identity argument.
- NO SEVERITY. There is no warning node and no severity field — every emitted diagnostic is an
  error (decision 12).

The exclusions are what keep the identity guarantee TRACTABLE: a closed, expression-free,
hook-free, target-neutral node set is small enough to implement once and to audit against
hand-authored spec fixtures.

## Amendment log

- 2026-07-27 — §3: clarified WHERE the per-target renderer table lives. It is part of the
  target's RUNTIME, generated once by the toolchain's own codegen from the catalogue when the
  runtime is built, and IMPORTED by consumers' generated validators — never emitted per consumer
  `gen` run. "Compiled per-target" means one renderer table per target language, not one per
  generated artifact. No node-set or semantic change; a wording pin removing a per-consumer-gen
  reading of the generation scheme.
- Go only — the Python and TypeScript runtimes and generation targets are deleted, so the
  identity guarantee holds between the reference interpreter and the generated Go validators,
  and the renderer table is the Go runtime's. No node-set or semantic change.

## Cross-references

- The templates the IR compiles into renderer tables: `appendix-error-codes.md`.
- The rendering pin the generated renderers implement: `appendix-rendering.md`.
- The constraint forms `constraint-eval` selects and their semantics: `appendix-semantics.md`.
- The concrete surface authors write these forms in: `appendix-surface-syntax.md`.
- Custom-scalar checks compiled into `scalar-check`: `appendix-custom-scalars.md`.
- The fixtures that guard against common-mode IR bugs: `go/testdata/fixtures/`.
- The constitution (decisions 16, 18, 23): `DESIGN.md`.
