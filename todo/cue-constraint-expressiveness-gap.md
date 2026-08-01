# CUE constraint-expressiveness gap analysis

## Context

strictspec is the schema/validation layer for an ecosystem standardizing on TOML
documents (with JSON document syntax available for deeply nested structures). CUE
was evaluated as the strongest external alternative to the whole approach. The
conclusion was to keep strictspec — CUE's evaluation model (unification, defaults,
non-local semantics) conflicts with the ecosystem's determinism philosophy — but
the comparison surfaced one area where CUE is clearly ahead: the expressiveness of
its constraint language. This todo is about closing that gap deliberately: audit
what CUE can express that strictspec cannot, adopt what fits the philosophy,
and explicitly reject what does not (recording the rejections as decisions).

## Current state (observed from conformance fixtures, verify against spec/)

- Type vocabulary: record, array, map, tuple, enum, literal, opaque (with
  mandatory stance), nullable, string, integer, float, number, boolean,
  datetime, date, time, identifier
- Constraints with min/max/limit forms
- Aggregates over collections (sum limits, count limits — see fleet fixtures)
- Unions, cross-file imports/shared types

## Problem

CUE expresses several constraint shapes that strictspec (apparently) cannot.
Each candidate below needs verification against the actual spec before being
treated as a real gap:

1. **Cross-field constraints within one document** — e.g. `start < end`,
   "exactly one of X and Y must be present", "A must equal B's length".
2. **Conditional requirements** — "if field X has value v, field Y is required".
3. **Richer value predicates** — string regex/pattern matching, string length
   bounds, multiple-of for numbers, array length bounds, array element
   uniqueness, map key patterns.
4. **Constrained-scalar disjunctions** — unions whose branches are constrained
   values, not just types (CUE: `(int & >=0) | "auto"`).
5. **Recursive type definitions** — trees referencing their own type; needed for
   deep JSON-syntax documents (scene graphs, ASTs).
6. **Quantified collection constraints** — "every element satisfies P",
   "at least one element satisfies P" (beyond sum/count aggregates).

## Explicitly reject (philosophy conflicts — record as decisions, not gaps)

- Default values via unification (violates no-implicit-defaults)
- Computed/derived values inside data documents (data must stay inert)
- Mixing schema fragments into data documents (schema and data stay separate
  document roles on purpose)

## Solutions

**Option A — full audit, then batch adoption (most correct).** Systematically
walk CUE's constraint documentation, produce an adopt/reject verdict per
construct with rationale, then land all adopted constructs together: meta-schema
surface, all language targets, and conformance fixtures (valid + reject pair per
construct). Pros: coherent constraint vocabulary designed once; rejections get
recorded alongside adoptions. Cons: largest single effort; some adopted
constructs may sit unused for a while.

**Option B — demand-driven adoption.** Implement only constructs an existing
schema concretely needs today; re-audit later. Pros: smallest immediate effort,
every construct has a real consumer. Cons: piecemeal vocabulary risks
inconsistent design between rounds; the audit gets repeated.

## Affected files

- `spec/` (meta-schema surface syntax, construct semantics)
- `conformance/fixtures/` (new `_schemas/` constructs, valid fixtures, reject
  fixtures with pinned diagnostics)
- `go/`, `python/`, `ts/` targets (validator implementations)

## Effort

Audit: about one session. Per adopted construct: meta-schema + three targets +
fixture pair — medium each. Total scales with adoption count (plausibly 4–8
constructs).
