# Appendix: The Shared Emitter IR (normative)

> NORMATIVE STATUS: Part of the strictspec constitution (see `DESIGN.md`). The IR is the
> mechanism behind the four-target conformance guarantee (decision 16; decision 23). VERSIONED:
> any change to the IR node set or the generation scheme is a breaking-class, changelog-covered
> release event that triggers full conformance-fixture regeneration.
>
> META NOTE: Under the soft-freeze regime, pre-release refinements to the IR node set are
> expected and recorded per-release. The binding freeze is the first release.

The shared emitter IR is the single intermediate representation from which strictspec generates
every target's validator AND renderer. This appendix sketches its purpose, node set, generation
scheme, and — importantly — what is deliberately EXCLUDED from it.

## 1. Purpose and the identity argument

strictspec guarantees ordered VERDICT + CODE + PATH + MESSAGE-TEXT identity across all four
targets (generated Python, Go, TypeScript, and the internal interpreter). The IR is HOW that
guarantee is mechanically achieved:

- ONE template source: every diagnostic's prose lives once, in `appendix-error-codes.md`.
- ONE rendering pin: every slot value renders per `appendix-rendering.md` (value rendering, path
  grammar, did-you-mean), pinned once.
- ONE shared IR: every target's validator and renderer is GENERATED from the same IR, so all
  four execute the same checks in the same order and emit the same codes at the same paths with
  the same rendered messages.

The identity argument: single template source + pinned rendering + shared IR ⇒ byte-identical
messages across four targets. There is no per-target hand-written message string anywhere; there
is no per-target hand-written check-ordering.

### 1.1 The common-mode-bug risk and its countermeasure

A shared IR carries a specific risk: a BUG IN THE IR ITSELF (or in the shared rendering pin)
would make all four targets AGREE ON A WRONG ANSWER — a common-mode failure that four-way
cross-target comparison cannot catch, because all four sides are derived from the same faulty
source. The countermeasure (per `conformance/DESIGN.md`, fixture-authoring discipline):

- Expected conformance-fixture outcomes are HAND-AUTHORED FROM THE SPEC, never regenerated from
  any target and never from the IR.
- A spec-derived, hand-authored expectation is the only oracle that can catch a wrong answer all
  four IR-derived targets agree on. This is why fixtures carry codes, paths, and slot values
  (not embedded prose) and why the harness renders the expected message from the pinned template
  independently.

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

Ordering is a PROPERTY OF THE IR, not of any target: the IR fixes the traversal and emission
order once, so all four targets accumulate diagnostics in the identical order (fixture-asserted
emission order; renderers may not reorder).

## 3. The generation scheme

- At `gen` time, the templates in `appendix-error-codes.md` are COMPILED PER-TARGET into
  renderer tables: for each `STRICTSPEC_*` code, a target-native function/table entry that takes
  the slot bindings and produces the pinned message text, using the target-native implementation
  of the rendering pin (`appendix-rendering.md`).
- The schema-shape IR is compiled per-target into the validator: record scopes, dispatches,
  scalar checks, constraint evals, and the gate, in the IR's fixed order.
- NO hand-translated message strings exist ANYWHERE — not in the runtimes, not in generated
  code, not in the interpreter. Every message is rendered by a generated renderer table entry
  from a template. This is the concrete meaning of "generators emit each runtime's renderer FROM
  the templates" (decision 16).
- The internal interpreter (the fourth target) is generated/driven from the SAME IR, so it is a
  genuine fourth implementation of the identical checks, not a privileged reference.
- Determinism: the emitters are strictspec's own canonical formatters (decision 18); regenerating
  twice is byte-identical (asserted by the conformance suite's artifact-determinism checker).

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
  compiled into the IR and are outside the conformance guarantee by declaration.
- NO TARGET-SPECIFIC NODES. Every node is language-neutral. There is no "Go node" or "Python
  node"; a node that behaved differently per target would defeat the identity argument. Per-target
  differences exist ONLY in the mechanical compilation of a node to native code, never in the
  node set or its semantics.
- NO SEVERITY. There is no warning node and no severity field — every emitted diagnostic is an
  error (decision 12).

The exclusions are what keep the four-target identity guarantee TRACTABLE: a closed, expression-
free, hook-free, target-neutral node set is small enough to implement identically four times and
to audit against hand-authored spec fixtures.

## Cross-references

- The templates the IR compiles into renderer tables: `appendix-error-codes.md`.
- The rendering pin the generated renderers implement: `appendix-rendering.md`.
- The constraint forms `constraint-eval` selects and their semantics: `appendix-semantics.md`.
- The concrete surface authors write these forms in: `appendix-surface-syntax.md`.
- Custom-scalar checks compiled into `scalar-check`: `appendix-custom-scalars.md`.
- The fixture-authoring discipline that guards against common-mode IR bugs:
  `conformance/DESIGN.md`.
- The constitution (decisions 16, 18, 23): `DESIGN.md`.
