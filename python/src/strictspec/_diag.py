"""The strictspec diagnostic model: the path grammar, the document-value and
slot tagged unions, and the Diagnostic / Diagnostics types.

A faithful Python port of go/internal/diag. It is the shared vocabulary the
emitter IR populates and the render module consumes. It contains no message
templates and no rendering of message text (that is _render, driven by the
generated codes catalogue) -- only the structured data a diagnostic carries and
the path/value primitives whose rendering is fixed by appendix-rendering.md.

Aligned with conformance/harness/paths.py: the path grammar tokenizer there and
Path.render() here produce the same strings.
"""

from __future__ import annotations

from dataclasses import dataclass, field


# --- string primitives (A.2 escaping, identifier-shaped rule) ----------------


def is_ident_shaped(s: str) -> bool:
    """Whether s is identifier-shaped: [A-Za-z_][A-Za-z0-9_-]* .

    Identifier-shaped keys render bare (`.name`); others switch to the quoted
    map-key form (`["name"]`). Candidates render bare when ident-shaped.
    """
    if s == "":
        return False
    for i, ch in enumerate(s):
        if i == 0:
            if not (ch == "_" or _is_ascii_letter(ch)):
                return False
            continue
        if not (ch == "_" or ch == "-" or _is_ascii_letter(ch) or ("0" <= ch <= "9")):
            return False
    return True


def _is_ascii_letter(ch: str) -> bool:
    return ("A" <= ch <= "Z") or ("a" <= ch <= "z")


def escape_string(s: str) -> str:
    """Apply the A.2 string-escaping table.

    Does NOT add surrounding quotes and never truncates. Exactly the A.2
    escapes are produced; all other code points -- including non-ASCII -- are
    emitted verbatim.
    """
    out: list[str] = []
    for ch in s:
        if ch == '"':
            out.append('\\"')
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\r":
            out.append("\\r")
        elif ch == "\t":
            out.append("\\t")
        else:
            o = ord(ch)
            if o <= 0x1F:
                out.append("\\u00" + format(o, "02x"))
            else:
                out.append(ch)
    return "".join(out)


# --- path steps --------------------------------------------------------------


class Step:
    """Base class for the closed step set: Root, Key, Index, MapKey, Arm."""

    __slots__ = ()


@dataclass(frozen=True)
class Root(Step):
    """Renders the document root marker "$". A path always begins with it."""


@dataclass(frozen=True)
class Key(Step):
    """A record field or typed-map key. Renders bare `.name` when ident-shaped,
    else the quoted map-key form `["<escaped>"]`.
    """

    name: str


@dataclass(frozen=True)
class Index(Step):
    """A zero-based array element, rendered "[n]"."""

    n: int


@dataclass(frozen=True)
class MapKey(Step):
    """A TYPED-MAP key, rendered ALWAYS in the bracketed, quoted form
    `["<escaped>"]` (unlike record Key steps which switch to dotted form).
    """

    name: str


@dataclass(frozen=True)
class Arm(Step):
    """Disambiguates which discriminated-union arm produced a nested
    diagnostic, rendered "(name)".
    """

    name: str


@dataclass(frozen=True)
class JSONLAnchor:
    """Addresses a value within a JSONL stream: "@L<line>:<offset>". Line is
    one-based; offset is a zero-based byte offset within the line.
    """

    line: int
    offset: int


@dataclass(frozen=True)
class Path:
    """A diagnostic path: an ordered sequence of steps rooted at the document
    root, optionally anchored to a JSONL stream position.
    """

    steps: tuple[Step, ...] = ()
    anchor: JSONLAnchor | None = None

    def render(self) -> str:
        out: list[str] = []
        for s in self.steps:
            if isinstance(s, Root):
                out.append("$")
            elif isinstance(s, Key):
                if is_ident_shaped(s.name):
                    out.append("." + s.name)
                else:
                    out.append('["' + escape_string(s.name) + '"]')
            elif isinstance(s, Index):
                out.append("[" + str(s.n) + "]")
            elif isinstance(s, MapKey):
                out.append('["' + escape_string(s.name) + '"]')
            elif isinstance(s, Arm):
                out.append("(" + s.name + ")")
            else:  # pragma: no cover
                raise ValueError("diag: unknown path step type")
        r = "".join(out)
        if self.anchor is not None:
            r += f"@L{self.anchor.line}:{self.anchor.offset}"
        return r

    def with_anchor(self, line: int, offset: int) -> "Path":
        return Path(steps=self.steps, anchor=JSONLAnchor(line, offset))


