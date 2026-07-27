# Gap note — aggregates-exercise (construct exercise: count-limit and sum-limit, decision 23)

Construct-only exercise drafted from the spec (examples/DESIGN.md draft 15). Deliverable is this
gap note. Exercises the two aggregate cross-document constraint forms with LITERAL bounds:
count-limit and sum-limit over a resolver-selected document collection.

## Files

- `schema-service.toml` — the per-service document schema (`name`, `image`, `memory_mb`,
  `replicas`).
- `schema-fleet.toml` — the fleet manifest schema; hosts three aggregate constraints over the
  sibling `services/*.toml` collection.
- `pass/` — fleet + 3 services: count 3, sum 2048. Passes count-limit (`>=1`, `<=8`) and
  sum-limit (`<=4096`).
- `fail-count/` — fleet + 9 services (each 256 MiB): count 9 > 8 violates count-limit; sum 2304
  stays under budget, so ONLY the count form fires.
- `fail-sum/` — fleet + 3 services (each 2048 MiB): sum 6144 > 4096 violates sum-limit; count 3
  is fine, so ONLY the sum form fires.

## Clean

- **Both aggregate forms express directly.** `form = "count-limit"` / `form = "sum-limit"`, a
  `compare` (`<=` / `>=`), a LITERAL `limit`, a `selection` resolved by the closed
  `documents-in(glob)` resolver, and (for sum) a `sum_field`. This matches the vocabulary table
  (DESIGN.md — Cross-field/cross-document vocabulary) and the semantics entries
  (appendix-semantics 3.25: "a collection's element count across evidence ≤ a LITERAL bound";
  "a summed field across evidence ≤ a LITERAL bound").
- **Literal-bounds-only is respected.** Every `limit` is a bare integer literal written in the
  schema — no expression, no computed bound (decision 23; appendix-semantics 3.25 "bound is
  LITERAL only — never computed"). There was no temptation to compute a bound; the fleet budget
  is genuinely a fixed number.
- **Both compare directions.** The fleet declares a `<=` count cap AND a `>=` count floor over
  the same selection; nothing in the notation obstructs multiple aggregate constraints sharing a
  selection.
- **Phase separation holds.** Each service document's phase-1 structural validation
  (`memory_mb` range, non-empty strings) is independent of the fleet's phase-2 aggregates; the
  aggregate runs over evidence supplied by the resolver only for the fleet record whose phase 1
  passed (DESIGN.md — Domain checks execution model).

### Expected diagnostics

- `pass/fleet.toml` — validates clean (with `--with-domain-checks`).
- `fail-count/fleet.toml`: `STRICTSPEC_CROSS_COUNT_LIMIT` · path `$` (the fleet root hosts the
  constraint) · slots `{actual: 9, source: "documents-in(services/*.toml)", limit: 8}`.
- `fail-sum/fleet.toml`: `STRICTSPEC_CROSS_SUM_LIMIT` · path `$` · slots `{field: "memory_mb",
  source: "documents-in(services/*.toml)", actual: 6144, limit: 4096}`.
- Environment without the resolver: `STRICTSPEC_CROSS_RESOLVER_UNAVAILABLE` · path `$` · slot
  `{source: "documents-in"}` — a hard error at check-execution time, never a skip (decision 4/23).

## Awkward — the selection-declaration ergonomics (the requested focus)

The construct works, but the SELECTION declaration surfaces three ergonomic questions the read
material does not fully pin. None blocks expression; all are worth a spec decision.

1. **Path anchoring of the glob.** `glob = "services/*.toml"` is resolved by `documents-in`
   relative to the fleet document's directory (my assumption). The spec names the resolver
   (`documents-in(glob)`) but does not pin the glob's ANCHOR (document dir? manifest dir? CWD?).
   For fleet manifests, "relative to the manifest document" is the only choice that makes a
   committed fleet portable, but it must be pinned — a CWD-relative anchor would make verdicts
   depend on where the CLI was invoked, violating determinism (DESIGN.md — no silent
   degradation). Recommend: pin `documents-in` globs as document-directory-relative.

2. **Selection uniform-schema assumption.** count-limit just counts resolved documents;
   sum-limit reads `sum_field` from EACH resolved document. sum-limit therefore silently assumes
   every document in the selection HAS a numeric `memory_mb`. If `services/*.toml` accidentally
   matches a document of a different schema (or a service doc predating `memory_mb`), what
   happens — hard error, skip, or zero? appendix-semantics 3.25 lists
   `aggregate-over-unresolved-selection` in the undecidability catalogue for the ANALYZER, but
   the RUNTIME behavior on a heterogeneous selection is unpinned. Recommend: pin that sum-limit
   over a selection containing a document lacking `sum_field` (or where it is non-numeric) is a
   hard error (a `CROSS`-area code), not a silent skip-or-zero — consistent with the
   "ranges-disjoint missing-bound source is a hard error" precedent (appendix-error-codes 13).

3. **Which document hosts the aggregate.** I put the constraints on a dedicated `fleet` manifest
   whose own fields are almost empty — the constraint ranges entirely over external evidence.
   The alternative (attaching them to the service schema so any service validation triggers the
   fleet-wide check) is expressible but pathological: N re-computations of the same aggregate and
   a diagnostic path pointing at an arbitrary member. The manifest-hosts-the-aggregate pattern is
   clearly correct; recommend the spec present it as the canonical shape for collection
   aggregates so consumers do not attach aggregates to member schemas.

## Inexpressible

- **Nothing inexpressible within the literal-bounds contract.** A computed bound (e.g. "sum of
  memory ≤ 80% of node capacity read from another document") is deliberately out of scope
  (decision 23) and correctly belongs to a consumer-native check over the typed values — not a
  gap, an intentional exclusion.

## Verdict

FINDINGS: 3 — RESOLVED (Phase 3.3).

## RESOLUTION (Phase 3.3)

- **F1 (`documents-in` glob anchor)** — ADOPTED, with a DIFFERENT anchor than this note guessed:
  the glob is anchored at the MANIFEST ROOT (not document-directory-relative), resolved in
  LEXICOGRAPHIC order (`spec/DESIGN.md` — Cross-document vocabulary; `appendix-semantics.md` 3.25;
  `appendix-surface-syntax.md` §5.1). Manifest-root anchoring keeps a committed fleet portable and
  invocation-independent. Deviation from the note's document-directory guess is deliberate.
- **F2 (sum-limit over a heterogeneous / `sum_field`-missing selection)** — ADOPTED. Pinned as a
  HARD ERROR, new code `STRICTSPEC_CROSS_SUM_FIELD_MISSING` — never skip-or-zero.
- **F3 (manifest-hosts-the-aggregate canonical shape)** — ADOPTED (documented as the canonical
  pattern in `appendix-semantics.md` 3.25 / `spec/DESIGN.md`).

VERDICT: RESOLVED (Phase 3.3).
