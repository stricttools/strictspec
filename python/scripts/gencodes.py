#!/usr/bin/env python3
"""Generate the Python strictspec error-code catalogue module.

Parses spec/appendix-error-codes.md (the single normative source for the
error-code catalogue) and emits src/strictspec/_codes.py. This mirrors the Go
generator go/tools/gencodes: the appendix is the only writer of the catalogue,
and a freshness test regenerates and byte-compares (drift = test failure).

Usage:
    python scripts/gencodes.py [--spec PATH] [--out PATH] [--check]

With no flags it auto-locates the repo root (the ancestor containing
spec/appendix-error-codes.md) and derives both paths. --check regenerates in
memory and exits non-zero if the on-disk file differs (the freshness gate).
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

SPEC_REL = "spec/appendix-error-codes.md"
OUT_REL = "python/src/strictspec/_codes.py"

_CODE_ROW_RE = re.compile(r"^\| `STRICTSPEC_")
_AREA_ROW_RE = re.compile(r"^\| `([A-Z]+)` \|")
_AREA_HEADING_RE = re.compile(r"^## 3\. ")
_NEXT_HEADING_RE = re.compile(r"^## ")
_PLACEHOLDER_RE = re.compile(r"\{(\w+)\}")
_SLOT_ENTRY_RE = re.compile(r"^([A-Za-z_]\w*):\s*(.+)$")
_BACKTICK_CODE_RE = re.compile(r"^`(STRICTSPEC_[A-Z0-9_]+)`$")


@dataclass
class SlotSpecData:
    name: str
    typ: str  # SlotType member name
    elem_type: str | None = None  # for list types


@dataclass
class EntryData:
    code: str
    area: str
    template: str
    slots: list[SlotSpecData]


def find_repo_root(start: Path) -> Path:
    d = start.resolve()
    while True:
        if (d / SPEC_REL).exists():
            return d
        if d.parent == d:
            raise SystemExit(f"gencodes: could not locate {SPEC_REL} in any ancestor")
        d = d.parent


def split_cells(row: str) -> list[str]:
    cells: list[str] = []
    cur: list[str] = []
    i = 0
    n = len(row)
    while i < n:
        ch = row[i]
        if ch == "\\" and i + 1 < n and row[i + 1] == "|":
            cur.append("|")
            i += 2
            continue
        if ch == "|":
            cells.append("".join(cur))
            cur = []
            i += 1
            continue
        cur.append(ch)
        i += 1
    cells.append("".join(cur))
    if len(cells) >= 2:
        cells = cells[1:-1]
    return [c.strip() for c in cells]


def parse_areas(lines: list[str]) -> list[str]:
    areas: list[str] = []
    in_section = False
    for line in lines:
        if _AREA_HEADING_RE.match(line):
            in_section = True
            continue
        if in_section and _NEXT_HEADING_RE.match(line):
            break
        if not in_section:
            continue
        m = _AREA_ROW_RE.match(line)
        if m:
            areas.append(m.group(1))
    if not areas:
        raise SystemExit("gencodes: could not parse the closed area set from section 3")
    return sorted(areas)


def parse_code_cell(cell: str) -> str:
    m = _BACKTICK_CODE_RE.match(cell)
    if m is None:
        raise SystemExit(f"gencodes: malformed code cell {cell!r}")
    return m.group(1)


def parse_template_cell(cell: str) -> str:
    if len(cell) < 2 or cell[0] != "`" or cell[-1] != "`":
        raise SystemExit(f"gencodes: template cell not backtick-delimited: {cell!r}")
    return cell[1:-1]


def area_of(code: str, area_set: set[str]) -> str:
    parts = code.split("_", 2)
    if len(parts) < 3 or parts[0] != "STRICTSPEC":
        raise SystemExit(f"gencodes: code {code!r} is not STRICTSPEC_<AREA>_<NAME>")
    area = parts[1]
    if area not in area_set:
        raise SystemExit(f"gencodes: code {code!r} has area {area!r} outside the closed set")
    return area


_SCALAR_SLOT_TYPES = {
    "string": "STRING",
    "int": "INT",
    "code": "CODE",
    "identifier": "IDENTIFIER",
    "version": "VERSION",
    "path": "PATH",
    "value": "VALUE",
}


def scalar_slot_type(token: str) -> str:
    if token not in _SCALAR_SLOT_TYPES:
        raise SystemExit(f"gencodes: unknown slot type {token!r}")
    return _SCALAR_SLOT_TYPES[token]


def parse_slot_type(name: str, type_text: str) -> SlotSpecData:
    type_text = type_text.replace(r"\<", "<").replace(r"\>", ">").strip()
    if type_text.startswith("list<") and type_text.endswith(">"):
        elem = type_text[len("list<") : -1]
        return SlotSpecData(name=name, typ="LIST", elem_type=scalar_slot_type(elem))
    return SlotSpecData(name=name, typ=scalar_slot_type(type_text))


def parse_slots_cell(cell: str) -> dict[str, SlotSpecData]:
    out: dict[str, SlotSpecData] = {}
    cell = cell.strip()
    if cell in ("", "—", "-"):
        return out
    for part in cell.split(","):
        part = part.strip()
        if part == "":
            continue
        m = _SLOT_ENTRY_RE.match(part)
        if m is None:
            raise SystemExit(f"gencodes: malformed slot declaration {part!r}")
        out[m.group(1)] = parse_slot_type(m.group(1), m.group(2).strip())
    return out


def resolve_slots(code: str, template: str, declared: dict[str, SlotSpecData]) -> list[SlotSpecData]:
    order: list[str] = []
    seen: set[str] = set()
    for m in _PLACEHOLDER_RE.finditer(template):
        name = m.group(1)
        if name in seen:
            continue
        seen.add(name)
        order.append(name)
    for name in declared:
        if name not in seen:
            raise SystemExit(
                f"gencodes: code {code} declares slot {name!r} the template does not reference"
            )
    slots: list[SlotSpecData] = []
    for name in order:
        if name in declared:
            slots.append(declared[name])
            continue
        if name == "path":
            slots.append(SlotSpecData(name="path", typ="PATH"))
        elif name == "suggestion":
            slots.append(SlotSpecData(name="suggestion", typ="STRING"))
        else:
            raise SystemExit(
                f"gencodes: code {code}: placeholder {{{name}}} has no declared slot type"
            )
    return slots


def parse(src: str) -> tuple[list[EntryData], list[str]]:
    lines = src.split("\n")
    areas = parse_areas(lines)
    area_set = set(areas)

    entries: list[EntryData] = []
    seen: set[str] = set()
    for lineno, line in enumerate(lines, 1):
        if not _CODE_ROW_RE.match(line):
            continue
        cells = split_cells(line)
        if len(cells) < 3:
            raise SystemExit(f"gencodes: line {lineno}: expected >=3 cells, got {len(cells)}")
        code = parse_code_cell(cells[0])
        if code in seen:
            raise SystemExit(f"gencodes: line {lineno}: duplicate code {code}")
        seen.add(code)
        template = parse_template_cell(cells[1])
        area = area_of(code, area_set)
        declared = parse_slots_cell(cells[2])
        slots = resolve_slots(code, template, declared)
        entries.append(EntryData(code=code, area=area, template=template, slots=slots))
    if not entries:
        raise SystemExit("gencodes: no code rows found")
    entries.sort(key=lambda e: e.code)
    return entries, areas


_HEADER = '''"""The generated strictspec error-code catalogue.

