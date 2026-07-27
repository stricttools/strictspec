"""Validator for the pinned diagnostic path grammar (appendix-rendering.md Part B).

A fixture that carries a path outside this grammar is malformed -- a hard error,
not a silent pass. The grammar is EBNF in the appendix; this is its tokenizer.
"""

from __future__ import annotations

import re

_ROOT = "$"
_KEY_STEP = re.compile(r"\.[A-Za-z_][A-Za-z0-9_-]*")
_INDEX_STEP = re.compile(r"\[\d+\]")
# map-key-step: bracketed, double-quoted key with the A.2 escape set.
_MAP_KEY_STEP = re.compile(
    r'\["(?:[^"\\\x00-\x1f]|\\(?:["\\nrt]|u[0-9a-f]{4}))*"\]'
)
_ARM_STEP = re.compile(r"\([A-Za-z_][A-Za-z0-9_-]*\)")
_JSONL_SUFFIX = re.compile(r"@L\d+:\d+")

_STEP_RES = (_KEY_STEP, _INDEX_STEP, _MAP_KEY_STEP, _ARM_STEP)


def is_valid_path(path: str) -> bool:
    """True iff ``path`` conforms to the pinned path grammar."""
    if not path.startswith(_ROOT):
        return False
    pos = len(_ROOT)
    n = len(path)
    # zero or more steps
    while pos < n:
        # the jsonl suffix, if present, is last
        m = _JSONL_SUFFIX.match(path, pos)
        if m and m.end() == n:
            return True
        matched = False
        for rx in _STEP_RES:
            m = rx.match(path, pos)
            if m:
                pos = m.end()
                matched = True
                break
        if not matched:
            return False
    return True
