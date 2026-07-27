"""Turn a structured Diagnostic into its pinned message text.

A faithful port of go/internal/render (render.go + didyoumean.go). It
substitutes each template slot with the value rendering fixed by
spec/appendix-rendering.md (Part A value rendering, Part B path grammar, Part C
did-you-mean, Part D condition scheme). Templates come from the generated codes
catalogue; there is no hand-written message string here.

Programmer-error policy: an unknown code, an unknown slot binding, or a missing
required slot at render time all raise (the Go panic-equivalent). A Diagnostic
is constructed by slot-correct emitter code; these conditions can only mean a
bug, never malformed user input, so they fail loudly.
"""

from __future__ import annotations

import re

from . import _codes as codes
from . import _diag as diag

_PLACEHOLDER_RE = re.compile(r"\{(\w+)\}")


class RenderError(Exception):
    """Raised on a programmer error at render time (the Go panic-equivalent)."""


def render(d: diag.Diagnostic) -> str:
    """Produce the pinned message text for a diagnostic."""
    entry = codes.lookup(d.code)
    if entry is None:
        raise RenderError(f"render: unknown code {d.code!r} (not in the catalogue)")

    placeholders = _placeholder_set(entry.template)
    _validate_slots(d, placeholders)

    def sub(m: re.Match[str]) -> str:
        name = m.group(1)
        if name == "path":
            return d.path.render()
        if name == "suggestion":
            slot = d.slots.get("suggestion")
            if slot is None:
                return ""
            if not isinstance(slot, diag.SlotSuggestion):
                raise RenderError(
                    f"render: code {d.code} slot 'suggestion' must be a SlotSuggestion"
                )
            return _render_suggestion(slot)
        slot = d.slots.get(name)
        if slot is None:
            raise RenderError(f"render: code {d.code} missing required slot {name!r}")
        return _render_slot(d.code, name, slot)

    return _PLACEHOLDER_RE.sub(sub, entry.template)


def _placeholder_set(template: str) -> set[str]:
    return {m.group(1) for m in _PLACEHOLDER_RE.finditer(template)}


def _validate_slots(d: diag.Diagnostic, placeholders: set[str]) -> None:
    for name in d.slots:
        if name == "path":
            raise RenderError(
                f"render: code {d.code} binds {{path}} manually; it is auto-injected"
            )
        if name not in placeholders:
            raise RenderError(
                f"render: code {d.code} has unknown slot {name!r} (not a template placeholder)"
            )
    for name in placeholders:
        if name in ("path", "suggestion"):
            continue
        if name not in d.slots:
            raise RenderError(f"render: code {d.code} missing required slot {name!r}")


def _render_slot(code: str, name: str, slot: diag.Slot) -> str:
    if isinstance(slot, diag.SlotString):
        return slot.s
    if isinstance(slot, diag.SlotInt):
        return str(slot.n)
    if isinstance(slot, diag.SlotCode):
        return slot.code
    if isinstance(slot, diag.SlotIdentifier):
        return slot.name
    if isinstance(slot, diag.SlotVersion):
        return str(slot.v)
    if isinstance(slot, diag.SlotPath):
        return slot.p.render()
    if isinstance(slot, diag.SlotValue):
        return _render_value(slot.v)
    if isinstance(slot, diag.SlotList):
        return _render_array(list(slot.elems), 1)
    if isinstance(slot, diag.SlotSuggestion):
        return _render_suggestion(slot)
    raise RenderError(f"render: code {code} slot {name!r} has unknown slot type {type(slot)!r}")


# --- Value rendering (appendix-rendering.md Part A) --------------------------


def render_value(v: diag.Value) -> str:
    """Render a document value per A.1 (top-level container depth = 1)."""
    return _render_value_at_depth(v, 1)


def _render_value(v: diag.Value) -> str:
    return _render_value_at_depth(v, 1)


