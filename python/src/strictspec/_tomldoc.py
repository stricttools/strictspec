"""The TOML backend of the strictspec document model, built on tomlkit.

Parses TOML bytes with tomlkit (the sanctioned python-runtime TOML dependency)
and folds the result into the format-neutral doc model: every TOML value node
maps to a tagged, lexeme-retaining Node, dotted keys / [table] / [[array-table]]
headers resolve into the record tree per TOML semantics, and the original bytes
round-trip byte-identically via Document.bytes() (the Document stores the exact
source; tomlkit is used only to read structure and exact lexemes).

Exact LEXEMES come straight from tomlkit's Item.as_string() and are what
validation consumes (decodeString, numeric parsing, enum/literal comparison).

SPANS (byte offsets): tomlkit exposes no source positions. This backend derives
byte-accurate spans by a document-order forward scan of the raw source for each
scalar's exact lexeme. Two properties make this sound for strictspec's use:

  1. A derived span always satisfies source[span] == lexeme by construction (the
     scan matches the exact lexeme bytes), so span/lexeme exactness holds.
  2. TOML node spans are NEVER consumed by any diagnostic in this runtime: the
     emitter attaches source positions only for JSONL (@L anchors), and TOML is
     never JSONL; TOML parse-error positions come from tomlkit's exception, not
     from node spans. So a scan that (in a pathological doc) matched a lexeme
     occurring inside a comment could not mis-position any diagnostic.

If a lexeme cannot be located at/after the cursor, that is a hard parse error
(never a silently-wrong position). Container spans are derived from their
children's spans.
"""

from __future__ import annotations

from bisect import bisect_right

import tomlkit
from tomlkit.exceptions import ParseError as _TKParseError
from tomlkit.items import (
    AoT,
    Array,
    Bool,
    Date,
    DateTime,
    Float,
    InlineTable,
    Integer,
    String,
    Table,
    Time,
)

from . import _doc as doc
from ._doc import Kind, ParseError, Position, Span


def parse(src: bytes) -> doc.Document:
    """Parse TOML source into a Document. Raises ParseError with a source
    position on any syntax or TOML-semantic error (including duplicate keys,
    which tomlkit rejects).
    """
    src = bytes(src)
    text = src.decode("utf-8")
    try:
        tk = tomlkit.parse(text)
    except _TKParseError as e:
        raise _to_parse_error(e, src) from None
    conv = _Converter(src)
    root = conv.build_root(tk)
    return doc.new_document(doc.FORMAT_TOML, root, src)


def _to_parse_error(e: _TKParseError, src: bytes) -> ParseError:
    line = getattr(e, "line", 0) or 0
    col0 = getattr(e, "col", 0) or 0  # tomlkit column is 0-based
    starts = _line_starts(src)
    off = 0
    if 1 <= line <= len(starts):
        off = starts[line - 1] + col0
    msg = str(e)
    # tomlkit prefixes messages with " at line X col Y"; keep the leading clause.
    idx = msg.find(" at line ")
    if idx != -1:
        msg = msg[:idx]
    return ParseError(doc.FORMAT_TOML, Position(line=line, column=col0 + 1, byte_offset=off), msg)


def _line_starts(src: bytes) -> list[int]:
    starts = [0]
    for i, b in enumerate(src):
        if b == 0x0A:
            starts.append(i + 1)
    return starts


class _Slot:
    """One key binding under construction: exactly one of a leaf value node, a
    sub-record builder, or a list of array-of-tables entry builders.
    """

    __slots__ = ("key_span", "value", "sub", "arr")

    def __init__(self) -> None:
        self.key_span = Span()
        self.value: doc.Node | None = None
        self.sub: "_Builder | None" = None
        self.arr: list["_Builder"] = []


class _Builder:
    """A mutable intermediate record preserving first-appearance key order."""

    __slots__ = ("keys", "by_key")

    def __init__(self) -> None:
        self.keys: list[str] = []
        self.by_key: dict[str, _Slot] = {}

    def note(self, key: str) -> _Slot:
        s = self.by_key.get(key)
        if s is not None:
            return s
        s = _Slot()
        self.by_key[key] = s
        self.keys.append(key)
        return s

    def finalize(self, span: Span) -> doc.Node:
        entries: list[doc.Entry] = []
        for k in self.keys:
            s = self.by_key[k]
            if s.value is not None:
                v = s.value
            elif s.arr:
                items = [b.finalize(Span()) for b in s.arr]
                v = doc.new_array(items, _cover(items))
            elif s.sub is not None:
                v = s.sub.finalize(Span())
            else:
                v = doc.new_record((), s.key_span)
            entries.append(doc.Entry(key=k, value=v, key_span=s.key_span))
        if not span.is_valid():
            span = _cover([e.value for e in entries])
        return doc.new_record(entries, span)


