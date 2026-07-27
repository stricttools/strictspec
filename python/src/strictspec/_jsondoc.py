"""The JSON/JSONL backend of the strictspec document model.

A faithful, hand-written, lossless port of go/internal/jsondoc. It parses JSON
(and, via the JSONL functions, JSONL) bytes and folds the result into the
format-neutral doc model: every value node maps to a tagged, lexeme-retaining
Node, object keys resolve into an ordered record while spans point at the
source, and the original bytes round-trip byte-identically via Document.bytes().

The standard-library json module is deliberately NOT used: it is lossy (it
normalizes numbers, decodes strings, and last-wins on duplicate keys). This
backend retains the EXACT source lexeme of every scalar, computes byte-accurate
spans (columns and offsets count BYTES, matching the doc model), and raises a
hard error on duplicate object keys.
"""

from __future__ import annotations

import sys
from typing import Callable

from . import _doc as doc
from ._doc import Kind, ParseError, Position, Span

# maxDepth bounds nesting to keep parsing safe on adversarial input. This is a
# stack-safety guard, NOT a document-size limit. It mirrors the Go backend's
# explicit cap; on CPython the interpreter's own recursion limit is reached
# first for pathological input and is surfaced as the SAME clean ParseError
# (see _guard_recursion) rather than crashing the interpreter.
MAX_DEPTH = 10000

_HEXDIGITS = "0123456789ABCDEF"


def _decode_rune(data: bytes, i: int) -> tuple[int, int]:
    """Decode one UTF-8 rune at data[i]. Returns (codepoint, size). On an
    invalid lead/continuation byte returns (-1, 1) (matching Go's RuneError,1).
    """
    b0 = data[i]
    if b0 < 0x80:
        return b0, 1
    if 0xC2 <= b0 <= 0xDF:
        if i + 1 < len(data) and 0x80 <= data[i + 1] <= 0xBF:
            return ((b0 & 0x1F) << 6) | (data[i + 1] & 0x3F), 2
        return -1, 1
    if 0xE0 <= b0 <= 0xEF:
        if i + 2 < len(data) and 0x80 <= data[i + 1] <= 0xBF and 0x80 <= data[i + 2] <= 0xBF:
            cp = ((b0 & 0x0F) << 12) | ((data[i + 1] & 0x3F) << 6) | (data[i + 2] & 0x3F)
            if 0xD800 <= cp <= 0xDFFF or cp < 0x800:
                return -1, 1
            return cp, 3
        return -1, 1
    if 0xF0 <= b0 <= 0xF4:
        if (
            i + 3 < len(data)
            and 0x80 <= data[i + 1] <= 0xBF
            and 0x80 <= data[i + 2] <= 0xBF
            and 0x80 <= data[i + 3] <= 0xBF
        ):
            cp = (
                ((b0 & 0x07) << 18)
                | ((data[i + 1] & 0x3F) << 12)
                | ((data[i + 2] & 0x3F) << 6)
                | (data[i + 3] & 0x3F)
            )
            if cp < 0x10000 or cp > 0x10FFFF:
                return -1, 1
            return cp, 4
        return -1, 1
    return -1, 1


