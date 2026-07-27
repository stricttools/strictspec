"""Freshness gate for the generated code catalogue (mirrors the Go freshness
test pattern): regenerate _codes.py in memory from the normative spec and assert
the on-disk file is byte-identical. Drift = failure.
"""

import importlib.util
import sys
from pathlib import Path

_PY_ROOT = Path(__file__).resolve().parents[1]
_REPO_ROOT = _PY_ROOT.parent
_GEN = _PY_ROOT / "scripts" / "gencodes.py"
_OUT = _PY_ROOT / "src" / "strictspec" / "_codes.py"


def _load_gencodes():
    spec = importlib.util.spec_from_file_location("gencodes", _GEN)
    mod = importlib.util.module_from_spec(spec)
    sys.modules["gencodes"] = mod
    spec.loader.exec_module(mod)
    return mod


def test_catalogue_is_fresh():
    gen = _load_gencodes()
    content = gen.generate(_REPO_ROOT)
    on_disk = _OUT.read_text(encoding="utf-8")
    assert on_disk == content, (
        "src/strictspec/_codes.py is STALE relative to spec/appendix-error-codes.md; "
        "regenerate with scripts/gencodes.py"
    )


def test_catalogue_matches_harness_transcription():
    """The generated catalogue's templates equal the harness's verbatim
    transcription of the appendix (a second, independent oracle).
    """
    from strictspec import _codes as codes

    harness = _REPO_ROOT / "conformance" / "harness"
    sys.path.insert(0, str(harness))
    try:
        import templates  # type: ignore
    finally:
        sys.path.pop(0)
    assert set(codes.CATALOGUE) == set(templates.CATALOGUE)
    for k, entry in codes.CATALOGUE.items():
        assert entry.template == templates.CATALOGUE[k], k
