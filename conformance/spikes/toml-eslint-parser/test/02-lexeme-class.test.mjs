import test from "node:test";
import assert from "node:assert/strict";
import { parse, collectValues, lexeme } from "../lib/lossless.mjs";
import { TORTURE } from "./fixture.mjs";

/**
 * Test 2 -- Lexeme-class distinction. Prove the AST distinguishes integer
 * lexemes from float lexemes (1 vs 1.0) via node.kind, and recovers value
 * spellings (1_000 vs 1000, 0xFF) via the node.number raw-spelling field.
 */

function nodeFor(path) {
	const program = parse(TORTURE);
	const v = collectValues(program).find((x) => x.path.join(".") === path);
	assert.ok(v, `value node present at ${path}`);
	return v.node;
}

test("integer vs float lexemes are distinguished by node.kind", () => {
	// f1 = 1.0 is a float lexeme; dec = 1_000 is an integer lexeme.
	assert.equal(nodeFor("f1").kind, "float");
	assert.equal(nodeFor("dec").kind, "integer");
	// A bare 1 (inline.x) is integer; 2.5 (inline.y) is float.
	assert.equal(nodeFor("inline.x").kind, "integer");
	assert.equal(nodeFor("inline.y").kind, "float");
	// inf / nan are float-kind lexemes, not integers.
	assert.equal(nodeFor("inf_pos").kind, "float");
	assert.equal(nodeFor("not_a_num").kind, "float");
});

test("integer nodes expose numeric value plus lossless bigint", () => {
	const dec = nodeFor("dec");
	assert.equal(dec.bigint, 1000n, "bigint value decoded");

	const hex = nodeFor("hex");
	assert.equal(hex.bigint, 0xdeadbeefn, "hex value decoded");

	const big = nodeFor("big");
	// Beyond Number.MAX_SAFE_INTEGER: bigint keeps it exact.
	assert.equal(big.bigint, 9223372036854775807n);
});

test("FINDING: node.number strips digit-group underscores -- NOT byte-lossless", () => {
	// The convenience `.number` field normalizes underscores away (both decimal
	// and hex), so it MUST NOT be used to recover the original spelling. The
	// source RANGE (lexeme) is the lossless recovery path and keeps every byte.
	const program = parse(TORTURE);
	const values = collectValues(program);
	const dec = values.find((v) => v.path.join(".") === "dec").node;
	const hex = values.find((v) => v.path.join(".") === "hex").node;

	// .number is normalized (underscores removed):
	assert.equal(dec.number, "1000", ".number drops underscores");
	assert.equal(hex.number, "0xDEADbeef", ".number drops underscores in hex too");

	// The range recovers the EXACT original spelling -- this is what strictspec
	// must rely on for byte-exact preservation:
	assert.equal(lexeme(TORTURE, dec), "1_000", "range recovers raw spelling");
	assert.equal(lexeme(TORTURE, hex), "0xDEAD_beef", "range recovers raw hex spelling");
});

test("float nodes carry kind float and raw spelling distinct from integers", () => {
	const f3 = nodeFor("f3");
	assert.equal(f3.kind, "float");
	assert.equal(f3.number, "1e5");
	assert.equal(f3.value, 100000);

	const negzero = nodeFor("negzero");
	assert.equal(negzero.kind, "float");
	assert.equal(negzero.number, "-0.0");
	assert.ok(Object.is(negzero.value, -0), "negative zero preserved as -0");
});

test("1 vs 1.0 do not collapse: same numeric value, different lexeme class", () => {
	// Construct a minimal doc where the numeric values are equal but spellings differ.
	const program = parse("a = 1\nb = 1.0\n");
	const vals = collectValues(program);
	const a = vals.find((v) => v.path.join(".") === "a").node;
	const b = vals.find((v) => v.path.join(".") === "b").node;
	assert.equal(a.kind, "integer");
	assert.equal(b.kind, "float");
	assert.equal(a.value, 1);
	assert.equal(b.value, 1);
	assert.notEqual(a.kind, b.kind, "integer and float are separable classes");
});
