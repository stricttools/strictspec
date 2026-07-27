// Diagnostics/path tests (port of go/internal/diag/path_test.go and python test_diag.py).

import assert from "node:assert/strict";
import { test } from "node:test";
import {
	appendKey,
	Diagnostics,
	escapeString,
	isIdentShaped,
	newDiagnostic,
	newPath,
	stepArm,
	stepIndex,
	stepKey,
} from "../dist/diag.js";

test("path render", () => {
	const cases: Array<[string, string]> = [
		[newPath().render(), "$"],
		[newPath(stepKey("a")).render(), "$.a"],
		[newPath(stepKey("a"), stepKey("b")).render(), "$.a.b"],
		[newPath(stepKey("items"), stepIndex(0)).render(), "$.items[0]"],
		[newPath(stepKey("items"), stepIndex(42)).render(), "$.items[42]"],
		[newPath(stepKey("a-b"), stepKey("c_d")).render(), "$.a-b.c_d"],
		[
			newPath(stepKey("config"), stepKey("weird key")).render(),
			'$.config["weird key"]',
		],
		[newPath(stepKey("m"), stepKey("1x")).render(), '$.m["1x"]'],
		[newPath(stepKey("m"), stepKey("")).render(), '$.m[""]'],
		[newPath(stepKey('a"b')).render(), '$["a\\"b"]'],
		[newPath(stepKey("a\\b")).render(), '$["a\\\\b"]'],
		[newPath(stepKey("a\nb")).render(), '$["a\\nb"]'],
		[newPath(stepKey("a\tb")).render(), '$["a\\tb"]'],
		[newPath(stepKey("a\x01b")).render(), '$["a\\u0001b"]'],
		[
			newPath(
				stepKey("shape"),
				stepArm("gradient"),
				stepKey("stops"),
				stepIndex(0),
			).render(),
			"$.shape(gradient).stops[0]",
		],
		[newPath().withAnchor(42, 0).render(), "$@L42:0"],
		[newPath(stepKey("budget")).withAnchor(42, 17).render(), "$.budget@L42:17"],
		[
			newPath(stepKey("rows"), stepIndex(3)).withAnchor(3, 12).render(),
			"$.rows[3]@L3:12",
		],
	];
	for (const [got, want] of cases) {
		assert.equal(got, want);
	}
});

test("is ident shaped", () => {
	for (const s of [
		"a",
		"abc",
		"_x",
		"a1",
		"a-b",
		"c_d",
		"A",
		"Content-Type",
		"x-y-z",
	]) {
		assert.ok(isIdentShaped(s), s);
	}
	for (const s of [
		"",
		"1x",
		"-a",
		"a b",
		"a.b",
		"a/b",
		'a"b',
		"trailing ",
		"é",
	]) {
		assert.ok(!isIdentShaped(s), s);
	}
});

test("escape string", () => {
	const cases: Array<[string, string]> = [
		["", ""],
		["plain", "plain"],
		['a"b', 'a\\"b'],
		["a\\b", "a\\\\b"],
		["a\nb", "a\\nb"],
		["a\rb", "a\\rb"],
		["a\tb", "a\\tb"],
		["a\x00b", "a\\u0000b"],
		["a\x1fb", "a\\u001fb"],
		["unicodé", "unicodé"],
		["emoji \u{1F600}", "emoji \u{1F600}"],
	];
	for (const [s, want] of cases) {
		assert.equal(escapeString(s), want);
	}
});

test("diagnostics accumulation order", () => {
	const d = new Diagnostics();
	assert.equal(d.length, 0);
	d.emitCode("STRICTSPEC_A", newPath(stepKey("x")), null);
	d.emit(newDiagnostic("STRICTSPEC_B", newPath(stepKey("y")), {}));
	d.emitCode("STRICTSPEC_C", newPath(stepKey("z")), null);
	assert.deepEqual(
		d.all().map((x) => x.code),
		["STRICTSPEC_A", "STRICTSPEC_B", "STRICTSPEC_C"],
	);
});

test("append step immutability", () => {
	const p = newPath(stepKey("a"));
	const p2 = appendKey(p, "b");
	assert.equal(p.render(), "$.a");
	assert.equal(p2.render(), "$.a.b");
});
