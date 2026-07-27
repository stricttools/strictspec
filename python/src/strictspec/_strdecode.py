"""Decode the RETAINED SOURCE LEXEME of a string scalar into its code points.

A faithful Python port of go/internal/strdecode. The document model retains the
exact source bytes of a scalar (quotes and escapes as written); validation that
reads a string's VALUE (regex, length, enum membership, literal comparison,
reference resolution, rendering) needs the decoded content, not the raw lexeme.

Two entry points cover the two source syntaxes: JSON (double-quoted with the
JSON escape set) and TOML (basic, literal, and their multiline forms). JSONL
strings use the JSON decoder.
"""

from __future__ import annotations

_REPLACEMENT = "�"


def decode_json(lexeme: str) -> str:
    """Decode a JSON string lexeme (quotes + escapes) into its code points."""
    if len(lexeme) < 2 or lexeme[0] != '"':
        return lexeme
    return _decode_escaped(lexeme[1:-1], multiline=False)


def decode_toml(lexeme: str) -> str:
    """Decode a TOML string lexeme into its code points, handling all four TOML
    string forms.
    """
    if lexeme.startswith('"""'):
        body = _strip_suffix(_strip_prefix(lexeme, '"""'), '"""')
        body = _trim_leading_newline(body)
        return _decode_escaped(body, multiline=True)
    if lexeme.startswith("'''"):
        body = _strip_suffix(_strip_prefix(lexeme, "'''"), "'''")
        return _trim_leading_newline(body)  # literal: no escapes
    if lexeme.startswith('"'):
        return _decode_escaped(_strip_suffix(_strip_prefix(lexeme, '"'), '"'), multiline=False)
    if lexeme.startswith("'"):
        return _strip_suffix(_strip_prefix(lexeme, "'"), "'")  # literal: no escapes
    return lexeme


def _strip_prefix(s: str, prefix: str) -> str:
    return s[len(prefix):] if s.startswith(prefix) else s


def _strip_suffix(s: str, suffix: str) -> str:
    return s[: -len(suffix)] if s.endswith(suffix) else s


def _trim_leading_newline(s: str) -> str:
    if s.startswith("\r\n"):
        return s[2:]
    if s.startswith("\n"):
        return s[1:]
    return s


def _decode_escaped(s: str, multiline: bool) -> str:
    if "\\" not in s:
        return s
    out: list[str] = []
    i = 0
    n = len(s)
    while i < n:
        c = s[i]
        if c != "\\":
            out.append(c)
            i += 1
            continue
        if i + 1 >= n:
            out.append(c)
            break
        e = s[i + 1]
        if e == '"':
            out.append('"')
            i += 2
        elif e == "\\":
            out.append("\\")
            i += 2
        elif e == "/":
            out.append("/")
            i += 2
        elif e == "b":
            out.append("\b")
            i += 2
        elif e == "f":
            out.append("\f")
            i += 2
        elif e == "n":
            out.append("\n")
            i += 2
        elif e == "r":
            out.append("\r")
            i += 2
        elif e == "t":
            out.append("\t")
            i += 2
        elif e == "u":
            r = _hex_n(s, i + 2, 4)
            if r is not None:
                out.append(r)
                i += 6
            else:
                out.append(c)
                i += 1
        elif e == "U":
            r = _hex_n(s, i + 2, 8)
            if r is not None:
                out.append(r)
                i += 10
            else:
                out.append(c)
                i += 1
        else:
            if multiline and e in ("\n", "\r", " ", "\t"):
                # Line-ending backslash: trim following whitespace incl. newline.
                j = i + 1
                while j < n and s[j] in (" ", "\t", "\n", "\r"):
                    j += 1
                i = j
            else:
                out.append(c)
                i += 1
    return "".join(out)


def _hex_n(s: str, start: int, count: int) -> str | None:
    if start + count > len(s):
        return None
    r = 0
    for k in range(count):
        c = s[start + k]
        if "0" <= c <= "9":
            v = ord(c) - ord("0")
        elif "a" <= c <= "f":
            v = ord(c) - ord("a") + 10
        elif "A" <= c <= "F":
            v = ord(c) - ord("A") + 10
        else:
            return None
        r = (r << 4) | v
    if r > 0x10FFFF or 0xD800 <= r <= 0xDFFF:
        return _REPLACEMENT
    return chr(r)
