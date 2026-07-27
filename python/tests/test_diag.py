"""Diagnostics/path tests (port of go/internal/diag/path_test.go)."""

from strictspec import _diag as diag
from strictspec._diag import Arm, Index, Key, MapKey, new_path


def test_path_render():
    tests = [
        (new_path(), "$"),
        (new_path(Key("a")), "$.a"),
        (new_path(Key("a"), Key("b")), "$.a.b"),
        (new_path(Key("items"), Index(0)), "$.items[0]"),
        (new_path(Key("items"), Index(42)), "$.items[42]"),
        (new_path(Key("a-b"), Key("c_d")), "$.a-b.c_d"),
        (new_path(Key("config"), Key("weird key")), '$.config["weird key"]'),
        (new_path(Key("m"), Key("1x")), '$.m["1x"]'),
        (new_path(Key("m"), Key("")), '$.m[""]'),
        (new_path(Key('a"b')), '$["a\\"b"]'),
        (new_path(Key("a\\b")), '$["a\\\\b"]'),
        (new_path(Key("a\nb")), '$["a\\nb"]'),
        (new_path(Key("a\tb")), '$["a\\tb"]'),
        (new_path(Key("a\x01b")), '$["a\\u0001b"]'),
        (new_path(Key("shape"), Arm("gradient"), Key("stops"), Index(0)), "$.shape(gradient).stops[0]"),
        (new_path().with_anchor(42, 0), "$@L42:0"),
        (new_path(Key("budget")).with_anchor(42, 17), "$.budget@L42:17"),
        (new_path(Key("rows"), Index(3)).with_anchor(3, 12), "$.rows[3]@L3:12"),
    ]
    for path, want in tests:
        assert path.render() == want


def test_is_ident_shaped():
    for s in ["a", "abc", "_x", "a1", "a-b", "c_d", "A", "Content-Type", "x-y-z"]:
        assert diag.is_ident_shaped(s), s
    for s in ["", "1x", "-a", "a b", "a.b", "a/b", 'a"b', "trailing ", "é"]:
        assert not diag.is_ident_shaped(s), s


def test_escape_string():
    tests = [
        ("", ""),
        ("plain", "plain"),
        ('a"b', 'a\\"b'),
        ("a\\b", "a\\\\b"),
        ("a\nb", "a\\nb"),
        ("a\rb", "a\\rb"),
        ("a\tb", "a\\tb"),
        ("a\x00b", "a\\u0000b"),
        ("a\x1fb", "a\\u001fb"),
        ("unicodé", "unicodé"),
        ("emoji \U0001F600", "emoji \U0001F600"),
    ]
    for s, want in tests:
        assert diag.escape_string(s) == want


def test_diagnostics_accumulation_order():
    d = diag.Diagnostics()
    assert len(d) == 0
    d.emit_code("STRICTSPEC_A", new_path(Key("x")), None)
    d.emit(diag.Diagnostic("STRICTSPEC_B", new_path(Key("y")), {}))
    d.emit_code("STRICTSPEC_C", new_path(Key("z")), None)
    assert [x.code for x in d.all()] == ["STRICTSPEC_A", "STRICTSPEC_B", "STRICTSPEC_C"]