Code generated by scripts/gencodes.py from {spec}. DO NOT EDIT.

Every STRICTSPEC_* code with its area, message template, and declared slots,
parsed from the single normative source. Hand-transcription is forbidden; a
freshness test regenerates and byte-compares (drift = failure).
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import IntEnum


class SlotType(IntEnum):
    """One of the slot-type vocabulary of appendix-error-codes.md sec.2."""

    STRING = 0
    INT = 1
    CODE = 2
    IDENTIFIER = 3
    VERSION = 4
    PATH = 5
    VALUE = 6
    LIST = 7

    def __str__(self) -> str:
        return _SLOT_TYPE_NAMES.get(self, "unknown")


_SLOT_TYPE_NAMES = {{
    SlotType.STRING: "string",
    SlotType.INT: "int",
    SlotType.CODE: "code",
    SlotType.IDENTIFIER: "identifier",
    SlotType.VERSION: "version",
    SlotType.PATH: "path",
    SlotType.VALUE: "value",
    SlotType.LIST: "list",
}}


@dataclass(frozen=True)
class SlotSpec:
    """One declared slot: its placeholder name and declared type. For LIST,
    elem_type is the element type (e.g. list<string>).
    """

    name: str
    type: SlotType
    elem_type: SlotType | None = None


@dataclass(frozen=True)
class Entry:
    """One catalogue row: the code, its area, the pinned message template, and
    the slots the template interpolates in placeholder order.
    """

    code: str
    area: str
    template: str
    slots: tuple[SlotSpec, ...] = ()


