// Public runtime tests (port of python test_public.py), plus the version-pairing
// drift guard: the runtime VERSION must match the packaged package.json version
// so lazy-download + pairing agree. Because VERSION is read directly from
// package.json, this is an executable invariant with no second source to drift.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
	checkRuntimeVersion,
	compileFromSource,
	Kind,
	loadValue,
	PairingError,
	type Program,
	requireRuntimeVersion,
	VERSION,
	versionGate,
} from "../dist/index.js";

const MINI = `
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
`;

function compile(): Program {
	return compileFromSource({ "point.schema.toml": MINI }, "point.schema.toml");
}

test("VERSION matches package.json (drift-impossibility)", () => {
	// Compiled tests are flattened to dist-test/*.test.js; package.json is at the
	// ts/ root, one level up.
	const pkgUrl = new URL("../package.json", import.meta.url);
	const pkg = JSON.parse(readFileSync(fileURLToPath(pkgUrl), "utf-8")) as {
		version: string;
	};
	assert.equal(VERSION, pkg.version);
});

test("pairing guard", () => {
	assert.equal(checkRuntimeVersion(VERSION), null);
	assert.notEqual(checkRuntimeVersion("9.9.9"), null);
	assert.throws(() => requireRuntimeVersion("9.9.9"), PairingError);
});

test("validate raw text valid", () => {
	const p = compile();
	const res = p.validate('{"format_version":1,"x":3,"label":"ok"}', "json");
	assert.ok(res.valid, JSON.stringify(res.diagnostics));
});

test("validate raw text invalid", () => {
	const p = compile();
	const res = p.validate('{"format_version":1,"x":"nope","label":""}', "json");
	assert.ok(!res.valid);
	assert.deepEqual(
		res.diagnostics.map((d) => d.code),
		["STRICTSPEC_TYPE_NOT_INTEGER", "STRICTSPEC_VALUE_STRING_EMPTY"],
	);
	for (const d of res.diagnostics) {
		assert.notEqual(d.message, "");
	}
});

test("gate terminal", () => {
	const p = compile();
	const res = p.validate('{"x":3,"label":"ok"}', "json");
	assert.ok(!res.valid);
	assert.deepEqual(
		res.diagnostics.map((d) => d.code),
		["STRICTSPEC_GATE_ABSENT"],
	);
});

test("validate_value entry point", () => {
	const p = compile();
	const v = loadValue('{"format_version":1,"x":3,"label":"ok"}', "json");
	assert.ok(p.validateValue(v).valid);
});

test("coercers", () => {
	const v = loadValue('{"x":3,"label":"hi","ratio":1.5,"flag":true}', "json");
	assert.equal(v.kind(), Kind.Record);
	const [xf, okX] = v.field("x");
	assert.ok(okX);
	assert.deepEqual(xf.int(), [3, true]);
	const [rf] = v.field("ratio");
	assert.equal(rf.int()[1], false); // float must not coerce to int
	assert.deepEqual(rf.float(), [1.5, true]);
	const [lf] = v.field("label");
	assert.deepEqual(lf.string(), ["hi", true]);
	const [ff] = v.field("flag");
	assert.deepEqual(ff.bool(), [true, true]);
});

test("version gate helper", () => {
	const p = compile();
	const g = versionGate(p, '{"x":3}', "json");
	assert.ok(!g.ok);
	assert.equal(g.diagnostics[0]?.code, "STRICTSPEC_GATE_ABSENT");
	const g2 = versionGate(p, '{"format_version":1,"x":3,"label":"ok"}', "json");
	assert.ok(g2.ok);
	assert.deepEqual(g2.diagnostics, []);
});

test("result is frozen", () => {
	const p = compile();
	const res = p.validate('{"format_version":1,"x":3,"label":"ok"}', "json");
	assert.ok(Object.isFrozen(res));
	assert.ok(Object.isFrozen(res.diagnostics));
});
