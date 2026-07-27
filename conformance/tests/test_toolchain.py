"""Toolchain-level (CLI-only) conformance fixtures — Phase 6.

Migration, the write path, ``diff`` certificates, and ``doc-diff`` deltas are
TOOLCHAIN-ONLY per the constitution: migration is CLI-only; ``diff``/``doc-diff``
are golden-output determinism, not multi-target execution. These fixtures run
against the GO TOOLCHAIN ALONE (single-target) and assert CLI behaviour — exit
code, byte-exact output, and expected diagnostics/certificate claims. The
four-target validation categories (``test_harness.py``) are untouched.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from harness import FIXTURES_ROOT
from harness.toolchain import (
    discover_toolchain_fixtures,
    ensure_cli,
    run_toolchain_fixture,
)

_FIXTURES = discover_toolchain_fixtures(FIXTURES_ROOT)


def test_toolchain_fixtures_exist():
    # Guard against a silent empty run (the discovery glob or the _toolchain dir
    # going missing would otherwise pass vacuously).
    assert len(_FIXTURES) >= 8, f"expected the seeded toolchain fixtures, found {len(_FIXTURES)}"


@pytest.mark.parametrize("fixture", _FIXTURES, ids=[f.name for f in _FIXTURES])
def test_toolchain_fixture(fixture, tmp_path: Path):
    binary = ensure_cli()
    result = run_toolchain_fixture(binary, fixture, tmp_path)
    assert result.ok, f"{fixture.name}: {result.detail}"


def test_toolchain_kinds_covered():
    kinds = {f.kind for f in _FIXTURES}
    assert kinds == {"migration", "diff", "doc-diff"}, kinds
