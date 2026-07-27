import test from "node:test";
import assert from "node:assert/strict";
import { parseTOML, ParseError } from "toml-eslint-parser";
import { parse } from "../lib/lossless.mjs";

/**
 * Test 5 -- Error / edge behavior. Establish how the parser rejects invalid
 * TOML: it throws a typed ParseError carrying byte offset, 1-based-ish line, and
 * column, which strictspec can surface as a structured diagnostic.
 */

test("invalid TOML throws a typed ParseError with position info", () => {
	let err;
	try {
		parse("a = = 1\n");
	} catch (e) {
		err = e;
	}
	assert.ok(err instanceof ParseError, "throws ParseError");
	assert.equal(typeof err.index, "number", "byte offset present");
	assert.equal(typeof err.lineNumber, "number", "line number present");
	assert.equal(typeof err.column, "number", "column present");
	assert.ok(err.message.length > 0, "has a message");
});

test("position points at the offending region", () => {
	let err;
	try {
		parse("valid = 1\nbroken = \n");
	} catch (e) {
		err = e;
	}
	assert.ok(err instanceof ParseError);
	// Error is on the second line (the empty value), not the first.
	assert.ok(err.lineNumber >= 2, `line ${err.lineNumber} should be >= 2`);
});

test("unterminated string is rejected with position", () => {
	let err;
	try {
		parse('s = "no closing quote\n');
	} catch (e) {
		err = e;
	}
	assert.ok(err instanceof ParseError, "unterminated string rejected");
	assert.equal(typeof err.index, "number");
});

test("version gating: a 1.1-only construct is accepted at 1.1 but rejected at 1.0", () => {
	// Trailing comma inside an inline table is a TOML 1.1-only construct.
	const doc = "t = { a = 1, }\n";
	// 1.1 accepts it.
	assert.doesNotThrow(() => parseTOML(doc, { tomlVersion: "1.1" }));
	// 1.0 rejects it with a positioned ParseError -- the acceptance-gate lever
	// strictcli relies on, available to strictspec too.
	let err;
	try {
		parseTOML(doc, { tomlVersion: "1.0" });
	} catch (e) {
		err = e;
	}
	assert.ok(err instanceof ParseError, "1.0 rejects the 1.1-only construct");
});

test("valid document does not throw", () => {
	assert.doesNotThrow(() => parse("a = 1\nb = 'x'\n"));
});