def _render_value_at_depth(v: diag.Value, depth: int) -> str:
    if isinstance(v, diag.IntVal):
        return str(v.n)
    if isinstance(v, diag.FloatVal):
        if v.has_lexeme:
            return v.lexeme  # A.1: source lexeme unchanged
        return _canonical_float(v.f)  # A.3: constructed float, float-marked
    if isinstance(v, diag.NumberVal):
        return v.lexeme  # A.1: per its source lexeme class
    if isinstance(v, diag.StringVal):
        return _render_quoted_string(v.s)
    if isinstance(v, diag.BoolVal):
        return "true" if v.b else "false"
    if isinstance(v, diag.NullVal):
        return "null"
    if isinstance(v, diag.DateVal):
        return v.s
    if isinstance(v, diag.TimeVal):
        return v.s
    if isinstance(v, diag.DatetimeVal):
        return v.s
    if isinstance(v, diag.ArrayVal):
        if depth > 2:
            return "[...]"
        return _render_array(list(v.elems), depth)
    if isinstance(v, diag.RecordVal):
        if depth > 2:
            return "{...}"
        return _render_record(v, depth)
    raise RenderError(f"render: unknown value type {type(v)!r}")


def _render_array(elems: list[diag.Value], depth: int) -> str:
    out = ["["]
    shown = min(len(elems), 3)
    for i in range(shown):
        if i > 0:
            out.append(", ")
        out.append(_render_value_at_depth(elems[i], depth + 1))
    if len(elems) > 3:
        out.append(", ...")
    out.append("]")
    return "".join(out)


def _render_record(r: diag.RecordVal, depth: int) -> str:
    if len(r.keys) != len(r.vals):
        raise RenderError(f"render: RecordVal has {len(r.keys)} keys but {len(r.vals)} values")
    out = ["{"]
    shown = min(len(r.keys), 3)
    for i in range(shown):
        if i > 0:
            out.append(", ")
        if diag.is_ident_shaped(r.keys[i]):
            out.append(r.keys[i])
        else:
            out.append('"' + diag.escape_string(r.keys[i]) + '"')
        out.append(": ")
        out.append(_render_value_at_depth(r.vals[i], depth + 1))
    if len(r.keys) > 3:
        out.append(", ...")
    out.append("}")
    return "".join(out)


def _render_quoted_string(s: str) -> str:
    truncated = False
    if len(s) > 64:
        s = s[:64]
        truncated = True
    content = diag.escape_string(s)
    if truncated:
        return '"' + content + '..."'
    return '"' + content + '"'


def _canonical_float(f: float) -> str:
    s = repr(f)
    if not any(c in s for c in ".eE"):
        s += ".0"
    return s


# --- did-you-mean (appendix-rendering.md Part C) -----------------------------


def _render_suggestion(s: diag.SlotSuggestion) -> str:
    cands: list[tuple[str, int]] = []
    for c in s.candidates:
        dist = _levenshtein(s.unknown, c)
        if dist <= 2:
            cands.append((c, dist))
    cands.sort(key=lambda cd: (cd[1], cd[0]))  # distance asc, then code-point order
    cands = cands[:3]
    if not cands:
        return ""
    names = [_render_candidate(c[0]) for c in cands]
    if len(names) == 1:
        return " Did you mean " + names[0] + "?"
    if len(names) == 2:
        return " Did you mean " + names[0] + " or " + names[1] + "?"
    return " Did you mean " + names[0] + ", " + names[1] + ", or " + names[2] + "?"


def _render_candidate(name: str) -> str:
    if diag.is_ident_shaped(name):
        return name
    return _render_quoted_string(name)


def _levenshtein(a: str, b: str) -> int:
    ra = a
    rb = b
    if len(ra) == 0:
        return len(rb)
    if len(rb) == 0:
        return len(ra)
    prev = list(range(len(rb) + 1))
    curr = [0] * (len(rb) + 1)
    for i in range(1, len(ra) + 1):
        curr[0] = i
        for j in range(1, len(rb) + 1):
            cost = 0 if ra[i - 1] == rb[j - 1] else 1
            curr[j] = min(prev[j] + 1, curr[j - 1] + 1, prev[j - 1] + cost)
        prev, curr = curr, prev
    return prev[len(rb)]
