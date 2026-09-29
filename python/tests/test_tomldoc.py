"""TOML backend tests: a torture document (mirroring the Go tomldoc torture)
covering all four string styles, integer bases, float forms, all datetime
kinds, dotted keys, inline tables, arrays, standard/nested tables, and
array-of-tables, with byte-identical round-trip and span/lexeme exactness.
"""

import pytest

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


# --- Header layout: tomlkit regroups tables, the reader must not care ------
#
# tomlkit folds every [[array-of-tables]] entry, and every table under a
# shared implicit super-table, into one container, so iterating its tree
# visits values in a different order from the source. Each case below pairs a
# layout with the 1-based source line of every scalar; the reader must build
# the same tree and give every scalar the span of its own occurrence (the
# repeated values catch a span that lands on another occurrence).

LAYOUT_CASES = {
    # The failing built-in: a constraints table between two field tables.
    "aot-between-field-tables": (
        '[t.fields.a]\ntype = "string"\n\n[[t.constraints]]\nform = "x"\n\n'
        '[t.fields.b]\ntype = "string"\n',
        {
            "t.fields.a.type": ('"string"', 2),
            "t.constraints[0].form": ('"x"', 5),
            "t.fields.b.type": ('"string"', 8),
        },
    ),
    "aot-entries-around-table": (
        '[[a]]\nx = "1"\n[b]\ny = "1"\n[[a]]\nx = "1"\n',
        {"a[0].x": ('"1"', 2), "a[1].x": ('"1"', 6), "b.y": ('"1"', 4)},
    ),
    "aot-entries-around-sibling-table": (
        '[[a.b]]\nx = "1"\n[a.c]\ny = "1"\n[[a.b]]\nx = "1"\n',
        {"a.b[0].x": ('"1"', 2), "a.b[1].x": ('"1"', 6), "a.c.y": ('"1"', 4)},
    ),
    "nested-aot-around-table": (
        '[[a]]\nx = "1"\n[[a.b]]\ny = "1"\n[c]\nq = "1"\n'
        '[[a]]\nx = "1"\n[[a.b]]\ny = "1"\n[[a.b]]\ny = "2"\n',
        {
            "a[0].x": ('"1"', 2),
            "a[0].b[0].y": ('"1"', 4),
            "a[1].x": ('"1"', 8),
            "a[1].b[0].y": ('"1"', 10),
            "a[1].b[1].y": ('"2"', 12),
            "c.q": ('"1"', 6),
        },
    ),
    "aot-and-super-tables-alternating": (
        '[[t.c]]\nf = "1"\n[t.f.a]\nx = "1"\n[[t.c]]\nf = "1"\n[t.f.b]\nx = "1"\n',
        {
            "t.c[0].f": ('"1"', 2),
            "t.c[1].f": ('"1"', 6),
            "t.f.a.x": ('"1"', 4),
            "t.f.b.x": ('"1"', 8),
        },
    ),
    "super-table-children-around-table": (
        '[t.f.a]\nx = "1"\n[u]\ny = "1"\n[t.f.b]\nz = "1"\n',
        {"t.f.a.x": ('"1"', 2), "t.f.b.z": ('"1"', 6), "u.y": ('"1"', 4)},
    ),
    "parent-table-after-child": (
        '[a.b]\nx = "1"\n[c]\ny = "1"\n[a]\nz = "1"\n',
        {"a.b.x": ('"1"', 2), "a.z": ('"1"', 6), "c.y": ('"1"', 4)},
    ),
    "dotted-keys-in-aot-around-table": (
        '[[g]]\nl.t = "1"\nn = "1"\n[m]\no = "1"\n[[g]]\nl.t = "1"\nn = "1"\n',
        {
            "g[0].l.t": ('"1"', 2),
            "g[0].n": ('"1"', 3),
            "g[1].l.t": ('"1"', 7),
            "g[1].n": ('"1"', 8),
            "m.o": ('"1"', 5),
        },
    ),
    "dotted-keys-split-by-key": (
        'a.x = "1"\nb = "1"\na.y = "1"\n',
        {"a.x": ('"1"', 1), "a.y": ('"1"', 3), "b": ('"1"', 2)},
    ),
    "header-lookalikes-in-values": (
        'm = """\n[[a]]\nx = "1"\n"""\nr = [\n  ["1"],\n]\n'
        '[[a]]\nx = "1" # [b]\n[b]\ny = "1"\n[[a]]\nx = "1"\n',
        {
            "m": ('"""\n[[a]]\nx = "1"\n"""', 1),
            "r[0][0]": ('"1"', 6),
            "a[0].x": ('"1"', 9),
            "a[1].x": ('"1"', 13),
            "b.y": ('"1"', 11),
        },
    ),
    "dotted-key-table-with-header-sub-table": (
        '[f]\na.c = "1"\n[f.a.t]\ns = "1"\n[x]\ny = "1"\n',
        {"f.a.c": ('"1"', 2), "f.a.t.s": ('"1"', 4), "x.y": ('"1"', 6)},
    ),
    "table-under-aot-entries-around-aot": (
        '[[a]]\n[a.b]\nx = "1"\n[[c]]\ny = "1"\n[[a]]\n[a.b]\nx = "1"\n',
        {"a[0].b.x": ('"1"', 3), "a[1].b.x": ('"1"', 8), "c[0].y": ('"1"', 5)},
    ),
    "crlf-line-endings": (
        '[a]\r\nx = "1"\r\n[[b]]\r\ny = "1"\r\n[a.c]\r\nz = "1"\r\n',
        {"a.x": ('"1"', 2), "a.c.z": ('"1"', 6), "b[0].y": ('"1"', 4)},
    ),
    "quoted-header-keys": (
        '[[\'a.b\']]\nx = "1"\n["a"]\ny = "1"\n[[ "a.b" ]]\nx = "1"\n',
        {"a.b[0].x": ('"1"', 2), "a.b[1].x": ('"1"', 6), "a.y": ('"1"', 4)},
    ),
}


