# Acceptance test — strictspec vs a private project's legacy validators

This directory is the acceptance test's own module (conformance/DESIGN.md, "The
acceptance test"). It proves the flagship end to end: from the hand-written
strictspec translation of a private project's character-preview schema, `strictspec gen`
emits Python and TypeScript validators; run over a private project's real corpus, they
agree with a private project's existing hand-written validators (pydantic + legacy TS)
STRICTLY, except a FIXED waiver list where strictspec's stricter verdict is
correct by definition.

The test lives at `../tests/test_acceptance.py` (picked up by the suite's
`uv run pytest`); the reusable machinery is `../harness/acceptance.py`.

## Layout

- `schema/character-preview.schema.toml` — conformance-owned copy of
  `examples/private-project/character-preview.toml`, byte-identical except for one
  added `safe_integers = true` (mandatory for TS emission, decision 14). A
  drift-guard test reconstructs the examples source from it and asserts equality.
- `corpus/*.json` — the corpus documents; `corpus/manifest.toml` records the
  provenance of each. The corpus is a private project's real character-preview test
  inputs (`server/tests/test_character_preview.py::_doc()` and its mutations) plus
  conformance fixtures derivable from the schema (the `examples/` samples) plus
  mutated documents that exercise rejection paths and the waived divergence classes.
- `legacy-verdicts.json` — committed capture of the pydantic and legacy-TS
  verdicts over the corpus, produced by `../scripts/capture_legacy_verdicts.py`.
  The `_meta` block records the capture command and sources.
- `waivers.toml` — the FIXED waiver list.

## Bootstrap stamp

a private project's real documents predate strictspec and carry no `format_version`.
Each imported real document is stamped `format_version = 1` (the one-time
pre-versioning conversion of decision 13). strictspec's `format_version` gate is
invisible to the legacy validators, which predate it, so the capture script
strips `format_version` before feeding a document to a legacy validator.

## Reproducing the legacy capture

```
cd conformance
uv run python scripts/capture_legacy_verdicts.py --private-project-root <a private project>
```

This is the only step that reads the a private project tree, and it is read-only. Its
committed output makes the acceptance test independent of a private project at run time.

## The criterion (as realized)

1. Strictspec verdicts come live from the generated Python AND TS validators; the
   two must agree exactly (four-target identity).
2. Verdict parity with each legacy target is strict except the fixed waivers; the
   observed divergence set must equal the waiver set exactly (a locked set).
3. Each waiver asserts strictspec's correct behaviour: the waived document is
   rejected by strictspec with the entry's exact codes/paths and accepted by every
   legacy target it lists.
4. On reject (both reject): every normalized legacy path is chain-compatible with
   strictspec's path set — the concrete realization of DESIGN's path-subset rule,
   given the legacy validators report at container/leaf granularity strictspec does
   not always mirror exactly.
