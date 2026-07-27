"""JSON/JSONL backend tests (port of go/internal/jsondoc/*_test.go), including
the torture document, the number-classification table, the 17-case invalid
input table, and JSONL framing/global-offset/streaming behavior.
"""

import io

import pytest

from strictspec import _doc as doc
from strictspec import _jsondoc as jsondoc
from strictspec._doc import Kind

# Byte-identical copy of the Go torture document.
TORTURE = (
    '{\n'
    '  "title": "basic \\"quoted\\" string",\n'
    '  "escapes": "tab\\tnewline\\nreturn\\rbackslash\\\\slash\\/bfk\\b\\fbmpéastral\U0001d11e",\n'
    '  "ünîcodé": "key written in raw UTF-8",\n'
    '  "empty_string": "",\n'
    '  "empty_object": {},\n'
    '  "empty_array": [],\n'
    '  "numbers": {\n'
    '    "int": 42,\n'
    '    "neg": -17,\n'
    '    "zero": 0,\n'
    '    "neg_zero": -0,\n'
    '    "float": 3.14,\n'
    '    "neg_zero_float": -0.0,\n'
    '    "exp_lower": 1e5,\n'
    '    "exp_upper": 1E-5,\n'
    '    "exp_signed": 6.626e-34,\n'
    '    "small_frac": 0.1,\n'
    '    "big_beyond_f64": 123456789012345678901234567890,\n'
    '    "big_float": 1.7976931348623159e308\n'
    '  },\n'
    '  "flags": [true, false, null],\n'
    '  "nested": {\n'
    '    "level1": {\n'
    '      "level2": {\n'
    '        "items": [1, 2, [3, 4, {"deep": "value"}]]\n'
    '      }\n'
    '    }\n'
    '  },\n'
    '  "long": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"\n'
    '}'
)


def _field(n, key):
    for e in n.entries:
        if e.key == key:
            return e.value
    raise AssertionError(f"key {key!r} not found")


def _walk_scalars(n, f):
    if n.kind == Kind.RECORD:
        for e in n.entries:
            _walk_scalars(e.value, f)
    elif n.kind == Kind.ARRAY:
        for it in n.items:
            _walk_scalars(it, f)
    else:
        f(n)


def test_roundtrip_byte_identity():
    d = jsondoc.parse(TORTURE.encode("utf-8"))
    assert d.bytes().decode("utf-8") == TORTURE
    assert d.format == doc.FORMAT_JSON


def test_span_lexeme_exactness():
    d = jsondoc.parse(TORTURE.encode("utf-8"))
    src = d.bytes()
    count = 0

    def check(n):
        nonlocal count
        count += 1
        sp = n.span
        assert sp.is_valid()
        s, e = sp.start.byte_offset, sp.end.byte_offset
        assert 0 <= s <= e <= len(src)
        assert src[s:e].decode("utf-8") == n.lexeme

    _walk_scalars(d.root, check)
    assert count > 0


def test_key_span_exactness():
    d = jsondoc.parse(TORTURE.encode("utf-8"))
    src = d.bytes()
    seen = 0

    def check(n):
        nonlocal seen
        if n.kind == Kind.RECORD:
            for e in n.entries:
                sp = e.key_span
                assert sp.is_valid()
                raw = src[sp.start.byte_offset : sp.end.byte_offset]
                assert len(raw) >= 2 and raw[0:1] == b'"' and raw[-1:] == b'"'
                seen += 1
                check(e.value)
        elif n.kind == Kind.ARRAY:
            for it in n.items:
                check(it)

    check(d.root)
    assert seen > 0


def test_number_lexeme_classification():
    d = jsondoc.parse(TORTURE.encode("utf-8"))
    nums = _field(d.root, "numbers")
    cases = [
        ("int", Kind.INTEGER, "42"),
        ("neg", Kind.INTEGER, "-17"),
        ("zero", Kind.INTEGER, "0"),
        ("neg_zero", Kind.INTEGER, "-0"),
        ("float", Kind.FLOAT, "3.14"),
        ("neg_zero_float", Kind.FLOAT, "-0.0"),
        ("exp_lower", Kind.FLOAT, "1e5"),
        ("exp_upper", Kind.FLOAT, "1E-5"),
        ("exp_signed", Kind.FLOAT, "6.626e-34"),
        ("small_frac", Kind.FLOAT, "0.1"),
        ("big_beyond_f64", Kind.INTEGER, "123456789012345678901234567890"),
        ("big_float", Kind.FLOAT, "1.7976931348623159e308"),
    ]
    for key, kind, lexeme in cases:
        n = _field(nums, key)
        assert n.kind == kind, key
        assert n.lexeme == lexeme, key