class _Parser:
    """A per-parse cursor over a byte slice.

    For a standalone JSON document data is the whole source and base_offset is
    0. For one JSONL line data is just that line's bytes (LF stripped) and
    base_offset/line seed the cursor so every emitted Position is GLOBAL.
    """

    __slots__ = ("data", "i", "line", "col", "base_offset", "format")

    def __init__(self, data: bytes, line: int, col: int, base_offset: int, fmt: str) -> None:
        self.data = data
        self.i = 0
        self.line = line
        self.col = col
        self.base_offset = base_offset
        self.format = fmt

    def pos(self) -> Position:
        return Position(line=self.line, column=self.col, byte_offset=self.base_offset + self.i)

    def at_end(self) -> bool:
        return self.i >= len(self.data)

    def peek(self) -> int:
        return self.data[self.i]

    def next(self) -> int:
        b = self.data[self.i]
        self.i += 1
        if b == 0x0A:  # '\n'
            self.line += 1
            self.col = 1
        else:
            self.col += 1
        return b

    def skip_ws(self) -> None:
        while not self.at_end() and _is_json_space(self.peek()):
            self.next()

    def err_at(self, pos: Position, msg: str) -> ParseError:
        return ParseError(self.format, pos, msg)

    def parse_document(self) -> doc.Node:
        self.skip_ws()
        if self.at_end():
            raise self.err_at(
                Position(1, 1, 0), "empty input: expected a JSON document, found none"
            )
        root = self.parse_value(0)
        self.skip_ws()
        if not self.at_end():
            raise self.err_at(self.pos(), "unexpected trailing content after the JSON document")
        return root

    def parse_value(self, depth: int) -> doc.Node:
        self.skip_ws()
        if self.at_end():
            raise self.err_at(self.pos(), "unexpected end of input, expected a value")
        b = self.peek()
        if b == 0x7B:  # '{'
            return self.parse_object(depth)
        if b == 0x5B:  # '['
            return self.parse_array(depth)
        if b == 0x22:  # '"'
            span, lexeme, _ = self.scan_string()
            return doc.new_scalar(Kind.STRING, lexeme, span)
        if b == 0x74:  # 't'
            return self.parse_literal("true", Kind.BOOL)
        if b == 0x66:  # 'f'
            return self.parse_literal("false", Kind.BOOL)
        if b == 0x6E:  # 'n'
            return self.parse_literal("null", Kind.NULL)
        if b == 0x2D or (0x30 <= b <= 0x39):  # '-' or digit
            return self.parse_number()
        if b == 0x4E or b == 0x49:  # 'N' or 'I'
            raise self.err_at(self.pos(), "NaN and Infinity are not valid JSON")
        raise self.err_at(self.pos(), "unexpected character " + _quote_byte(b))

    def parse_object(self, depth: int) -> doc.Node:
        if depth >= MAX_DEPTH:
            raise self.err_at(self.pos(), "maximum nesting depth exceeded")
        start_pos = self.pos()
        self.next()  # '{'
        entries: list[doc.Entry] = []
        seen: set[str] = set()

        self.skip_ws()
        if not self.at_end() and self.peek() == 0x7D:  # '}'
            self.next()
            return doc.new_record(entries, Span(start_pos, self.pos()))

        while True:
            self.skip_ws()
            if self.at_end():
                raise self.err_at(self.pos(), "unterminated object, expected a key or '}'")
            if self.peek() != 0x22:
                raise self.err_at(
                    self.pos(), "expected a string key, found " + _quote_byte(self.peek())
                )
            key_span, _, decoded = self.scan_string()
            if decoded in seen:
                raise self.err_at(
                    key_span.start, "duplicate key " + _quote_str(decoded) + " in JSON object"
                )
            seen.add(decoded)

            self.skip_ws()
            if self.at_end() or self.peek() != 0x3A:  # ':'
                raise self.err_at(self.pos(), "expected ':' after object key")
            self.next()

            value = self.parse_value(depth + 1)
            entries.append(doc.Entry(key=decoded, value=value, key_span=key_span))

            self.skip_ws()
            if self.at_end():
                raise self.err_at(self.pos(), "unterminated object, expected ',' or '}'")
            c = self.peek()
            if c == 0x2C:  # ','
                self.next()
            elif c == 0x7D:  # '}'
                self.next()
                return doc.new_record(entries, Span(start_pos, self.pos()))
            else:
                raise self.err_at(
                    self.pos(), "expected ',' or '}' in object, found " + _quote_byte(c)
                )

    def parse_array(self, depth: int) -> doc.Node:
        if depth >= MAX_DEPTH:
            raise self.err_at(self.pos(), "maximum nesting depth exceeded")
        start_pos = self.pos()
        self.next()  # '['
        items: list[doc.Node] = []

        self.skip_ws()
        if not self.at_end() and self.peek() == 0x5D:  # ']'
            self.next()
            return doc.new_array(items, Span(start_pos, self.pos()))

        while True:
            value = self.parse_value(depth + 1)
            items.append(value)
            self.skip_ws()
            if self.at_end():
                raise self.err_at(self.pos(), "unterminated array, expected ',' or ']'")
            c = self.peek()
            if c == 0x2C:  # ','
                self.next()
            elif c == 0x5D:  # ']'
                self.next()
                return doc.new_array(items, Span(start_pos, self.pos()))
            else:
                raise self.err_at(
                    self.pos(), "expected ',' or ']' in array, found " + _quote_byte(c)
                )

    def parse_literal(self, word: str, kind: Kind) -> doc.Node:
        start_pos = self.pos()
        start_i = self.i
        for k in range(len(word)):
            if self.at_end() or self.peek() != ord(word[k]):
                raise self.err_at(start_pos, "invalid literal, expected " + _quote_str(word))
            self.next()
        return doc.new_scalar(
            kind, self.data[start_i:self.i].decode("utf-8"), Span(start_pos, self.pos())
        )

    def parse_number(self) -> doc.Node:
        start_pos = self.pos()
        start_i = self.i
        is_float = False

        if self.peek() == 0x2D:  # '-'
            self.next()
        if self.at_end():
            raise self.err_at(start_pos, "invalid number, expected a digit")
        b = self.peek()
        if b == 0x30:  # '0'
            self.next()
        elif 0x31 <= b <= 0x39:
            self.next()
            while not self.at_end() and _is_digit(self.peek()):
                self.next()
        else:
            raise self.err_at(
                self.pos(), "invalid number, expected a digit, found " + _quote_byte(b)
            )
        # Fraction.
        if not self.at_end() and self.peek() == 0x2E:  # '.'
            is_float = True
            self.next()
            if self.at_end() or not _is_digit(self.peek()):
                raise self.err_at(
                    self.pos(), "invalid number, expected a digit after the decimal point"
                )
            while not self.at_end() and _is_digit(self.peek()):
                self.next()
        # Exponent.
        if not self.at_end() and self.peek() in (0x65, 0x45):  # 'e' 'E'
            is_float = True
            self.next()
            if not self.at_end() and self.peek() in (0x2B, 0x2D):  # '+' '-'
                self.next()
            if self.at_end() or not _is_digit(self.peek()):
                raise self.err_at(self.pos(), "invalid number, expected a digit in the exponent")
            while not self.at_end() and _is_digit(self.peek()):
                self.next()

        kind = Kind.FLOAT if is_float else Kind.INTEGER
        return doc.new_scalar(
            kind, self.data[start_i:self.i].decode("utf-8"), Span(start_pos, self.pos())
        )

    def scan_string(self) -> tuple[Span, str, str]:
        start_pos = self.pos()
        start_i = self.i
        self.next()  # opening '"'
        dec: list[str] = []

        while True:
            if self.at_end():
                raise self.err_at(self.pos(), "unterminated string")
            b = self.peek()
            if b == 0x22:  # '"'
                self.next()
                span = Span(start_pos, self.pos())
                return span, self.data[start_i:self.i].decode("utf-8"), "".join(dec)
            if b == 0x5C:  # '\'
                self.scan_escape(dec)
            elif b < 0x20:
                raise self.err_at(
                    self.pos(),
                    "raw control character U+" + _hex4(b) + " in string; it must be escaped",
                )
            elif b < 0x80:
                self.next()
                dec.append(chr(b))
            else:
                r, size = _decode_rune(self.data, self.i)
                if r < 0 and size == 1:
                    raise self.err_at(self.pos(), "invalid UTF-8 byte in string")
                for _ in range(size):
                    self.next()
                dec.append(chr(r))

    def scan_escape(self, dec: list[str]) -> None:
        esc_pos = self.pos()
        self.next()  # '\'
        if self.at_end():
            raise self.err_at(esc_pos, "unterminated escape sequence")
        e = self.next()
        simple = {
            0x22: '"',
            0x5C: "\\",
            0x2F: "/",
            0x62: "\b",
            0x66: "\f",
            0x6E: "\n",
            0x72: "\r",
            0x74: "\t",
        }
        if e in simple:
            dec.append(simple[e])
            return
        if e == 0x75:  # 'u'
            r1 = self.read_hex4()
            if r1 is None:
                raise self.err_at(
                    esc_pos, "invalid \\u escape: expected four hexadecimal digits"
                )
            if 0xD800 <= r1 <= 0xDBFF:
                # High surrogate: try to pair with a following \uXXXX low surrogate.
                if (
                    self.i + 1 < len(self.data)
                    and self.data[self.i] == 0x5C
                    and self.data[self.i + 1] == 0x75
                ):
                    save = (self.i, self.line, self.col)
                    self.next()  # '\'
                    self.next()  # 'u'
                    r2 = self.read_hex4()
                    if r2 is not None and 0xDC00 <= r2 <= 0xDFFF:
                        dec.append(chr(0x10000 + ((r1 - 0xD800) << 10) + (r2 - 0xDC00)))
                        return
                    self.i, self.line, self.col = save
                dec.append("�")
            elif 0xDC00 <= r1 <= 0xDFFF:
                dec.append("�")
            else:
                dec.append(chr(r1))
            return
        raise self.err_at(esc_pos, "invalid escape sequence \\" + chr(e))

    def read_hex4(self) -> int | None:
        if self.i + 4 > len(self.data):
            return None
        r = 0
        for k in range(4):
            v = _hex_val(self.data[self.i + k])
            if v is None:
                return None
            r = (r << 4) | v
        for _ in range(4):
            self.next()
        return r


