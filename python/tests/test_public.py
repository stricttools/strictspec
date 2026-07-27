"""Public runtime tests (port of go/strictspec/strictspec_test.go), plus the
version-pairing drift guard mirroring the Go fix pattern: the runtime version
must match the packaged pyproject version so lazy-download + pairing agree.
"""

import tomllib
from pathlib import Path

import pytest

import strictspec as ss

MINI = """
name = "point"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Point"

[types.Point]
type = "record"
[types.Point.fields.x]
type = "integer"
required = true
[types.Point.fields.label]
type = "string"
required = true
non_empty = true
"""


def _compile():
    return ss.compile_embedded({"point.schema.toml": MINI}, "point.schema.toml")


def test_version_matches_pyproject():
    pyproject = Path(__file__).resolve().parents[1] / "pyproject.toml"
    data = tomllib.loads(pyproject.read_text(encoding="utf-8"))
    assert ss.__version__ == data["project"]["version"], (
        "runtime __version__ must match pyproject [project].version (rlsbl bumps both); "
        "drift breaks the exact version-pairing rule"
    )
    assert ss.Version == ss.__version__


def test_pairing_guard():
    assert ss.check_runtime_version(ss.Version) is None
    assert ss.check_runtime_version("9.9.9") is not None
    with pytest.raises(ss.PairingError):
        ss.require_runtime_version("9.9.9")


def test_validate_bytes_valid():
    p = _compile()
    res = p.validate(b'{"format_version":1,"x":3,"label":"ok"}', "json")
    assert res.valid, res.diagnostics


def test_validate_bytes_invalid():
    p = _compile()
    res = p.validate(b'{"format_version":1,"x":"nope","label":""}', "json")
    assert not res.valid
    want = ["STRICTSPEC_TYPE_NOT_INTEGER", "STRICTSPEC_VALUE_STRING_EMPTY"]
    assert [d.code for d in res.diagnostics] == want
    for d in res.diagnostics:
        assert d.message != ""


def test_gate_terminal():
    p = _compile()
    res = p.validate(b'{"x":3,"label":"ok"}', "json")
    assert not res.valid
    assert [d.code for d in res.diagnostics] == ["STRICTSPEC_GATE_ABSENT"]


def test_validate_value_entry_point():
    p = _compile()
    v = ss.load_value(b'{"format_version":1,"x":3,"label":"ok"}', "json")
    assert p.validate_value(v).valid


def test_coercers():
    v = ss.load_value(b'{"x":3,"label":"hi","ratio":1.5,"flag":true}', "json")
    assert v.kind() == ss.Kind.RECORD
    xf, ok = v.field("x")
    assert ok
    assert xf.int() == (3, True)
    rf, _ = v.field("ratio")
    assert rf.int()[1] is False  # float must not coerce to int
    assert rf.float() == (1.5, True)
    lf, _ = v.field("label")
    assert lf.string() == ("hi", True)
    ff, _ = v.field("flag")
    assert ff.bool() == (True, True)


def test_version_gate_helper():
    p = _compile()
    g = ss.version_gate(p, b'{"x":3}', "json")
    assert not g.ok
    assert g.diagnostics[0].code == "STRICTSPEC_GATE_ABSENT"
    g2 = ss.version_gate(p, b'{"format_version":1,"x":3,"label":"ok"}', "json")
    assert g2.ok
    assert g2.diagnostics == ()
