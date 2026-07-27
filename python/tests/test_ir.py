"""IR executor unit tests per area (gate-terminal, structural traversal order,
interleaved missing-required, unions, enums, scalars, constraints, depth cap,
JSONL anchors, custom scalars).
"""

import strictspec as ss


def _prog(schema_toml: str, main="s.schema.toml", extra=None):
    files = {main: schema_toml}
    if extra:
        files.update(extra)
    return ss.compile_embedded(files, main)


BASE = """
name = "s"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Root"
"""


def codes(res):
    return [d.code for d in res.diagnostics]


def paths(res):
    return [d.path for d in res.diagnostics]


def test_gate_terminal_suppresses_structural():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.x]
type = "integer"
required = true
""")
    # Missing gate + missing required: only the gate fires (terminal).
    r = p.validate(b'{"y":1}', "json")
    assert codes(r) == ["STRICTSPEC_GATE_ABSENT"]


def test_gate_wrong_type_and_unsupported():
    p = _prog(BASE + '\n[types.Root]\ntype = "record"\n')
    assert codes(p.validate(b'{"format_version":"1"}', "json")) == ["STRICTSPEC_GATE_WRONG_TYPE"]
    assert codes(p.validate(b'{"format_version":2}', "json")) == ["STRICTSPEC_GATE_UNSUPPORTED"]


def test_present_keys_document_order_with_interleaved_missing():
    # Fields declared a,b,c,d all required; document supplies d then a (out of
    # order) and omits b,c. b/c anchor before the first later-declared present.
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.a]
type = "integer"
required = true
[types.Root.fields.b]
type = "integer"
required = true
[types.Root.fields.c]
type = "integer"
required = true
[types.Root.fields.d]
type = "integer"
required = true
""")
    r = p.validate(b'{"format_version":1,"a":1,"d":4}', "json")
    # a present; b and c missing, anchored (in declaration order) BEFORE the
    # later-declared present field d; then d present. So exactly two missing
    # diagnostics, emitted b then c.
    assert codes(r) == [
        "STRICTSPEC_TYPE_MISSING_REQUIRED",
        "STRICTSPEC_TYPE_MISSING_REQUIRED",
    ]
    assert "field b at" in r.diagnostics[0].message
    assert "field c at" in r.diagnostics[1].message


def test_unknown_key_with_suggestion():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.color]
type = "string"
""")
    r = p.validate(b'{"format_version":1,"colr":"x"}', "json")
    assert codes(r) == ["STRICTSPEC_KEY_UNKNOWN"]
    assert "Did you mean color?" in r.diagnostics[0].message


def test_array_bounds_and_item():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.xs]
type = "array"
min_len = 2
[types.Root.fields.xs.item]
type = "integer"
""")
    r = p.validate(b'{"format_version":1,"xs":["a"]}', "json")
    assert "STRICTSPEC_VALUE_ARRAY_TOO_SHORT" in codes(r)
    assert "STRICTSPEC_TYPE_NOT_INTEGER" in codes(r)


def test_enum_member_and_suggestion():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.color]
type = "enum"
values = ["red", "green", "blue"]
""")
    r = p.validate(b'{"format_version":1,"color":"gren"}', "json")
    assert codes(r) == ["STRICTSPEC_TYPE_NOT_ENUM_MEMBER"]
    assert "Did you mean green" in r.diagnostics[0].message


def test_number_scalar_unrepresentable():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.n]
type = "number"
""")
    r = p.validate(b'{"format_version":1,"n":123456789012345678901234567890}', "json")
    assert codes(r) == ["STRICTSPEC_NUM_UNREPRESENTABLE"]


def test_depth_cap():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.n]
type = "Nest"
[types.Nest]
type = "record"
[types.Nest.fields.n]
type = "Nest"
""")
    deep = '{"format_version":1' + ',"n":{"n":' * 200
    deep = '{"format_version":1,"n":' + "{" * 200
    # Build a genuinely deep nested doc.
    inner = "1"
    for _ in range(200):
        inner = '{"n":' + inner + "}"
    doc = '{"format_version":1,"n":' + inner + "}"
    r = p.validate(doc.encode(), "json")
    assert "STRICTSPEC_DEPTH_EXCEEDED" in codes(r)


def test_intra_constraint_exactly_one_of():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.a]
type = "integer"
[types.Root.fields.b]
type = "integer"
[[types.Root.constraints]]
form = "exactly-one-of"
fields = ["a", "b"]
""")
    r = p.validate(b'{"format_version":1,"a":1,"b":2}', "json")
    assert codes(r) == ["STRICTSPEC_INTRA_EXACTLY_ONE_OF"]
    # Phase 2 skipped when phase 1 dirty.
    r2 = p.validate(b'{"format_version":1,"a":"x","b":2}', "json")
    assert "STRICTSPEC_INTRA_EXACTLY_ONE_OF" not in codes(r2)


def test_jsonl_anchor_paths():
    p = _prog(BASE + """
[types.Root]
type = "record"
[types.Root.fields.x]
type = "integer"
required = true
""")
    r = p.validate(b'{"format_version":1,"x":1}\n{"format_version":1}\n', "jsonl")
    # Second line missing x; path carries the @L anchor.
    assert codes(r) == ["STRICTSPEC_TYPE_MISSING_REQUIRED"]
    assert r.diagnostics[0].path.startswith("$@L2:")


def test_discriminated_union_unknown():
    p = _prog(BASE + """
[types.Root]
type = "discriminated-union"
discriminator = "kind"
[types.Root.arms.circle]
type = "record"
[types.Root.arms.circle.fields.kind]
type = "literal"
value = "circle"
[types.Root.arms.square]
type = "record"
[types.Root.arms.square.fields.kind]
type = "literal"
value = "square"
""")
    r = p.validate(b'{"format_version":1,"kind":"circl"}', "json")
    assert codes(r) == ["STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN"]
    assert "Did you mean circle?" in r.diagnostics[0].message
