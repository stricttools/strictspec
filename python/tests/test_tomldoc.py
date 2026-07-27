"""TOML backend tests: a torture document (mirroring the Go tomldoc torture)
covering all four string styles, integer bases, float forms, all datetime
kinds, dotted keys, inline tables, arrays, standard/nested tables, and
array-of-tables, with byte-identical round-trip and span/lexeme exactness.
"""

from strictspec import _doc as doc
from strictspec import _tomldoc as tomldoc
from strictspec._doc import Kind

TORTURE = """# Top-level document comment
# second comment line

title = "basic \\"quoted\\" string"   # inline comment on a key
literal = 'C:\\path\\no\\escape'
multiline = \"\"\"
line one
line two\"\"\"
multiline_literal = '''raw
lines'''

dec = 1_000
neg = -17
hex = 0xDEAD_beef
oct = 0o755
bin = 0b1010
big = 9_223_372_036_854_775_807

f1 = 1.0
f2 = 3.14
f3 = 1e5
negzero = -0.0
planck = 6.626e-34
inf_pos = inf
inf_neg = -inf
not_a_num = nan

yes = true
no = false

odt = 1979-05-27T07:32:00Z
ldt = 1979-05-27T07:32:00
ld = 1979-05-27
lt = 07:32:00

fruit.name = "apple"
fruit.color = "red"

inline = { x = 1, y = 2.5, label = "pt" }
arr = [1, 2, 3]
nested_arr = [
  "a",
  "b",
]

[table_a]
key = "value"   # trailing comment inside a table
count = 42

[table_a.sub]
deep = true

[[products]]
name = "hammer"
sku = 738594937

[[products]]
name = "nail"
sku = 284758393
"""


def _field(n, key):
    for e in n.entries:
        if e.key == key:
            return e.value
    raise AssertionError(f"key {key!r} not found")


def test_roundtrip_byte_identity():
    src = TORTURE.encode("utf-8")
    d = tomldoc.parse(src)
    assert d.bytes() == src
    assert d.format == doc.FORMAT_TOML


def test_span_lexeme_exactness():
    src = TORTURE.encode("utf-8")
    d = tomldoc.parse(src)
    count = 0

    def check(n):
        nonlocal count
        if n.kind not in (Kind.RECORD, Kind.ARRAY):
            count += 1
            sp = n.span
            assert sp.is_valid()
            assert src[sp.start.byte_offset : sp.end.byte_offset].decode("utf-8") == n.lexeme
        for e in n.entries:
            check(e.value)
        for it in n.items:
            check(it)

    check(d.root)
    assert count > 0


def test_scalar_kinds_and_lexemes():
    d = tomldoc.parse(TORTURE.encode("utf-8"))
    cases = [
        ("title", Kind.STRING, '"basic \\"quoted\\" string"'),
        ("literal", Kind.STRING, "'C:\\path\\no\\escape'"),
        ("dec", Kind.INTEGER, "1_000"),
        ("hex", Kind.INTEGER, "0xDEAD_beef"),
        ("oct", Kind.INTEGER, "0o755"),
        ("bin", Kind.INTEGER, "0b1010"),
        ("f1", Kind.FLOAT, "1.0"),
        ("negzero", Kind.FLOAT, "-0.0"),
        ("inf_pos", Kind.FLOAT, "inf"),
        ("inf_neg", Kind.FLOAT, "-inf"),
        ("not_a_num", Kind.FLOAT, "nan"),
        ("yes", Kind.BOOL, "true"),
        ("no", Kind.BOOL, "false"),
        ("odt", Kind.DATETIME_OFFSET, "1979-05-27T07:32:00Z"),
        ("ldt", Kind.DATETIME_LOCAL, "1979-05-27T07:32:00"),
        ("ld", Kind.DATE_LOCAL, "1979-05-27"),
        ("lt", Kind.TIME_LOCAL, "07:32:00"),
    ]
    for key, kind, lexeme in cases:
        n = _field(d.root, key)
        assert n.kind == kind, key
        assert n.lexeme == lexeme, key


def test_dotted_keys_merge():
    d = tomldoc.parse(TORTURE.encode("utf-8"))
    fruit = _field(d.root, "fruit")
    assert fruit.kind == Kind.RECORD
    assert [e.key for e in fruit.entries] == ["name", "color"]


def test_inline_table_and_arrays():
    d = tomldoc.parse(TORTURE.encode("utf-8"))
    inline = _field(d.root, "inline")
    assert inline.kind == Kind.RECORD
    assert [e.key for e in inline.entries] == ["x", "y", "label"]
    arr = _field(d.root, "arr")
    assert arr.kind == Kind.ARRAY and len(arr.items) == 3
    na = _field(d.root, "nested_arr")
    assert na.kind == Kind.ARRAY and [it.lexeme for it in na.items] == ['"a"', '"b"']


def test_nested_tables_and_array_of_tables():
    d = tomldoc.parse(TORTURE.encode("utf-8"))
    ta = _field(d.root, "table_a")
    assert ta.kind == Kind.RECORD
    assert _field(ta, "key").lexeme == '"value"'
    sub = _field(ta, "sub")
    assert _field(sub, "deep").lexeme == "true"
    products = _field(d.root, "products")
    assert products.kind == Kind.ARRAY and len(products.items) == 2
    assert _field(products.items[0], "name").lexeme == '"hammer"'
    assert _field(products.items[1], "sku").lexeme == "284758393"


def test_parse_error_position():
    import pytest

    with pytest.raises(doc.ParseError) as ei:
        tomldoc.parse(b"a = = 1\n")
    assert ei.value.format == doc.FORMAT_TOML
    assert ei.value.position.line == 1
