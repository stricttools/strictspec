# Gap note — rlsbl JSONL changelog entry

Source: `rlsbl/rlsbl/changelog/schema.py` (`ChangelogEntry`, `validate_schema`,
`parse_entry`, `serialize_entry`). Schemas:
`changelog-entry-commit-mode.schema.toml`,
`changelog-entry-changeset-file-mode.schema.toml`. Samples: `valid-commit-mode.jsonl`,
`invalid-commit-mode.jsonl`, `valid-changeset-file-mode.jsonl`.

## The mode-split decision (and its justification)

`coverage_unit` (`"commit"` vs `"changeset-file"`) governs the shape:

| field    | commit mode        | changeset-file mode |
|----------|--------------------|---------------------|
| commits  | required, non-empty| FORBIDDEN           |
| id       | optional           | REQUIRED            |

The critical fact: **`coverage_unit` is a PROJECT-CONFIG value
(`.rlsbl/config.json`), not a field on the JSONL line.** The entry itself carries
no mode marker. That rules out a discriminated union (§3.5) outright — there is
no in-document discriminator to dispatch on.

Three candidate modelings, and why TWO SCHEMAS wins:

1. **Discriminated union on a mode field** — impossible; no such field exists on
   the line, and inventing one would change the on-disk format.
2. **One schema with conditional-required/forbidden on a mode marker** — same
   problem: the marker is not in the document, and strictspec conditions test
   only fields of the document (§3.24). A constraint cannot read project config.
3. **TWO schemas, consumer selects by `coverage_unit`** — CHOSEN. The consumer
   reads `coverage_unit` from config and validates the stream against the
   matching schema. This is textbook **explicit mode selection** (DESIGN.md, "No
   silent degradation": "The presence or absence of a config object IS the
   choice… this is not fallback — it is explicit mode selection"). The two
   schemas share every field but `commits`/`id`; the difference is a required↔
   forbidden flip, which is exactly a per-schema structural difference, not a
   runtime branch.

Two schemas is the cleanest and the only one consistent with the language: a
schema is a pure function of the document, and the mode is not in the document.
The mild cost — the shared fields are written twice — is the honest price of a
format whose shape is chosen out-of-band. (Type-definition imports (decision 21)
could factor the shared fields into a named type per record, but the two ROOT
records still differ, so imports only dedupe the leaves, not the split.)

## Expressed cleanly

- **Per-line `format_version` gate.** JSONL is validated line-by-line; each line
  carries `format_version` and gates independently (§DESIGN.md JSONL; a
  mixed-version stream is well-defined). Clean.
- **`user_facing` conditional-required.** `description` and `type` required when
  `user_facing == true` — two `conditional-required` forms with a value-triggered
  condition (§3.24). This is the exact shape `validate_schema` implements. Clean.
- **`type` / `release_type` enums.** `{feature,fix,breaking}` and `{ota,build}`
  map onto the enum construct. Clean.
- **`commits` non-empty (commit mode).** `min_len = 1` reproduces
  "commits is empty". Clean.
- **`id` required (changeset-file mode).** A required string. Clean.
- **set-coverage across the stream.** "Every commit in `<last_tag>..HEAD` is
  covered by some entry's `commits[]`" is the flagship `set-coverage`
  cross-document form (§3.25) with the `commits-in-range` resolver — declared in
  the commit-mode schema. This is precisely the origin DESIGN.md cites for
  set-coverage, and it maps exactly.

### Expected diagnostics — `invalid-commit-mode.jsonl` (streaming, per line)

1. `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED` · path `$.type@L1:...`
   · slots: key="type", condition="user_facing == true"
   (line 1 is user_facing with no `type`; `description` is present so only `type`
   fires).
2. `STRICTSPEC_VALUE_ARRAY_TOO_SHORT` · path `$.commits@L2:...`
   · slots: actual=0, limit=1  (line 2's `commits` is `[]`).
3. `STRICTSPEC_TYPE_MISSING_REQUIRED` · path `$@L3:...`
   · slots: key="commits"  (line 3 omits `commits` entirely).

## Awkward / partial

- **"Forbidden-always" vs unknown-key (changeset-file `commits`).** rlsbl treats
  a present `commits` in changeset-file mode as a NAMED error ("commits must be
  empty in changeset-file mode"). strictspec has no "this declared field must be
  absent" construct; the changeset-file schema simply does not declare `commits`,
  so a present `commits` is `STRICTSPEC_KEY_UNKNOWN` with a possible did-you-mean.
  The verdict is identical (rejected), but the diagnostic is a generic unknown-key
  rather than a mode-specific message. Acceptable, but note: an author reading
  "unknown key commits" in a changelog file may be confused, since `commits` is a
  very well-known field in the OTHER mode. A `forbidden-field` construct (declare
  the field, mark it always-absent, get a targeted message) would read better.
  Judgment call — logged as an awkwardness, not a hard finding.

## Notes (not gaps)

- **Bootstrap contract (per-line stamping).** Existing rlsbl JSONL predates
  versioning and carries no `format_version`. The one-time conversion script
  (DESIGN.md decision 34, per-consumer) stamps `format_version` into every line —
  it must STAMP, never reshape, and refuse ambiguous inputs. Shape-detection
  rules for this format: (a) a line with a non-empty `commits` array and no `id`
  is a commit-mode entry; (b) a line with `id` and no `commits` is a
  changeset-file entry; (c) a line with BOTH is ambiguous and the script refuses
  (it cannot know the project's mode from the line alone — it must read
  `coverage_unit` from config to disambiguate, which is the correct input). The
  stamper adds `format_version` and nothing else. This is documentation only;
  strictspec ships no stamper (decision 34).
- **`serialize_entry` field order / compactness.** rlsbl writes `id` first, omits
  absent optionals, and uses `separators=(",",":")`. On the write side strictspec
  preserves source lexemes for untouched values and renders constructed values
  canonically; JSONL compact separators are a serialization detail within-backend.
  No cross-target issue for read verdicts. Not a gap.

## Verdict

CLEAN. The mode split resolves cleanly to two consumer-selected schemas (explicit
mode selection), and every field/constraint maps onto an existing construct. One
ergonomics note logged (forbidden-field would give a better message than
unknown-key for changeset-file `commits`), but it changes no verdict and needs no
new expressive power — not counted as a finding.
