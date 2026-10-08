"""THE ACCEPTANCE TEST (conformance/DESIGN.md, "The acceptance test").

From the hand-written strictspec translation of the sprite tool's character-preview
schema, GENERATE Python and TS validators via ``strictspec gen``, run them over
the sprite tool's real corpus, and assert VERDICT PARITY against the sprite tool's
existing hand-written validators (pydantic + legacy TS) — STRICT except a
committed, FROZEN waiver list where strictspec's stricter verdict is correct by
definition.

Timing (DESIGN): this runs at MVP time, from the schema alone, BEFORE any
consumer migration. The strictspec verdicts are produced live by the generated
validators; the legacy verdicts are committed capture data
(``acceptance/legacy-verdicts.json``, produced by
``scripts/capture_legacy_verdicts.py``); the corpus and waiver list are committed
under ``acceptance/``. Nothing here touches the sprite tool's working tree.

This module is collected by the same ``uv run pytest`` invocation as the rest of
the suite (it lives in ``tests/``), so the acceptance test is part of the gate.
"""

from __future__ import annotations

import shutil

import pytest

from harness import acceptance
from harness.acceptance import (
    Waiver,
    corpus_documents,
    generate_and_run,
    legacy_reject_paths,
    load_legacy_verdicts,
    load_waivers,
    strictspec_reject_paths,
    subset_holds,
)

_LEGACY_TARGETS = ("pydantic", "ts")


# --- prerequisites ------------------------------------------------------------

_HAVE_TOOLCHAIN = all(shutil.which(t) for t in ("go", "uv", "node"))
_HAVE_TSC = (acceptance._TS_DIR / "node_modules" / ".bin" / "tsc").exists()
requires_toolchain = pytest.mark.skipif(
    not (_HAVE_TOOLCHAIN and _HAVE_TSC),
    reason="acceptance test needs go + uv + node + ts/node_modules (tsc)",
)


@pytest.fixture(scope="module")
def strictspec_verdicts():
    """Generate + run the strictspec validators once for the whole module."""
    return generate_and_run()


# --- structural guards (run without the toolchain) ----------------------------


def test_acceptance_schema_tracks_examples():
    """The conformance-owned schema copy is the examples/ source plus exactly one
    added schema-wide safe-integer declaration (mandatory for TS emission). Drop
    the provenance header (through the sentinel) and that one line, and it must be
    byte-identical to examples/sprite-tool/character-preview.toml — so the copy can
    never silently drift from the source it was translated from."""
    copy_lines = acceptance.SCHEMA_FILE.read_text().splitlines(keepends=True)
    sentinel = "# --- end conformance provenance header ---\n"
    assert sentinel in copy_lines, "provenance-header sentinel missing"
    body = copy_lines[copy_lines.index(sentinel) + 1:]
    reconstructed = "".join(l for l in body if l != "safe_integers = true\n")
    assert reconstructed == acceptance.EXAMPLES_SCHEMA.read_text()
    # And the one adaptation really is present.
    assert "safe_integers = true\n" in acceptance.SCHEMA_FILE.read_text()


def test_corpus_and_legacy_verdicts_aligned():
    """Every corpus document has a captured verdict for both legacy targets, and
    vice versa — no orphans on either side."""
    corpus = set(corpus_documents())
    legacy = load_legacy_verdicts()
    assert corpus == set(legacy), (
        f"corpus/legacy mismatch: {corpus ^ set(legacy)}"
    )
    for name, v in legacy.items():
        assert set(v) == set(_LEGACY_TARGETS), f"{name}: legacy targets {set(v)}"


def test_waivers_are_wellformed():
    """Every waiver names a real corpus doc, real legacy targets, a non-empty
    defect, and expected strictspec codes/paths."""
    corpus = set(corpus_documents())
    for w in load_waivers():
        assert w.document in corpus, f"waiver for unknown doc {w.document!r}"
        assert set(w.legacy_targets) <= set(_LEGACY_TARGETS)
        assert w.defect.strip()
        assert w.strictspec_codes
        assert w.strictspec_paths


# --- the acceptance criterion -------------------------------------------------


