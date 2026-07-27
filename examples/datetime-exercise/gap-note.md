# Gap note — datetime-exercise (Phase 3.3 coverage closure, cluster 5b)

Construct-only exercise added in Phase 3.3 to CLOSE the datetime-scalar coverage gap batch 2
flagged: the rlsbl release-file draft (examples/DESIGN.md row 5 anticipated a datetime stress) turned
out to carry no `date`/`time`/`datetime` field, so no adoption-facing draft exercised the datetime
scalar set directly. `Instant` (offset `datetime`) is exercised by the betterclaude contracts and
imagine, but `date`, `time`, and LOCAL `datetime` were not. This tiny schema exercises all of them.

## Files

- `schema-datetime.toml` — one record with a `date`, a `time`, an OFFSET `datetime` (carrying a
  same-kind `[min, max]` range), and an optional LOCAL `datetime`.
- `valid-01.json` — validates clean.
- `invalid-01-kind-and-range.json` — `recorded_at` is a LOCAL-datetime lexeme in an OFFSET-typed
  field (kind mismatch) AND before the range minimum.

## Coverage confirmed

- **`date` / `time` scalar kinds** — declared and exercised (`day`, `wall_clock`).
- **`datetime` offset vs local** — both kinds declared; the offset lexeme is retained (a `+00:00`
  is never rewritten to `Z`; primitives appendix item 11).
- **Same-kind datetime range** — `recorded_at` carries `min`/`max` offset datetimes; comparisons use
  the instant (appendix-semantics 3.12/3.16). A cross-kind range would be
  `STRICTSPEC_SCHEMA_DATETIME_KIND_MISMATCH` at meta-schema time.
- **Optional-absent** — `scheduled_local` absent binds ABSENT (decision 30, no default).

### Expected diagnostics — `invalid-01-kind-and-range.json`

Phase 1 (structural) runs first; the datetime KIND check fires during structural validation:

1. `STRICTSPEC_TYPE_DATETIME_KIND` · path `$.recorded_at` · slots `{expected: "offset", got:
   "local"}` (the string `"2025-06-01T13:37:00"` has no offset). A kind mismatch short-circuits the
   range comparison for that field (the value is not a well-formed offset datetime), so no separate
   `STRICTSPEC_VALUE_DATETIME_BEFORE` is emitted for `recorded_at` in this fixture.

## Verdict

CLEAN — RESOLVED (Phase 3.3). The datetime scalar set (`date`/`time`/`datetime`, offset and local)
is COMPLETE for the corpus (decision 35 — no `duration` scalar) and now exercised end to end.

VERDICT: RESOLVED (Phase 3.3).
