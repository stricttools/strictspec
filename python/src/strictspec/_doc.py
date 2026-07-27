"""The format-neutral, tagged, lexeme-retaining document model.

A faithful Python port of go/internal/doc. It is the shared contract every
backend (TOML via tomlkit, JSON/JSONL via a hand-written ordered decoder)
populates: one model, three syntaxes. Values carry their lexeme class (Kind)
and their exact source bytes (lexeme), which is what makes both verdict
identity (read side) and byte-stable writes (write side, later) possible.

This module is intentionally dependency-light and knows nothing about
diagnostics, schemas, or validation. Parse failures surface as the typed
ParseError below (with a source position); conversion to toolchain diagnostics
happens later, at the validator layer.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import IntEnum


class Kind(IntEnum):
    """The lexeme class of a document-model node.

    FORMAT-NEUTRAL and LEXICAL: it records how the value was written (integer
    vs float lexeme class, which datetime flavor), never a schema-level
    interpretation. Per the constitution, the NUMBER scalar is a schema-level
    concept layered over Integer and Float; there is no Number kind here.
    """

    RECORD = 0
    ARRAY = 1
    STRING = 2
    INTEGER = 3
    FLOAT = 4
    BOOL = 5
    NULL = 6
    DATETIME_OFFSET = 7
    DATETIME_LOCAL = 8
    DATE_LOCAL = 9
    TIME_LOCAL = 10

    def __str__(self) -> str:
        return _KIND_NAMES.get(self, f"Kind({int(self)})")

    def is_scalar(self) -> bool:
        return self not in (Kind.RECORD, Kind.ARRAY)


_KIND_NAMES = {
    Kind.RECORD: "Record",
    Kind.ARRAY: "Array",
    Kind.STRING: "String",
    Kind.INTEGER: "Integer",
    Kind.FLOAT: "Float",
    Kind.BOOL: "Bool",
    Kind.NULL: "Null",
    Kind.DATETIME_OFFSET: "DateTimeOffset",
    Kind.DATETIME_LOCAL: "DateTimeLocal",
    Kind.DATE_LOCAL: "DateLocal",
    Kind.TIME_LOCAL: "TimeLocal",
}


@dataclass(frozen=True)
class Position:
    """A single location in a source document.

    Line and column are 1-based (column counts bytes, matching the TOML
    substrate); byte_offset is a 0-based offset into the source bytes. The zero
    Position (line == 0) is the sentinel for "no source position".
    """

    line: int = 0
    column: int = 0
    byte_offset: int = 0

    def is_valid(self) -> bool:
        return self.line >= 1


@dataclass(frozen=True)
class Span:
    """The half-open source range [start, end) covered by a node.

    For any scalar node parsed from source, the bytes in
    [start.byte_offset, end.byte_offset) are exactly the node's lexeme.
    """

    start: Position = field(default_factory=Position)
    end: Position = field(default_factory=Position)

    def is_valid(self) -> bool:
        return self.start.is_valid()


@dataclass(frozen=True)
class Entry:
    """One ordered key/value binding inside a Record.

    key is the DECODED (unquoted, code-point) key string; key_span points at
    the key's source location; value is the bound node.
    """

    key: str
    value: "Node"
    key_span: Span = field(default_factory=Span)


class Node:
    """A tagged, immutable document-model value.

    One model, three syntaxes: a backend's parser is the only thing that
    constructs Nodes, via new_scalar/new_record/new_array. Read-only by
    convention; there is no mutation API.
    """

    __slots__ = ("kind", "lexeme", "span", "_entries", "_items")

    def __init__(
        self,
        kind: Kind,
        lexeme: str,
        span: Span,
        entries: tuple[Entry, ...],
        items: tuple["Node", ...],
    ) -> None:
        self.kind = kind
        self.lexeme = lexeme
        self.span = span
        self._entries = entries
        self._items = items

    @property
    def entries(self) -> tuple[Entry, ...]:
        """The ordered key/value bindings of a Record; () for non-Records."""
        return self._entries

    @property
    def items(self) -> tuple["Node", ...]:
        """The ordered elements of an Array; () for non-Arrays."""
        return self._items

    def __repr__(self) -> str:  # pragma: no cover - debugging aid
        if self.kind == Kind.RECORD:
            return f"Node(Record, {len(self._entries)} entries)"
        if self.kind == Kind.ARRAY:
            return f"Node(Array, {len(self._items)} items)"
        return f"Node({self.kind}, {self.lexeme!r})"


def new_scalar(kind: Kind, lexeme: str, span: Span) -> Node:
    """Build a scalar Node. Raises on a container kind (a backend bug)."""
    if not kind.is_scalar():
        raise ValueError(f"new_scalar: {kind} is not a scalar kind")
    return Node(kind, lexeme, span, (), ())


def new_record(entries: tuple[Entry, ...] | list[Entry], span: Span) -> Node:
    """Build a Record Node from ordered entries."""
    return Node(Kind.RECORD, "", span, tuple(entries), ())


def new_array(items: tuple[Node, ...] | list[Node], span: Span) -> Node:
    """Build an Array Node from ordered items."""
    return Node(Kind.ARRAY, "", span, (), tuple(items))


class Format(str):
    """The concrete surface syntax a Document was parsed from."""


FORMAT_TOML = "toml"
FORMAT_JSON = "json"
FORMAT_JSONL = "jsonl"


class Document:
    """One parsed document: its source format, its root Node, and its exact
    source bytes.
    """

    __slots__ = ("format", "root", "_source")

    def __init__(self, fmt: str, root: Node, source: bytes) -> None:
        self.format = fmt
        self.root = root
        self._source = bytes(source)  # private, immutable snapshot

    def bytes(self) -> bytes:
        """The exact source bytes the document was parsed from (a fresh copy)."""
        return bytes(self._source)


def new_document(fmt: str, root: Node, source: bytes) -> Document:
    return Document(fmt, root, source)


class ParseError(Exception):
    """A typed parse failure with a source position.

    The ONLY error a backend's parse raises. Deliberately does not reference
    the toolchain's diagnostic types.
    """

    def __init__(self, fmt: str, position: Position, message: str) -> None:
        self.format = fmt
        self.position = position
        self.message = message
        super().__init__(str(self))

    def __str__(self) -> str:
        return (
            f"{self.format} parse error at line {self.position.line}, "
            f"column {self.position.column}: {self.message}"
        )
