# Gap note — migration files (op-vocabulary fit)

Two migration drafts in the CLOSED 13-op set (add_field, remove_field, rename_field, move_field,
set_value, set_value_where, remove_where, add_collection, drop_collection, append,
merge_defaults, wrap_in_array, unwrap_singleton). Both are the spec's named flagship examples
(Versioning section / decision 9): the claudestream budget shape-op and the rlsbl dev_node
pure-rename chain. Both are EXAMPLES/FIXTURES — no live documents exist for either (the budget
field change happened in code long ago; the dev_node renames already shipped in rlsbl).

## What the op vocabulary expressed cleanly

- **Flagship shape-op (budget).** `rename_field` + `wrap_in_array` expresses
  `max_cost_usd: float -> cost_thresholds: [float]` in exactly two ops, neither of which computes
  a value from a value (rename carries the value verbatim; wrap injects array structure around the
  verbatim scalar). This sits squarely inside the admission criterion. The write side is a clean
  demonstration of the canonical-serialization rule: wrapping the float `5.0` writes `[5.0]`
  (float renders float-marked, never a bare `5`), and the output revalidates at v2 by
  construction.
- **PARTIAL down taxonomy.** The `wrap_in_array` op's inverse is `unwrap_singleton`, which is a
  canonical hard error (`STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON`) on any v2 document whose
  `cost_thresholds` has >1 element. Declaring the op `down = "partial"` (not `total`) is exactly
  right, and the empirical engine can VERIFY the taxonomy against a corpus: a v2 document like
  `samples/budget.v2.multi-element.json` is the corpus witness that down is partial, not total
  (a `total` declaration would be a `STRICTSPEC_DIFF_TAXONOMY_MISDECLARED` hard error). This is
  the pinned partial-down case working end to end.
- **Pure-rename chain (dev_node).** `internal -> changelog_exempt -> dev_node` is two independent
  `rename_field` migrations across two `format_version` bumps (v1->v2, v2->v3), each `down =
  "total"`. Renames are the simplest restricted-transducer ops (verbatim carry-over of the value
  under a new key); reversibility is total because nothing is dropped or reshaped. The chain also
  demonstrates that a migration SET is a sequence of single-version-step files.

## Findings

### FINDING 1 — migration-file surface syntax is unpinned (same class as the schema surface gap)
Just as spec/ pins the language but not the schema TOML surface (see sibling gap notes), it pins
the 13-op vocabulary and their semantics/collision rules but not the concrete TOML spelling of a
migration file (`[migration]` header keys, `[[ops]]` shape, how `down`/`partial`/`irreversible`
and a partial reason are written, how op targets are addressed — this draft uses the read-side
path grammar `$.budget.cost_thresholds`). Recommend the same resolution: a normative migration-
file surface appendix, or "the shipped built-in migration schema is the pinned surface, by
example." Migration files ARE documents of a toolchain schema (they get gated and migrated like
any document), so this surface deserves the same pinning as the meta-schema surface.

### FINDING 2 — every op needed here is a pure structural/rename op; no admission-criterion pressure
Neither flagship reaches for anything near the criterion's edge. Rename, wrap, unwrap are all
verbatim-carry or literal-structure ops. The retired value-computing ops (transform/CEL, raw,
merge_defaults_by_key) are not missed by either example — consistent with the verification that
no consumer used them. No new op is requested; the vocabulary fit is clean.

### FINDING 3 — the up-direction is fully expressible; the down-direction's per-op inverse is implicit
The draft declares each op's `down` taxonomy but writes the inverse op sequence only as a
reference comment (budget file). The spec says the taxonomy is VERIFIED EMPIRICALLY (corpus
round-trip), which implies the engine derives/executes the inverse from the forward op + its
taxonomy, rather than the author hand-writing a separate down-op list. That is a reasonable
reading, but the spec does not state whether a down-migration is auto-derived from the forward
ops or authored separately. Minor clarity item: state whether `down` is engine-derived (the
natural reading for these reversible ops) or author-supplied. For the two flagships the inverse
is mechanical (rename<->rename, wrap<->unwrap), so auto-derivation is the obvious choice; worth
one normative sentence.

## Expected behavior (sample documents)

- `samples/budget.v1.json` --(migrate v1->v2)--> `samples/budget.v2.expected.json`
  (`max_cost_usd: 5.0` becomes `cost_thresholds: [5.0]`; revalidates at v2). Soundness/
  completeness over a one-document corpus grade `corpus-supported`.
- `samples/budget.v2.multi-element.json` --(down v2->v1)--> HARD ERROR
  `STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON` · path `$.budget.cost_thresholds` · actual `3`
  elements. This is the corpus-witnessed proof that the `wrap_in_array` op's down is PARTIAL,
  not total (feeds `strictspec diff` down-taxonomy verification).
- `samples/workspace-project.v1.json` --(migrate v1->v2->v3)--> `samples/workspace-project.v3.expected.json`
  (`internal: true` -> `changelog_exempt: true` -> `dev_node: true`). Both steps `down: total`;
  round-trip is clean, grade `corpus-supported`.

## Verdict

FINDINGS: 3 (1 shared surface-syntax gap, now spanning migration files too; 1 confirmation the
op vocabulary fits both flagships with zero pressure; 1 minor clarity item — whether `down` is
engine-derived or author-supplied). The closed 13-op set expresses both flagship migrations
completely, including the pinned partial-down hard-error case. No new op is needed.
