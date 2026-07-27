# Gap note — orxtra-workflow (stretch draft; examples/DESIGN.md draft 8)

The stretch draft: "if it fits, everything fits." Source: `orxtra/scheduler/_loader.py` and
`_validator.py`, and `orxtra/examples/*.toml`. Stresses recursion, execution-mode selection,
conditional requirements, intra-document dependency references, and — expected — the DAG cycle
check that exceeds the constraint vocabulary.

## Files

- `schema-workflow.toml` — the workflow schema; recursive `Task` type with phase-2 constraints.
- `feature-pipeline.valid.toml` — a valid workflow (adapted from `simple_workflow.toml`).
- `broken-pipeline.invalid.toml` — four catchable violations + one uncatchable cycle.

## Clean — what fit without strain

- **Recursion.** `Task.subtasks` is `array<Task>`, a direct self-reference (appendix-semantics
  3.4). The pinned max validation depth bounds it; a document nesting past the cap is
  `STRICTSPEC_DEPTH_EXCEEDED`. orxtra's `_validate_task` recursion into `subtasks` maps 1:1.
- **Execution-mode selection WITHOUT a literal discriminator.** orxtra has no `type = "..."`
  discriminator field; the mode is IMPLIED by which fields are present
  (`_count_execution_modes`). This is NOT a strictspec discriminated union (those need a
  literal-valued discriminator field; appendix-semantics 3.5). But it DECOMPOSES cleanly into two
  vocabulary forms:
  - `co-presence(agent, task_prompt)` — the one two-field mode (`_validate_agent_fields`: agent
    iff task_prompt).
  - `exactly-one-of(agent, callable, subtasks, wait_for, decision_point)` — one representative per
    mode (`agent` represents agent-mode).
  Together these yield "exactly one execution mode," matching `_count_execution_modes == 1`. A
  pleasant result: the presence-implied union needed no new construct.
- **Presence-triggered conditional-required.** agent-mode ⇒ `timeout` + `context_refinement`
  required; `for_each` ⇒ `for_each_abort_on_failure` + `max_concurrency` required. All are
  `conditional-required` with a `predicate = "present"` condition (appendix-semantics 3.24).
- **Intra-document dependency references.** `depends_on` entries resolve to `tasks[].name` via the
  `intra-document-references` form (appendix-semantics 3.24; `STRICTSPEC_INTRA_REFERENCE_UNRESOLVED`
  on a dangling ref). This is `_validate_dependencies`' existence half.
- **Enums, min_items, non-empty, ranges** — all direct (`escalation_policy`, `tasks` min 1,
  names non-empty, `timeout >= 1`).

### Expected diagnostics — `broken-pipeline.invalid.toml` (traversal order, item 6)

1. `STRICTSPEC_INTRA_EXACTLY_ONE_OF` · path `$.tasks[0]` · slots `{fields: ["agent","callable",
   "subtasks","wait_for","decision_point"], actual: ["agent","callable"]}`.
2. `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED` · path `$.tasks[1]` · slots `{key: "retry_resume",
   condition: "retry != 0"}`.
3. `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED` · path `$.tasks[1]` · slots `{key:
   "retry_inject_failure", condition: "retry != 0"}`.
4. `STRICTSPEC_INTRA_REFERENCE_UNRESOLVED` · path `$.tasks[2].depends_on[0]` · slot `{value:
   "ghost"}`.

The `loop-a`/`loop-b` cycle produces NO strictspec diagnostic — both references resolve. See
finding 4.

## Findings — where the spec's constraint vocabulary is stretched or exceeded

### Finding 1 — presence-implied unions are decomposable but the pattern should be documented

orxtra's task, imagine's ops, and pgdesign columns all use PRESENCE (not a literal discriminator)
to select a shape. Each decomposes into `co-presence` + `exactly-one-of`/`mutual-exclusion` +
`conditional-required`, as shown. This is expressible TODAY, but the decomposition is non-obvious
and easy to get subtly wrong (a consumer might reach for a discriminated union and fail because
there is no discriminator field). Recommend: the spec add a worked "presence-implied variant"
pattern to the union-diagnostics or constraint-vocabulary section, pointing consumers at the
decomposition rather than the discriminated-union construct. No new construct — a documentation
finding.

### Finding 2 — value-triggered conditional-required with a `>` predicate

The vocabulary table cites "orxtra retry>0 => retry_resume" as the ORIGIN of value-triggered
conditional-required (DESIGN.md — intra-document forms table). But appendix-semantics 3.24's
interaction note restricts the condition to "presence/equality only." `retry > 0` is an
INEQUALITY. This draft models it as `retry present AND retry != 0`, which is EXACT — but ONLY
because `retry`'s domain is non-negative (`min = 0`), so `> 0` ⟺ `!= 0`. A general value-trigger
like `budget > 100` or `priority >= 3` could NOT be expressed with presence/equality alone.

Recommend a spec decision, one of:
  (a) Confirm value-triggered conditional-required is EQUALITY-ONLY, and reword the vocabulary
      table's `retry>0` example to `retry != 0` (with a note that it relies on retry ≥ 0), so the
      cited origin is honest; OR
  (b) Extend the condition predicate set to include numeric comparisons (`>`, `>=`, `<`, `<=`
      against a literal), matching the vocabulary-table's "value-triggered" wording. This is a
      small, closed extension (literal comparisons only, no expressions — consistent with the
      aggregate literal-bounds rule).
