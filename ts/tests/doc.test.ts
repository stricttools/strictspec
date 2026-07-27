// Document-model tests (port of go/internal/doc/node_test.go and python test_doc.py).

import assert from "node:assert/strict";
import { test } from "node:test";
import {
	Document,
	type Entry,
	FORMAT_TOML,
	Kind,
	kindString,
	newArray,
	newDocument,
	newPosition,
	newRecord,
	newScalar,
	newSpan,
	ParseError,
} from "../dist/doc.js";

test("kind string", () => {
	const cases: Array<[Kind, string]> = [
		[Kind.Record, "Record"],
		[Kind.Array, "Array"],
		[Kind.String, "String"],
		[Kind.Integer, "Integer"],
		[Kind.Float, "Float"],
		[Kind.Bool, "Bool"],
		[Kind.Null, "Null"],
		[Kind.DateTimeOffset, "DateTimeOffset"],
		[Kind.DateTimeLocal, "DateTimeLocal"],
		[Kind.DateLocal, "DateLocal"],
		[Kind.TimeLocal, "TimeLocal"],
	];
	for (const [k, want] of cases) {
		assert.equal(kindString(k), want);
	}
});

test("scalar node", () => {
	const sp = newSpan(newPosition(1, 1, 0), newPosition(1, 6, 5));
	const n = newScalar(Kind.Integer, "1_000", sp);
	assert.equal(n.kind, Kind.Integer);
	assert.equal(n.lexeme, "1_000");
	assert.deepEqual(n.span, sp);
	assert.deepEqual(n.entries, []);
	assert.deepEqual(n.items, []);
});

test("newScalar rejects container kinds", () => {
	for (const k of [Kind.Record, Kind.Array]) {
		assert.throws(() => newScalar(k, "", newSpan()));
	}
});

test("record node", () => {
	const child = newScalar(Kind.String, '"v"', newSpan());
	const entries: Entry[] = [{ key: "k", value: child, keySpan: newSpan() }];
	const n = newRecord(entries, newSpan());
	assert.equal(n.kind, Kind.Record);
	assert.equal(n.lexeme, "");
	assert.equal(n.entries.length, 1);
	assert.equal(n.entries[0]?.key, "k");
	assert.deepEqual(n.items, []);
});

test("array node", () => {
	const items = [
		newScalar(Kind.Integer, "1", newSpan()),
		newScalar(Kind.Integer, "2", newSpan()),
	];
	const n = newArray(items, newSpan());
	assert.equal(n.kind, Kind.Array);
	assert.equal(n.items.length, 2);
	assert.deepEqual(n.entries, []);
});

test("document bytes copy independence", () => {
	const src = new TextEncoder().encode("a = 1\n");
	const d = newDocument(FORMAT_TOML, newRecord([], newSpan()), src);
	src[0] = "X".charCodeAt(0);
	assert.equal(new TextDecoder().decode(d.bytes()), "a = 1\n");
	const b = d.bytes();
	b[0] = "Z".charCodeAt(0);
	assert.equal(new TextDecoder().decode(d.bytes()), "a = 1\n");
	assert.ok(d instanceof Document);
});

test("parse error message", () => {
	const e = new ParseError(FORMAT_TOML, newPosition(3, 5, 20), "boom");
	assert.equal(e.message, "toml parse error at line 3, column 5: boom");
});