def test_ordered_entries_preserved():
    d = jsondoc.parse(b'{"z":1,"a":2,"m":3,"b":4}')
    assert [e.key for e in d.root.entries] == ["z", "a", "m", "b"]


def test_escape_decoding():
    cases = [
        (b'{"a\\tb":1}', "a\tb"),
        (b'{"quote\\"end":1}', 'quote"end'),
        ('{"bmp\\u00e9":1}'.encode(), "bmpé"),
        (b'{"clef\\ud834\\udd1e":1}', "clef\U0001d11e"),
        (b'{"slash\\/end":1}', "slash/end"),
        (b'{"nl\\ncr\\r":1}', "nl\ncr\r"),
    ]
    for src, decoded in cases:
        d = jsondoc.parse(src)
        assert d.root.entries[0].key == decoded
        assert d.bytes() == src


def test_bare_scalar_and_array_documents():
    cases = [
        (b"42", Kind.INTEGER),
        (b"3.14", Kind.FLOAT),
        (b'"hi"', Kind.STRING),
        (b"true", Kind.BOOL),
        (b"null", Kind.NULL),
        (b"[1,2,3]", Kind.ARRAY),
        (b'  "padded"  ', Kind.STRING),
    ]
    for src, kind in cases:
        d = jsondoc.parse(src)
        assert d.root.kind == kind
        assert d.bytes() == src


def test_duplicate_key_error():
    with pytest.raises(doc.ParseError) as ei:
        jsondoc.parse(b'{"a":1,"a":2}')
    pe = ei.value
    assert (pe.position.byte_offset, pe.position.line, pe.position.column) == (7, 1, 8)
    with pytest.raises(doc.ParseError):
        jsondoc.parse(b'{"ab":1,"ab":2}')


def test_parse_errors_with_positions():
    # The 17-case invalid-input table (positions verbatim from the Go table).
    cases = [
        ("unterminated object", b'{"a":1', 1, 7, 6),
        ("unterminated array", b"[1,2", 1, 5, 4),
        ("unterminated string", b'"nope', 1, 6, 5),
        ("missing colon", b'{"a" 1}', 1, 6, 5),
        ("missing value", b'{"a":}', 1, 6, 5),
        ("trailing content", b"123 456", 1, 5, 4),
        ("invalid literal", b"nul", 1, 1, 0),
        ("trailing comma array", b"[1,]", 1, 4, 3),
        ("trailing comma object", b'{"a":1,}', 1, 8, 7),
        ("bare NaN", b"NaN", 1, 1, 0),
        ("bare Infinity", b"Infinity", 1, 1, 0),
        ("bad escape", b'"\\x"', 1, 2, 1),
        ("fraction no digit", b"1.", 1, 3, 2),
        ("exponent no digit", b"1e", 1, 3, 2),
        ("control char in string", b'"line\nbreak"', 1, 6, 5),
        ("empty input", b"", 1, 1, 0),
        ("whitespace only", b"   \n  ", 1, 1, 0),
    ]
    for name, src, line, col, offset in cases:
        with pytest.raises(doc.ParseError) as ei:
            jsondoc.parse(src)
        pe = ei.value
        assert pe.format == doc.FORMAT_JSON, name
        assert (pe.position.line, pe.position.column, pe.position.byte_offset) == (
            line,
            col,
            offset,
        ), name
        assert pe.position.is_valid()


def test_invalid_utf8_position():
    with pytest.raises(doc.ParseError) as ei:
        jsondoc.parse(b'"\xff"')
    pe = ei.value
    assert (pe.position.byte_offset, pe.position.line, pe.position.column) == (1, 1, 2)


