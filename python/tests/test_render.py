"""Renderer tests (port of go/internal/render/render_test.go). The golden
expected strings are copied verbatim from the Go goldens (spec-derived oracle),
not regenerated.
"""

import pytest

from strictspec import _diag as d
from strictspec import _render as render
from strictspec._diag import (
    ArrayVal, BoolVal, DateVal, DatetimeVal, Diagnostic, FloatVal, IntVal,
    Key, NullVal, NumberVal, RecordVal, SlotInt, SlotIdentifier, SlotList,
    SlotString, SlotSuggestion, SlotValue, StringVal, TimeVal, new_path,
)


def _s(**kv):
    return dict(kv)


GOLDEN = [
    (
        Diagnostic("STRICTSPEC_TYPE_NOT_INTEGER", new_path(Key("count")), _s(got=SlotString("float"))),
        "Expected an integer at $.count, got float.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_TYPE_MISMATCH",
            new_path(Key("canvas")),
            _s(expected=SlotString("record"), got=SlotString("array")),
        ),
        "Expected record at $.canvas, got array.",
    ),
    (
        Diagnostic("STRICTSPEC_KEY_UNKNOWN", new_path(Key("config")), _s(key=SlotString("colour"))),
        "Unknown key colour at $.config.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_KEY_UNKNOWN",
            new_path(Key("config")),
            _s(key=SlotString("colr"), suggestion=SlotSuggestion("colr", ("color", "width", "height"))),
        ),
        "Unknown key colr at $.config. Did you mean color?",
    ),
    (
        Diagnostic(
            "STRICTSPEC_VALUE_NUM_TOO_SMALL",
            new_path(Key("age")),
            _s(actual=SlotValue(IntVal(3)), limit=SlotValue(IntVal(18))),
        ),
        "Value 3 at $.age is below the minimum 18.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_VALUE_STRING_TOO_LONG",
            new_path(Key("bio")),
            _s(actual=SlotInt(200), limit=SlotInt(64)),
        ),
        "String at $.bio has 200 code points; maximum is 64.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_VALUE_STRING_REGEX",
            new_path(Key("slug")),
            _s(actual=SlotValue(StringVal("Hello World")), pattern=SlotValue(StringVal("^[a-z-]+$"))),
        ),
        'String "Hello World" at $.slug does not match the required pattern "^[a-z-]+$".',
    ),
    (
        Diagnostic(
            "STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
            new_path(Key("color")),
            _s(
                got=SlotValue(StringVal("gren")),
                expected=SlotList((StringVal("red"), StringVal("green"), StringVal("blue"), StringVal("cyan"))),
                suggestion=SlotSuggestion("gren", ("red", "green", "blue", "cyan")),
            ),
        ),
        'Value "gren" at $.color is not one of ["red", "green", "blue", ...]. Did you mean green or red?',
    ),
    (
        Diagnostic(
            "STRICTSPEC_GATE_UNSUPPORTED",
            new_path(),
            _s(
                got=d.SlotVersion(2),
                schema=SlotIdentifier("canvas"),
                expected=d.SlotVersion(3),
                migset=SlotIdentifier("canvas_v2_v3"),
                invocation=SlotString("strictspec migrate --schema canvas --to 3 doc.json"),
            ),
        ),
        "Document `format_version` is 2, but schema canvas accepts exactly 3 (migration set canvas_v2_v3). Run: strictspec migrate --schema canvas --to 3 doc.json",
    ),
    (
        Diagnostic(
            "STRICTSPEC_INTRA_FORBIDDEN_WHEN",
            new_path(Key("legacy")),
            _s(key=SlotString("legacy"), condition=SlotString('mode == "strict"')),
        ),
        'Field legacy at $.legacy is forbidden when mode == "strict".',
    ),
    (
        Diagnostic(
            "STRICTSPEC_INTRA_EXACTLY_ONE_OF",
            new_path(Key("payment")),
            _s(
                fields=SlotList((StringVal("card"), StringVal("bank"))),
                actual=SlotList((StringVal("card"), StringVal("bank"))),
            ),
        ),
        'Exactly one of ["card", "bank"] must be present at $.payment; found ["card", "bank"].',
    ),
    (
        Diagnostic(
            "STRICTSPEC_NUM_SAFE_INTEGER",
            new_path(Key("id")),
            _s(actual=SlotValue(IntVal(9007199254740993))),
        ),
        "Integer 9007199254740993 at $.id exceeds the safe-integer range (|n| >= 2^53) required by `safe_integers`.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN",
            new_path(Key("shape")),
            _s(
                got=SlotValue(StringVal("circl")),
                expected=SlotList((StringVal("circle"), StringVal("square"))),
                suggestion=SlotSuggestion("circl", ("circle", "square")),
            ),
        ),
        'Discriminator "circl" at $.shape is not one of ["circle", "square"]. Did you mean circle?',
    ),
    (
        Diagnostic(
            "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX",
            new_path().with_anchor(3, 12),
            _s(line=SlotInt(3), detail=SlotString("unexpected end of input")),
        ),
        "JSONL parse error on line 3 at $@L3:12: unexpected end of input.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON",
            new_path(Key("tags")),
            _s(actual=SlotInt(3)),
        ),
        "unwrap_singleton at $.tags requires a single-element array; found 3 elements.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_ALIAS_BOTH_PRESENT",
            new_path(Key("node")),
            _s(alias=SlotIdentifier("colour"), canonical=SlotIdentifier("color")),
        ),
        "Both colour and canonical color are present at $.node; provide exactly one.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_TYPE_NOT_LITERAL",
            new_path(Key("version")),
            _s(expected=SlotValue(IntVal(1)), got=SlotValue(IntVal(2))),
        ),
        "Expected the literal 1 at $.version, got 2.",
    ),
    (
        Diagnostic(
            "STRICTSPEC_INTRA_UNIQUE_BY",
            new_path(Key("users")),
            _s(
                value=SlotValue(StringVal("alice")),
                field=SlotString("username"),
                normalization=SlotString("case-fold"),
            ),
        ),
        'Duplicate value "alice" for unique-by username at $.users (normalization: case-fold).',
    ),
]