def _cover(nodes: list[doc.Node]) -> Span:
    spans = [n.span for n in nodes if n.span.is_valid()]
    if not spans:
        return Span()
    return Span(spans[0].start, spans[-1].end)


class _Converter:
    __slots__ = ("src", "text", "line_starts", "cursor")

    def __init__(self, src: bytes) -> None:
        self.src = src
        self.text = src.decode("utf-8")
        self.line_starts = _line_starts(src)
        self.cursor = 0  # byte offset

    def _pos(self, off: int) -> Position:
        li = bisect_right(self.line_starts, off) - 1
        if li < 0:
            li = 0
        return Position(line=li + 1, column=off - self.line_starts[li] + 1, byte_offset=off)

    def _scan(self, lexeme: str) -> Span:
        """Locate lexeme at/after the cursor; return its span and advance."""
        needle = lexeme.encode("utf-8")
        idx = self.src.find(needle, self.cursor)
        if idx < 0:
            raise ParseError(
                doc.FORMAT_TOML,
                self._pos(self.cursor),
                f"internal: could not locate TOML lexeme {lexeme!r} in source",
            )
        end = idx + len(needle)
        self.cursor = end
        return Span(self._pos(idx), self._pos(end))

    def build_root(self, tk_container) -> doc.Node:
        b = _Builder()
        self._fold_container(b, tk_container)
        root_span = Span(Position(1, 1, 0), self._pos(len(self.src)))
        return b.finalize(root_span)

    def _fold_container(self, b: _Builder, container) -> None:
        for key, item in container.body:
            if key is None:
                continue  # whitespace / comment
            seg = key.key  # decoded single segment
            slot = b.note(seg)
            self._fold_item(slot, item)

    def _fold_item(self, slot: _Slot, item) -> None:
        if isinstance(item, InlineTable):
            slot.value = self._convert_inline_table(item)
        elif isinstance(item, Table):
            if slot.sub is None:
                slot.sub = _Builder()
            self._fold_container(slot.sub, item.value)
        elif isinstance(item, AoT):
            for entry in item.body:
                eb = _Builder()
                self._fold_container(eb, entry.value)
                slot.arr.append(eb)
        else:
            slot.value = self._convert_value(item)

    def _convert_inline_table(self, item: InlineTable) -> doc.Node:
        b = _Builder()
        self._fold_container(b, item.value)
        return b.finalize(Span())

    def _convert_value(self, item) -> doc.Node:
        if isinstance(item, Array):
            children = [
                self._convert_value(g.value) for g in item._value if g.value is not None
            ]
            return doc.new_array(children, _cover(children))
        if isinstance(item, InlineTable):
            return self._convert_inline_table(item)
        lexeme = item.as_string()
        span = self._scan(lexeme)
        return doc.new_scalar(_scalar_kind(item, lexeme), lexeme, span)


def _scalar_kind(item, lexeme: str) -> Kind:
    if isinstance(item, String):
        return Kind.STRING
    if isinstance(item, Bool):
        return Kind.BOOL
    if isinstance(item, DateTime):
        return Kind.DATETIME_OFFSET if _has_offset(lexeme) else Kind.DATETIME_LOCAL
    if isinstance(item, Date):
        return Kind.DATE_LOCAL
    if isinstance(item, Time):
        return Kind.TIME_LOCAL
    if isinstance(item, Integer):
        return Kind.INTEGER
    if isinstance(item, Float):
        return Kind.FLOAT
    # Fallback: classify by lexeme shape (should not occur for valid TOML).
    return Kind.STRING


def _has_offset(lexeme: str) -> bool:
    # An offset datetime ends with 'Z'/'z' or an explicit +HH:MM / -HH:MM.
    tpos = lexeme.find("T")
    if tpos == -1:
        tpos = lexeme.find(" ")
    tail = lexeme[tpos + 1 :] if tpos != -1 else lexeme
    if tail.endswith("Z") or tail.endswith("z"):
        return True
    if len(tail) >= 6 and tail[-6] in "+-" and tail[-3] == ":":
        return True
    return False
