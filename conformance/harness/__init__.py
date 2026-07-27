"""strictspec cross-target conformance harness.

Pure-data fixtures (see ``fixtures.py``) are run against a declarative N-way
target registry (``targets.py``) with honest UNIMPLEMENTED reporting
(``runner.py``) and a cross-target parity checker (``parity.py``). Message text
is rendered from the pinned catalogue (``templates.py``); paths are validated
against the pinned grammar (``paths.py``).
"""

from __future__ import annotations

from pathlib import Path

HARNESS_DIR = Path(__file__).resolve().parent
CONFORMANCE_DIR = HARNESS_DIR.parent
FIXTURES_ROOT = CONFORMANCE_DIR / "fixtures"
REPO_ROOT = CONFORMANCE_DIR.parent

__all__ = ["HARNESS_DIR", "CONFORMANCE_DIR", "FIXTURES_ROOT", "REPO_ROOT"]