@pytest.mark.parametrize("diag_, want", GOLDEN)
def test_render_golden(diag_, want):
    assert render.render(diag_) == want


def _neg_zero():
    z = 0.0
    return -z


def test_render_value():
    tests = [
        (IntVal(1000), "1000"),
        (IntVal(-42), "-42"),
        (IntVal(0), "0"),
        (FloatVal(lexeme="1e3", has_lexeme=True), "1e3"),
        (FloatVal(lexeme="5.0", has_lexeme=True), "5.0"),
        (FloatVal(f=5.0), "5.0"),
        (FloatVal(f=_neg_zero()), "-0.0"),
        (NumberVal("007", True), "007"),
        (NumberVal("1.50", False), "1.50"),
        (StringVal("line\nbreak"), '"line\\nbreak"'),
        (BoolVal(True), "true"),
        (BoolVal(False), "false"),
        (NullVal(), "null"),
        (DateVal("2026-07-27"), "2026-07-27"),
        (TimeVal("13:37:00"), "13:37:00"),
        (DatetimeVal("2026-07-27T13:37:00+00:00"), "2026-07-27T13:37:00+00:00"),
        (ArrayVal(()), "[]"),
        (ArrayVal((IntVal(1), IntVal(2), IntVal(3))), "[1, 2, 3]"),
        (ArrayVal((IntVal(1), IntVal(2), IntVal(3), IntVal(4))), "[1, 2, 3, ...]"),
        (RecordVal((), ()), "{}"),
        (RecordVal(("a", "weird key"), (IntVal(1), BoolVal(True))), '{a: 1, "weird key": true}'),
        (
            RecordVal(("a", "b", "c", "d"), (IntVal(1), IntVal(2), IntVal(3), IntVal(4))),
            "{a: 1, b: 2, c: 3, ...}",
        ),
        (ArrayVal((ArrayVal((ArrayVal((IntVal(1),)),)),)), "[[[...]]]"),
    ]
    for v, want in tests:
        assert render.render_value(v) == want


def test_string_truncation_boundary():
    s63 = "a" * 63
    assert render._render_quoted_string(s63) == '"' + s63 + '"'
    s64 = "a" * 64
    assert render._render_quoted_string(s64) == '"' + s64 + '"'
    s65 = "a" * 65
    assert render._render_quoted_string(s65) == '"' + "a" * 64 + '..."'
    emoji = "\U0001F600" * 65
    assert render._render_quoted_string(emoji) == '"' + "\U0001F600" * 64 + '..."'
    nl = "\n" * 65
    assert render._render_quoted_string(nl) == '"' + "\\n" * 64 + '..."'


def test_render_panics():
    with pytest.raises(render.RenderError):
        render.render(Diagnostic("STRICTSPEC_NOT_A_REAL_CODE", new_path(), {}))
    with pytest.raises(render.RenderError):
        render.render(Diagnostic("STRICTSPEC_TYPE_NOT_INTEGER", new_path(Key("x")), {}))
    with pytest.raises(render.RenderError):
        render.render(
            Diagnostic(
                "STRICTSPEC_TYPE_NOT_INTEGER",
                new_path(Key("x")),
                _s(got=SlotString("float"), bogus=SlotString("x")),
            )
        )
    with pytest.raises(render.RenderError):
        render.render(
            Diagnostic(
                "STRICTSPEC_TYPE_NOT_INTEGER",
                new_path(Key("x")),
                _s(got=SlotString("float"), path=d.SlotPath(new_path())),
            )
        )