def _scalars(n, path=""):
    if n.kind == Kind.RECORD:
        for e in n.entries:
            yield from _scalars(e.value, f"{path}.{e.key}" if path else e.key)
    elif n.kind == Kind.ARRAY:
        for i, it in enumerate(n.items):
            yield from _scalars(it, f"{path}[{i}]")
    else:
        yield path, n


@pytest.mark.parametrize("name", sorted(LAYOUT_CASES))
def test_header_layout_spans(name):
    src, want = LAYOUT_CASES[name]
    raw = src.encode("utf-8")
    d = tomldoc.parse(raw)
    assert d.bytes() == raw
    got = {}
    for path, n in _scalars(d.root):
        assert raw[n.span.start.byte_offset : n.span.end.byte_offset].decode() == n.lexeme
        got[path] = (n.lexeme, n.span.start.line)
    assert got == want


def test_constraints_between_field_tables_keep_semantics():
    interleaved = LAYOUT_CASES["aot-between-field-tables"][0]
    trailing = (
        '[t.fields.a]\ntype = "string"\n\n[t.fields.b]\ntype = "string"\n\n'
        '[[t.constraints]]\nform = "x"\n'
    )
    a = tomldoc.parse(interleaved.encode("utf-8"))
    b = tomldoc.parse(trailing.encode("utf-8"))
    assert {p: n.lexeme for p, n in _scalars(a.root)} == {
        p: n.lexeme for p, n in _scalars(b.root)
    }


def test_array_comment_lines_are_not_items():
    # tomlkit keeps a comment on its own line inside an array as a group whose
    # value is a Null placeholder; that is not an element.
    src = b'a = [\n  # lead\n  "x", # after x\n  # between\n  "y",\n  # trail\n]\nb = [ # c\n]\n'
    d = tomldoc.parse(src)
    a = _field(d.root, "a")
    assert [it.lexeme for it in a.items] == ['"x"', '"y"']
    assert [it.span.start.line for it in a.items] == [3, 5]
    assert _field(d.root, "b").items == ()