def _is_json_space(b: int) -> bool:
    return b in (0x20, 0x09, 0x0A, 0x0D)


def _is_digit(b: int) -> bool:
    return 0x30 <= b <= 0x39


def _hex_val(b: int) -> int | None:
    if 0x30 <= b <= 0x39:
        return b - 0x30
    if 0x61 <= b <= 0x66:
        return b - 0x61 + 10
    if 0x41 <= b <= 0x46:
        return b - 0x41 + 10
    return None


def _quote_byte(b: int) -> str:
    if 0x20 <= b < 0x7F:
        return "'" + chr(b) + "'"
    return "U+" + _hex4(b)


def _quote_str(s: str) -> str:
    return '"' + s + '"'


def _hex4(r: int) -> str:
    buf = ["0", "0", "0", "0"]
    for k in range(3, -1, -1):
        buf[k] = _HEXDIGITS[r & 0xF]
        r >>= 4
    return "".join(buf)


def parse(src: bytes) -> doc.Document:
    """Parse a single JSON document into a Document. Raises ParseError on any
    lexical or structural error.
    """
    p = _Parser(bytes(src), line=1, col=1, base_offset=0, fmt=doc.FORMAT_JSON)
    root = _guard_recursion(p, p.parse_document)
    return doc.new_document(doc.FORMAT_JSON, root, src)


