import test from "node:test";
import assert from "node:assert/strict";
import { parse, collectValues, lexeme, locateKeyValue } from "../lib/lossless.mjs";
import { TORTURE } from "./fixture.mjs";

/**
 * Test 1 -- AST completeness + spans. Parse the torture document and prove that
 * every scalar value node carries a precise source range recovering the EXACT
 * original lexeme text, across every construct class.
 */

test("every scalar value's range recovers its exact original lexeme", () => {
	const program = parse(TORTURE);
	const values = collectValues(program);
	// Map resolved dotted path -> recovered lexeme.
	const byPath = new Map(values.map((v) => [v.path.join("."), lexeme(TORTURE, v.node)]));

	const expected = {
		// Strings, all four styles (lexeme includes the quotes / delimiters).
		title: '"basic \\"quoted\\" string"',
		literal: "'C:\\path\\no\\escape'",
		multiline: '"""\nline one\nline two"""',
		multiline_literal: "'''raw\nlines'''",
		// Integers: decimal-with-underscores, negative, hex, octal, binary, big.
		dec: "1_000",
		neg: "-17",
		hex: "0xDEAD_beef",
		oct: "0o755",
		bin: "0b1010",
		big: "9_223_372_036_854_775_807",
		// Floats: 1.0, fractional, exponent, negative zero, exp-negative, inf, nan.
		f1: "1.0",
		f2: "3.14",
		f3: "1e5",
		negzero: "-0.0",
		planck: "6.626e-34",
		inf_pos: "inf",
		inf_neg: "-inf",
		not_a_num: "nan",
		// Booleans.
		yes: "true",
		no: "false",
		// All four datetime forms.
		odt: "1979-05-27T07:32:00Z",
		ldt: "1979-05-27T07:32:00",
		ld: "1979-05-27",
		lt: "07:32:00",
		// Dotted keys.
		"fruit.name": '"apple"',
		"fruit.color": '"red"',
		// Inline table members.
		"inline.x": "1",
		"inline.y": "2.5",
		"inline.label": '"pt"',
		// Inline + multiline arrays (element-by-element).
		"arr.[0]": "1",
		"arr.[1]": "2",
		"arr.[2]": "3",
		"nested_arr.[0]": '"a"',
		"nested_arr.[1]": '"b"',
		// Standard table + nested table.
		"table_a.key": '"value"',
		"table_a.count": "42",
		"table_a.sub.deep": "true",
		// Array-of-tables: first entry resolves under products.0.
		"products.0.name": '"hammer"',
		"products.0.sku": "738594937",
		"products.1.name": '"nail"',
		"products.1.sku": "284758393",
	};

	for (const [path, lex] of Object.entries(expected)) {
		assert.equal(byPath.get(path), lex, `lexeme mismatch at ${path}`);
	}
	// Completeness: no expected path missing, and count matches (nothing dropped).
	assert.equal(byPath.size, Object.keys(expected).length, "unexpected extra/missing value nodes");
});

test("key nodes also carry ranges recovering literal key spellings", () => {
	const program = parse(TORTURE);
	const found = locateKeyValue(program, ["fruit", "name"]);
	assert.ok(found, "dotted key located");
	assert.equal(lexeme(TORTURE, found.kv.key), "fruit.name");
});

test("comments are present as tokens with ranges (nothing silently dropped)", () => {
	const program = parse(TORTURE);
	assert.ok(Array.isArray(program.comments), "program.comments present");
	const commentTexts = program.comments.map((c) => lexeme(TORTURE, c));
	assert.ok(
		commentTexts.some((t) => t.includes("Top-level document comment")),
		"line comment captured",
	);
	assert.ok(
		commentTexts.some((t) => t.includes("inline comment on a key")),
		"inline comment captured",
	);
});
