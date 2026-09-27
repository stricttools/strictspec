"""Freshness check for the generated built-in schema module: regenerate
_builtins.py in memory from go/strictspec/builtin/ (the single source of every
built-in schema) and assert the on-disk file is byte-identical.
"""

import importlib.util
import sys
from pathlib import Path

_PY_ROOT = Path(__file__).resolve().parents[1]
_GEN = _PY_ROOT / "scripts" / "genbuiltins.py"
_OUT = _PY_ROOT / "src" / "strictspec" / "_builtins.py"


def test_builtins_are_fresh():
    spec = importlib.util.spec_from_file_location("genbuiltins", _GEN)
    mod = importlib.util.module_from_spec(spec)
    sys.modules["genbuiltins"] = mod
    spec.loader.exec_module(mod)
    assert _OUT.read_text(encoding="utf-8") == mod.generate(mod.repo_root()), (
        "src/strictspec/_builtins.py is stale relative to go/strictspec/builtin/; "
        "regenerate with python/scripts/genbuiltins.py"
    )