The tension is real and currently unresolved in the read material; the draft flags it rather than
silently choosing.

### Finding 3 — computed-name disjointness (variable collision) is inexpressible → consumer-native

`_validate_variable_collisions` checks that no workflow variable collides with any task OUTPUT
name, where output names are COMPUTED as `{task.name} + suffix` for suffixes `_output`, `_text`,
`_result`. The constraint engine never computes values from values (the admission criterion
mirror; appendix-semantics §1). A disjointness check between a declared set and a set derived by
string concatenation is therefore OUTSIDE the vocabulary — `unique-by`/`pairwise-distinct` compare
existing values, they do not synthesize `name + "_output"`. This is correctly a consumer-native
check over the typed values (DESIGN.md — the consumer-native tail). EXPECTED and not a gap;
recorded so the boundary is on the record.

### Finding 4 — DAG cycle detection exceeds the vocabulary (the expected consumer-native tail)

`_validate_dependencies` runs `build_graph` + `topological_sort`, raising `CycleError` on a
cycle. The vocabulary has `intra-document-references` (does each reference RESOLVE) but NO
acyclicity / reachability / topological form. The `loop-a <-> loop-b` cycle in the invalid
document RESOLVES cleanly (both names exist) so strictspec accepts it; only a graph algorithm
detects the cycle. This is the expected consumer-native tail (the draft brief anticipates it):
cycle detection is bespoke graph analysis over the strictspec-validated typed values, emitting a
consumer-prefixed code (e.g. `ORXTRA_DEP_CYCLE`) via the runtime's diagnostic constructor,
downstream of validation (DESIGN.md — Domain checks; decision 23).

This is the RIGHT boundary — a general graph-property DSL hand-ported to four targets is exactly
the perpetual drift surface decision 23 rejected. Recording it confirms the vocabulary's edge:
references-resolve is portable; graph SHAPE (acyclicity, reachability) is not, and stays
consumer-native. NOT a construct gap; a boundary confirmation.

### Finding 5 — validation MODE (headless) is not a document property

`_validate_task(..., headless=True)` forbids `context_refinement`/`decision_point` when no
Overseer is configured. `headless` is a VALIDATE-CALL parameter, not a document field — the same
document is valid or not depending on an external mode. strictspec validates a document against a
schema; it has no "validation mode parameter" concept, and adding one would violate "the same
input produces the same behavior every time" (DESIGN.md — no silent degradation; imagine
principle 6). Correct modeling: either (a) two schemas (a `headless` variant that marks those
fields forbidden via `forbidden-when` with an always-true condition — awkward) or, better,
(b) headless enforcement stays consumer-native, since it depends on deployment configuration, not
document content. Recommend (b). Recorded as a modeling boundary, not a gap.

## Verdict

FINDINGS: 5

1. Document the presence-implied-variant decomposition pattern (co-presence + exactly-one-of) so
   consumers do not misreach for discriminated unions. (Doc)
2. Resolve the value-triggered conditional-required predicate tension: either reword the
   `retry>0` origin to `retry != 0` (equality-only) or add closed numeric-literal comparison
   predicates. (Spec decision — the one genuinely open item.)
3. Computed-name disjointness (variable collisions) is consumer-native by construction.
   (Expected boundary.)
4. DAG cycle detection is the expected consumer-native tail; the vocabulary correctly stops at
   references-resolve. (Boundary confirmation.)
5. Validation-mode (headless) is a deployment fact, not a document property; keep it
   consumer-native. (Modeling boundary.)

Only finding 2 is a genuine open spec decision; the rest are documentation or
boundary-confirmations. No new construct is required for the workflow schema to fit — the
recursive tree, execution-mode selection, conditional requirements, and reference resolution all
express within the current vocabulary.

## RESOLUTION (Phase 3.3)

- **F1 (presence-implied-variant decomposition)** — BOUNDARY-CONFIRMED. Expressible today via
  `co-presence` + `exactly-one-of`; documentation note, no construct.
- **F2 (value-triggered predicate tension, `retry > 0`)** — ADOPTED (equality-only). The closed
  condition set is literal-equality/membership only; NUMERIC COMPARISON predicates are REJECTED.
  The `retry>0` origin is reworded to `retry != 0` (with a note it relies on `retry >= 0`), so the
  cited origin is honest (`spec/DESIGN.md` — Condition set + vocabulary table). This draft uses
  `retry not-equals-literal 0`.
- **F3 (computed-name disjointness, variable collisions)** — BOUNDARY-CONFIRMED. Consumer-native
  (values-from-values is banned).
- **F4 (DAG cycle detection)** — BOUNDARY-CONFIRMED. The expected consumer-native tail; the
  vocabulary correctly stops at references-resolve.
- **F5 (validation-mode `headless`)** — BOUNDARY-CONFIRMED. A deployment fact, not a document
  property; consumer-native.

VERDICT: RESOLVED (Phase 3.3).