def _guard_recursion(p: "_Parser", fn):
    """Run a recursive-descent parse, converting a CPython RecursionError on
    pathological nesting into the same clean ParseError the explicit MAX_DEPTH
    guard would raise.
    """
    old = sys.getrecursionlimit()
    sys.setrecursionlimit(max(old, 4000))
    try:
        return fn()
    except RecursionError:
        raise p.err_at(p.pos(), "maximum nesting depth exceeded") from None
    finally:
        sys.setrecursionlimit(old)


def parse_lines(src: bytes) -> list[doc.Document]:
    """Parse JSONL bytes into one Document per line. Raises ParseError (with a
    global position) at the first framing or parse error; empty input yields [].
    """
    docs: list[doc.Document] = []
    parse_stream_bytes(src, docs.append)
    return docs


def parse_stream_bytes(src: bytes, emit: Callable[[doc.Document], None]) -> None:
    """Parse JSONL from an in-memory byte string, invoking emit once per
    successfully parsed line-document in order. Positions are GLOBAL.
    """
    src = bytes(src)
    line_no = 0
    offset = 0
    n = len(src)
    while offset < n:
        nl = src.find(b"\n", offset)
        if nl == -1:
            chunk = src[offset:]
            has_lf = False
        else:
            chunk = src[offset : nl + 1]
            has_lf = True
        if len(chunk) == 0:
            return
        line_no += 1
        line_start = offset
        offset += len(chunk)
        line = chunk[:-1] if has_lf else chunk

        _frame_line(line, line_no, line_start)
        p = _Parser(line, line=line_no, col=1, base_offset=line_start, fmt=doc.FORMAT_JSONL)
        root = _guard_recursion(p, p.parse_document)
        emit(doc.new_document(doc.FORMAT_JSONL, root, line))


def parse_stream(reader, emit: Callable[[doc.Document], None]) -> None:
    """Streaming variant over a binary reader (memory bounded by the largest
    line). reader must be a binary file-like object supporting readline().
    """
    line_no = 0
    offset = 0
    while True:
        chunk = reader.readline()
        if not chunk:
            return
        line_no += 1
        line_start = offset
        offset += len(chunk)
        if chunk.endswith(b"\n"):
            line = chunk[:-1]
        else:
            line = chunk
        _frame_line(line, line_no, line_start)
        p = _Parser(line, line=line_no, col=1, base_offset=line_start, fmt=doc.FORMAT_JSONL)
        root = _guard_recursion(p, p.parse_document)
        emit(doc.new_document(doc.FORMAT_JSONL, root, line))


def _frame_line(line: bytes, line_no: int, line_start: int) -> None:
    n = len(line)
    if n > 0 and line[-1] == 0x0D:  # '\r'
        raise ParseError(
            doc.FORMAT_JSONL,
            Position(line=line_no, column=n, byte_offset=line_start + n - 1),
            "line ends with a carriage return; JSONL is LF-only",
        )
    if _is_blank_line(line):
        raise ParseError(
            doc.FORMAT_JSONL,
            Position(line=line_no, column=1, byte_offset=line_start),
            "blank line is not a valid JSONL document",
        )


def _is_blank_line(line: bytes) -> bool:
    for b in line:
        if b != 0x20 and b != 0x09:
            return False
    return True
