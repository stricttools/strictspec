// IR executor unit tests per area (gate-terminal, structural traversal order,
// interleaved missing-required, unions, enums, scalars, constraints, depth cap,
// JSONL anchors). Port of python test_ir.py.

import assert from "node:assert/strict";
import { test } from "node:test";
import { compileFromSource, type Program, type Result } from "../dist/index.js";

function prog(schemaToml: string, extra?: Record<string, string>): Program {
	const main = "s.schema.toml";
	const files: Record<string, string> = { [main]: schemaToml };
	if (extra) {
		Object.assign(files, extra);
	}
	return compileFromSource(files, main);
}

const BASE = `
name = "s"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Root"
`;

function codes(res: Result): string[] {
	return res.diagnostics.map((d) => d.code);
}

test("gate terminal suppresses structural", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.x]
type = "integer"
required = true
`);
	const r = p.validate('{"y":1}', "json");
	assert.deepEqual(codes(r), ["STRICTSPEC_GATE_ABSENT"]);
});

test("gate wrong type and unsupported", () => {
	const p = prog(`${BASE}\n[types.Root]\ntype = "record"\n`);
	assert.deepEqual(codes(p.validate('{"format_version":"1"}', "json")), [
		"STRICTSPEC_GATE_WRONG_TYPE",
	]);
	assert.deepEqual(codes(p.validate('{"format_version":2}', "json")), [
		"STRICTSPEC_GATE_UNSUPPORTED",
	]);
});

test("present keys document order with interleaved missing", () => {
	const p = prog(`${BASE}
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
`);
	const r = p.validate('{"format_version":1,"a":1,"d":4}', "json");
	assert.deepEqual(codes(r), [
		"STRICTSPEC_TYPE_MISSING_REQUIRED",
		"STRICTSPEC_TYPE_MISSING_REQUIRED",
	]);
	assert.ok(r.diagnostics[0]?.message.includes("field b at"));
	assert.ok(r.diagnostics[1]?.message.includes("field c at"));
});

test("unknown key with suggestion", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.color]
type = "string"
`);
	const r = p.validate('{"format_version":1,"colr":"x"}', "json");
	assert.deepEqual(codes(r), ["STRICTSPEC_KEY_UNKNOWN"]);
	assert.ok(r.diagnostics[0]?.message.includes("Did you mean color?"));
});

test("array bounds and item", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.xs]
type = "array"
min_len = 2
[types.Root.fields.xs.item]
type = "integer"
`);
	const r = p.validate('{"format_version":1,"xs":["a"]}', "json");
	assert.ok(codes(r).includes("STRICTSPEC_VALUE_ARRAY_TOO_SHORT"));
	assert.ok(codes(r).includes("STRICTSPEC_TYPE_NOT_INTEGER"));
});

test("enum member and suggestion", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.color]
type = "enum"
values = ["red", "green", "blue"]
`);
	const r = p.validate('{"format_version":1,"color":"gren"}', "json");
	assert.deepEqual(codes(r), ["STRICTSPEC_TYPE_NOT_ENUM_MEMBER"]);
	assert.ok(r.diagnostics[0]?.message.includes("Did you mean green"));
});

test("number scalar unrepresentable", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.n]
type = "number"
`);
	const r = p.validate(
		'{"format_version":1,"n":123456789012345678901234567890}',
		"json",
	);
	assert.deepEqual(codes(r), ["STRICTSPEC_NUM_UNREPRESENTABLE"]);
});

test("depth cap", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.n]
type = "Nest"
[types.Nest]
type = "record"
[types.Nest.fields.n]
type = "Nest"
`);
	let inner = "1";
	for (let i = 0; i < 200; i++) {
		inner = `{"n":${inner}}`;
	}
	const docText = `{"format_version":1,"n":${inner}}`;
	const r = p.validate(docText, "json");
	assert.ok(codes(r).includes("STRICTSPEC_DEPTH_EXCEEDED"));
});

test("intra constraint exactly-one-of", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.a]
type = "integer"
[types.Root.fields.b]
type = "integer"
[[types.Root.constraints]]
form = "exactly-one-of"
fields = ["a", "b"]
`);
	const r = p.validate('{"format_version":1,"a":1,"b":2}', "json");
	assert.deepEqual(codes(r), ["STRICTSPEC_INTRA_EXACTLY_ONE_OF"]);
	// Phase 2 skipped when phase 1 dirty.
	const r2 = p.validate('{"format_version":1,"a":"x","b":2}', "json");
	assert.ok(!codes(r2).includes("STRICTSPEC_INTRA_EXACTLY_ONE_OF"));
});

test("jsonl anchor paths", () => {
	const p = prog(`${BASE}
[types.Root]
type = "record"
[types.Root.fields.x]
type = "integer"
required = true
`);
	const r = p.validate(
		'{"format_version":1,"x":1}\n{"format_version":1}\n',
		"jsonl",
	);
	assert.deepEqual(codes(r), ["STRICTSPEC_TYPE_MISSING_REQUIRED"]);
	assert.ok(r.diagnostics[0]?.path.startsWith("$@L2:"));
});

test("discriminated union unknown", () => {
	const p = prog(`${BASE}
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
`);
	const r = p.validate('{"format_version":1,"kind":"circl"}', "json");
	assert.deepEqual(codes(r), ["STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN"]);
	assert.ok(r.diagnostics[0]?.message.includes("Did you mean circle?"));
});