def new_path(*steps: Step) -> Path:
    """Build a Path rooted at "$" with the given steps (Root prepended)."""
    return Path(steps=(Root(),) + tuple(steps))


def append_step(p: Path, s: Step) -> Path:
    return Path(steps=p.steps + (s,), anchor=p.anchor)


def append_key(p: Path, key: str) -> Path:
    return append_step(p, Key(key))


def append_map_key(p: Path, key: str) -> Path:
    return append_step(p, MapKey(key))


def append_index(p: Path, i: int) -> Path:
    return append_step(p, Index(i))


def append_arm(p: Path, name: str) -> Path:
    return append_step(p, Arm(name))


# --- document values (payloads of value-typed slots and list<T> elements) ----


class Value:
    """Base class for rendered document values (A.1 table)."""

    __slots__ = ()


@dataclass(frozen=True)
class IntVal(Value):
    """An int64 integer; renders as decimal digits (A.1). No negative zero."""

    n: int


@dataclass(frozen=True)
class FloatVal(Value):
    """A float value. With a lexeme it renders from source UNCHANGED (A.1);
    without, per the canonical shortest-round-trip rule (A.3), float-marked.
    """

    f: float = 0.0
    lexeme: str = ""
    has_lexeme: bool = False


@dataclass(frozen=True)
class NumberVal(Value):
    """A `number`-scalar value, rendered per its source lexeme class (A.1)."""

    lexeme: str
    int_class: bool


@dataclass(frozen=True)
class StringVal(Value):
    """A string; renders double-quoted with A.2 escaping and A.4 truncation."""

    s: str


@dataclass(frozen=True)
class BoolVal(Value):
    """Renders lowercase "true"/"false" (A.1)."""

    b: bool


@dataclass(frozen=True)
class NullVal(Value):
    """Renders lowercase "null" (A.1)."""


@dataclass(frozen=True)
class DateVal(Value):
    s: str


@dataclass(frozen=True)
class TimeVal(Value):
    s: str


@dataclass(frozen=True)
class DatetimeVal(Value):
    s: str


@dataclass(frozen=True)
class ArrayVal(Value):
    """A container value; renders as the A.5 truncated inline form."""

    elems: tuple[Value, ...] = ()


@dataclass(frozen=True)
class RecordVal(Value):
    """A container value in document order; renders as the A.5 truncated inline
    form (keys bare when ident-shaped, else quoted; at most 3 pairs).
    """

    keys: tuple[str, ...] = ()
    vals: tuple[Value, ...] = ()


# --- slots (template placeholder bindings) -----------------------------------


class Slot:
    """Base class for the slot-type vocabulary (appendix-error-codes.md sec.2)."""

    __slots__ = ()


@dataclass(frozen=True)
class SlotString(Slot):
    """A `string`-typed slot: a PROSE insertion, rendered BARE."""

    s: str


@dataclass(frozen=True)
class SlotInt(Slot):
    n: int


@dataclass(frozen=True)
class SlotCode(Slot):
    code: str


@dataclass(frozen=True)
class SlotIdentifier(Slot):
    name: str


@dataclass(frozen=True)
class SlotVersion(Slot):
    v: int


@dataclass(frozen=True)
class SlotPath(Slot):
    p: Path


@dataclass(frozen=True)
class SlotValue(Slot):
    v: Value


@dataclass(frozen=True)
class SlotList(Slot):
    elems: tuple[Value, ...] = ()


@dataclass(frozen=True)
class SlotSuggestion(Slot):
    """Fills `{suggestion}`. The renderer computes the did-you-mean clause from
    unknown against candidates.
    """

    unknown: str
    candidates: tuple[str, ...] = ()


# --- diagnostics -------------------------------------------------------------


@dataclass(frozen=True)
class Diagnostic:
    """One emitted hard error: a STRICTSPEC_* code, the Path it is attached to,
    and the typed slot bindings that fill the remaining template placeholders.
    {path} is never present in slots -- it comes from path.
    """

    code: str
    path: Path
    slots: dict[str, Slot] = field(default_factory=dict)


class Diagnostics:
    """An ordered, one-pass accumulation of diagnostics in emission order.
    Renderers may not reorder.
    """

    __slots__ = ("_items",)

    def __init__(self) -> None:
        self._items: list[Diagnostic] = []

    def emit(self, diagnostic: Diagnostic) -> None:
        self._items.append(diagnostic)

    def emit_code(
        self, code: str, path: Path, slots: dict[str, Slot] | None
    ) -> None:
        self._items.append(Diagnostic(code=code, path=path, slots=slots or {}))

    def all(self) -> list[Diagnostic]:
        return self._items

    def __len__(self) -> int:
        return len(self._items)
