"""Document-model tests (port of go/internal/doc/node_test.go)."""

import pytest

from strictspec import _doc as doc
from strictspec._doc import Kind, Position, Span


def test_kind_string():
    cases = {
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
    for k, want in cases.items():
        assert str(k) == want


def test_scalar_node():
    sp = Span(Position(1, 1, 0), Position(1, 6, 5))
    n = doc.new_scalar(Kind.INTEGER, "1_000", sp)
    assert n.kind == Kind.INTEGER
    assert n.lexeme == "1_000"
    assert n.span == sp
    assert n.entries == ()
    assert n.items == ()


def test_new_scalar_rejects_container_kind():
    for k in (Kind.RECORD, Kind.ARRAY):
        with pytest.raises(ValueError):
            doc.new_scalar(k, "", Span())


def test_record_node():
    child = doc.new_scalar(Kind.STRING, '"v"', Span())
    n = doc.new_record([doc.Entry(key="k", value=child)], Span())
    assert n.kind == Kind.RECORD
    assert n.lexeme == ""
    assert len(n.entries) == 1 and n.entries[0].key == "k"
    assert n.items == ()


def test_array_node():
    items = [doc.new_scalar(Kind.INTEGER, "1", Span()), doc.new_scalar(Kind.INTEGER, "2", Span())]
    n = doc.new_array(items, Span())
    assert n.kind == Kind.ARRAY
    assert len(n.items) == 2
    assert n.entries == ()


def test_document_bytes_copy_independence():
    src = bytearray(b"a = 1\n")
    d = doc.new_document(doc.FORMAT_TOML, doc.new_record((), Span()), bytes(src))
    src[0] = ord("X")
    assert d.bytes() == b"a = 1\n"
    b = bytearray(d.bytes())
    b[0] = ord("Z")
    assert d.bytes() == b"a = 1\n"


def test_parse_error_message():
    e = doc.ParseError(doc.FORMAT_TOML, Position(3, 5, 20), "boom")
    assert str(e) == "toml parse error at line 3, column 5: boom"
