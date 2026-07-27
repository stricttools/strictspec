// TOML backend tests (port of python test_tomldoc.py / go tomldoc torture):
// all four string styles, integer bases, float forms, all datetime kinds, dotted
// keys, inline tables, arrays, standard/nested tables, and array-of-tables, with
// byte-identical round-trip and span/lexeme exactness.

import assert from "node:assert/strict";
import { test } from "node:test";
import { Kind, type Node, ParseError } from "../dist/doc.js";
import { parse } from "../dist/tomldoc.js";

const TORTURE = `# Top-level document comment
# second comment line

title = "basic \\"quoted\\" string"   # inline comment on a key
literal = 'C:\\path\\no\\escape'
multiline = """
line one
line two"""
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
`;

const enc = (s: string): Uint8Array => new TextEncoder().encode(s);
const dec = (b: Uint8Array): string => new TextDecoder().decode(b);

function field(n: Node, key: string): Node {
	for (const e of n.entries) {
		if (e.key === key) {
			return e.value;
		}
	}
	throw new Error(`key ${key} not found`);
}

test("roundtrip byte identity", () => {
	const src = enc(TORTURE);
	const d = parse(src);
	assert.deepEqual(d.bytes(), src);
	assert.equal(d.format, "toml");
});

test("span lexeme exactness", () => {
	const src = enc(TORTURE);
	const d = parse(src);
	let count = 0;
	const check = (n: Node): void => {
		if (n.kind !== Kind.Record && n.kind !== Kind.Array) {
			count += 1;
			const sp = n.span;
			assert.ok(sp.start.line >= 1);
			assert.equal(
				dec(src.subarray(sp.start.byteOffset, sp.end.byteOffset)),
				n.lexeme,
			);
		}
		for (const e of n.entries) {
			check(e.value);
		}
		for (const it of n.items) {
			check(it);
		}
	};
	check(d.root);
	assert.ok(count > 0);
});

test("scalar kinds and lexemes", () => {
	const d = parse(enc(TORTURE));
	const cases: Array<[string, Kind, string]> = [
		["title", Kind.String, '"basic \\"quoted\\" string"'],
		["literal", Kind.String, "'C:\\path\\no\\escape'"],
		["dec", Kind.Integer, "1_000"],
		["hex", Kind.Integer, "0xDEAD_beef"],
		["oct", Kind.Integer, "0o755"],
		["bin", Kind.Integer, "0b1010"],
		["f1", Kind.Float, "1.0"],
		["negzero", Kind.Float, "-0.0"],
		["inf_pos", Kind.Float, "inf"],
		["inf_neg", Kind.Float, "-inf"],
		["not_a_num", Kind.Float, "nan"],
		["yes", Kind.Bool, "true"],
		["no", Kind.Bool, "false"],
		["odt", Kind.DateTimeOffset, "1979-05-27T07:32:00Z"],
		["ldt", Kind.DateTimeLocal, "1979-05-27T07:32:00"],
		["ld", Kind.DateLocal, "1979-05-27"],
		["lt", Kind.TimeLocal, "07:32:00"],
	];
	for (const [key, kind, lexeme] of cases) {
		const n = field(d.root, key);
		assert.equal(n.kind, kind, key);
		assert.equal(n.lexeme, lexeme, key);
	}
});

test("dotted keys merge", () => {
	const d = parse(enc(TORTURE));
	const fruit = field(d.root, "fruit");
	assert.equal(fruit.kind, Kind.Record);
	assert.deepEqual(
		fruit.entries.map((e) => e.key),
		["name", "color"],
	);
});

test("inline table and arrays", () => {
	const d = parse(enc(TORTURE));
	const inline = field(d.root, "inline");
	assert.equal(inline.kind, Kind.Record);
	assert.deepEqual(
		inline.entries.map((e) => e.key),
		["x", "y", "label"],
	);
	const arr = field(d.root, "arr");
	assert.equal(arr.kind, Kind.Array);
	assert.equal(arr.items.length, 3);
	const na = field(d.root, "nested_arr");
	assert.equal(na.kind, Kind.Array);
	assert.deepEqual(
		na.items.map((it) => it.lexeme),
		['"a"', '"b"'],
	);
});

test("nested tables and array of tables", () => {
	const d = parse(enc(TORTURE));
	const ta = field(d.root, "table_a");
	assert.equal(ta.kind, Kind.Record);
	assert.equal(field(ta, "key").lexeme, '"value"');
	const sub = field(ta, "sub");
	assert.equal(field(sub, "deep").lexeme, "true");
	const products = field(d.root, "products");
	assert.equal(products.kind, Kind.Array);
	assert.equal(products.items.length, 2);
	assert.equal(field(products.items[0] as Node, "name").lexeme, '"hammer"');
	assert.equal(field(products.items[1] as Node, "sku").lexeme, "284758393");
});

test("parse error position", () => {
	let e: ParseError | null = null;
	try {
		parse(enc("a = = 1\n"));
	} catch (err) {
		e = err as ParseError;
	}
	assert.ok(e !== null);
	assert.equal(e.format, "toml");
	assert.equal(e.position.line, 1);
});

test("duplicate key is rejected", () => {
	assert.throws(() => parse(enc("a = 1\na = 2\n")), ParseError);
});