def test_deep_nesting_bounded():
    n = jsondoc.MAX_DEPTH + 50
    src = b"[" * n + b"]" * n
    with pytest.raises(doc.ParseError):
        jsondoc.parse(src)
    ok = b"[" * 100 + b"1" + b"]" * 100
    jsondoc.parse(ok)  # must not raise


# --- JSONL -----------------------------------------------------------------


def test_jsonl_multiline_valid():
    src = b'{"a":1}\n[1,2,3]\n"bare"\n42\ntrue\n'
    docs = jsondoc.parse_lines(src)
    assert len(docs) == 5
    kinds = [Kind.RECORD, Kind.ARRAY, Kind.STRING, Kind.INTEGER, Kind.BOOL]
    wbytes = [b'{"a":1}', b"[1,2,3]", b'"bare"', b"42", b"true"]
    for i, d in enumerate(docs):
        assert d.format == doc.FORMAT_JSONL
        assert d.root.kind == kinds[i]
        assert d.bytes() == wbytes[i]


def test_jsonl_global_positions():
    src = b'{"a":1}\n{"b":2}\n{"c":333}\n'
    docs = jsondoc.parse_lines(src)
    assert len(docs) == 3
    c = _field(docs[2].root, "c")
    sp = c.span
    assert c.lexeme == "333"
    assert sp.start.line == 3
    assert sp.start.byte_offset == 21
    assert src[sp.start.byte_offset : sp.end.byte_offset] == b"333"


def test_jsonl_final_line_without_lf():
    assert len(jsondoc.parse_lines(b'{"a":1}\n{"b":2}\n')) == 2
    assert len(jsondoc.parse_lines(b'{"a":1}\n{"b":2}')) == 2
    single = jsondoc.parse_lines(b"42")
    assert len(single) == 1 and single[0].root.lexeme == "42"


def test_jsonl_empty_input():
    assert jsondoc.parse_lines(b"") == []


def test_jsonl_blank_line_error():
    for name, src, line, offset in [
        ("empty middle line", b"{}\n\n{}\n", 2, 3),
        ("whitespace middle line", b"{}\n   \n{}\n", 2, 3),
        ("leading blank line", b"\n{}\n", 1, 0),
    ]:
        with pytest.raises(doc.ParseError) as ei:
            jsondoc.parse_lines(src)
        pe = ei.value
        assert pe.format == doc.FORMAT_JSONL, name
        assert (pe.position.line, pe.position.byte_offset) == (line, offset), name
        assert "lank line" in pe.message


def test_jsonl_trailing_cr_error():
    with pytest.raises(doc.ParseError) as ei:
        jsondoc.parse_lines(b"{}\r\n{}\n")
    pe = ei.value
    assert (pe.position.line, pe.position.column, pe.position.byte_offset) == (1, 3, 2)
    assert "carriage return" in pe.message


def test_jsonl_duplicate_key_per_line():
    with pytest.raises(doc.ParseError) as ei:
        jsondoc.parse_lines(b'{"a":1}\n{"b":2,"b":3}\n')
    pe = ei.value
    assert pe.position.line == 2
    assert "duplicate" in pe.message


def _flatten(n):
    out = []

    def walk(n):
        if n.kind == Kind.RECORD:
            out.append("{")
            for e in n.entries:
                out.append(e.key + "=")
                walk(e.value)
                out.append(";")
            out.append("}")
        elif n.kind == Kind.ARRAY:
            out.append("[")
            for it in n.items:
                walk(it)
                out.append(";")
            out.append("]")
        else:
            sp = n.span
            out.append(f"{n.kind}:{n.lexeme}@{sp.start.byte_offset}-{sp.end.byte_offset}")

    walk(n)
    return "".join(out)


def test_stream_slice_parity():
    src = b'{"a":1}\n[1,2,3]\n"x"\n{"nested":{"k":true}}\n42\n'
    slice_docs = jsondoc.parse_lines(src)
    streamed = []
    jsondoc.parse_stream(io.BytesIO(src), streamed.append)
    assert len(slice_docs) == len(streamed)
    for a, b in zip(slice_docs, streamed):
        assert _flatten(a.root) == _flatten(b.root)
        assert a.bytes() == b.bytes()