@requires_toolchain
def test_generated_python_and_ts_agree(strictspec_verdicts):
    """Four-target identity subset: the generated Python and generated TS
    validators produce identical verdicts (generate_and_run raises otherwise).
    Reaching here means every corpus doc got a unified strictspec verdict."""
    assert set(strictspec_verdicts) == set(corpus_documents())


@requires_toolchain
def test_verdict_parity_except_waivers(strictspec_verdicts):
    """VERDICT PARITY: strictspec agrees with each legacy target on valid/invalid
    for every corpus document, EXCEPT exactly the (document, legacy-target) pairs
    covered by the frozen waiver list. The waiver set must match the observed
    divergence set EXACTLY (the freeze)."""
    legacy = load_legacy_verdicts()
    waivers = load_waivers()

    waived_pairs: set[tuple[str, str]] = set()
    for w in waivers:
        for t in w.legacy_targets:
            waived_pairs.add((w.document, t))

    observed_divergences: set[tuple[str, str]] = set()
    for name in corpus_documents():
        sv = strictspec_verdicts[name]
        for target in _LEGACY_TARGETS:
            lv = legacy[name][target]
            if sv.valid != lv["valid"]:
                observed_divergences.add((name, target))

    # FREEZE: additions or removals after the list went green fail here.
    assert observed_divergences == waived_pairs, (
        "waiver list is no longer frozen against the divergence set.\n"
        f"  unexpected NEW divergences (need investigation): {observed_divergences - waived_pairs}\n"
        f"  waived but NO LONGER diverging (re-capture legacy?):  {waived_pairs - observed_divergences}"
    )


@requires_toolchain
def test_waivers_assert_strictspec_correct_behaviour(strictspec_verdicts):
    """Each waiver asserts strictspec's CORRECT behaviour: the waived document
    must be REJECTED by strictspec with exactly the entry's diagnostic codes and
    paths (in emission order), and ACCEPTED by every legacy target it lists (that
    IS the divergence the waiver covers)."""
    legacy = load_legacy_verdicts()
    for w in load_waivers():
        sv = strictspec_verdicts[w.document]
        assert not sv.valid, f"{w.document}: waiver expects strictspec to REJECT"
        got_codes = tuple(d["code"] for d in sv.diagnostics)
        got_paths = tuple(d["path"] for d in sv.diagnostics)
        assert got_codes == w.strictspec_codes, (
            f"{w.document}: strictspec codes {got_codes} != waiver {w.strictspec_codes}"
        )
        assert got_paths == w.strictspec_paths, (
            f"{w.document}: strictspec paths {got_paths} != waiver {w.strictspec_paths}"
        )
        for target in w.legacy_targets:
            assert legacy[w.document][target]["valid"], (
                f"{w.document}: waiver claims legacy {target} ACCEPTS, but it rejected"
            )


@requires_toolchain
def test_reject_path_subset(strictspec_verdicts):
    """ON REJECT: when strictspec AND a legacy target both reject a document, the
    normalized legacy path set must be covered by strictspec's (DESIGN: "normalized
    legacy path SET must be a subset of strictspec's"). Realized as chain-
    compatibility — every legacy path shares a root-to-node chain with a strictspec
    path — because the legacy validators report at container/leaf granularity that
    strictspec does not always mirror exactly (see acceptance.py)."""
    legacy = load_legacy_verdicts()
    checked = 0
    for name in corpus_documents():
        sv = strictspec_verdicts[name]
        if sv.valid:
            continue
        strict_paths = strictspec_reject_paths(list(sv.diagnostics))
        for target in _LEGACY_TARGETS:
            lv = legacy[name][target]
            if lv["valid"]:
                continue  # verdict divergence — handled by the waiver tests
            lpaths = legacy_reject_paths(lv, target)
            assert subset_holds(lpaths, strict_paths), (
                f"{name} [{target}]: legacy paths {sorted(lpaths)} not covered by "
                f"strictspec paths {sorted(strict_paths)}"
            )
            checked += 1
    assert checked > 0, "no both-reject cases exercised the subset criterion"