def lookup(code: str) -> Entry | None:
    """Return the catalogue entry for a code, or None."""
    return CATALOGUE.get(code)


def all_entries() -> list[Entry]:
    """Every catalogue entry, sorted by code."""
    return [CATALOGUE[k] for k in sorted(CATALOGUE)]
'''


def render(entries: list[EntryData], areas: list[str], spec_rel: str) -> str:
    out: list[str] = []
    out.append(_HEADER.format(spec=spec_rel))
    out.append("\n\n")
    out.append("# The closed area set (appendix-error-codes.md section 3).\n")
    out.append("AREAS = (\n")
    for a in areas:
        out.append(f"    {a!r},\n")
    out.append(")\n\n")
    out.append("CATALOGUE: dict[str, Entry] = {\n")
    for e in entries:
        out.append(f"    {e.code!r}: Entry(\n")
        out.append(f"        code={e.code!r},\n")
        out.append(f"        area={e.area!r},\n")
        out.append(f"        template={e.template!r},\n")
        if not e.slots:
            out.append("        slots=(),\n")
        else:
            out.append("        slots=(\n")
            for s in e.slots:
                if s.elem_type is not None:
                    out.append(
                        f"            SlotSpec({s.name!r}, SlotType.{s.typ}, SlotType.{s.elem_type}),\n"
                    )
                else:
                    out.append(f"            SlotSpec({s.name!r}, SlotType.{s.typ}),\n")
            out.append("        ),\n")
        out.append("    ),\n")
    out.append("}\n")
    return "".join(out)


def generate(repo_root: Path) -> str:
    spec_path = repo_root / SPEC_REL
    src = spec_path.read_text(encoding="utf-8")
    entries, areas = parse(src)
    return render(entries, areas, SPEC_REL)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--spec", default="")
    ap.add_argument("--out", default="")
    ap.add_argument("--check", action="store_true", help="fail if the on-disk file is stale")
    args = ap.parse_args()

    root = find_repo_root(Path(__file__).parent)
    spec_path = Path(args.spec) if args.spec else root / SPEC_REL
    out_path = Path(args.out) if args.out else root / OUT_REL

    src = spec_path.read_text(encoding="utf-8")
    entries, areas = parse(src)
    content = render(entries, areas, SPEC_REL)

    if args.check:
        existing = out_path.read_text(encoding="utf-8") if out_path.exists() else ""
        if existing != content:
            print(f"gencodes: {out_path} is STALE; run scripts/gencodes.py", file=sys.stderr)
            return 1
        print(f"gencodes: {out_path} is fresh ({len(entries)} codes)")
        return 0

    out_path.write_text(content, encoding="utf-8")
    print(f"gencodes: wrote {len(entries)} codes to {out_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
