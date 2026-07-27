// Renderer tests (port of go/internal/render/render_test.go and python
// test_render.py). The golden expected strings are copied verbatim from the Go
// goldens (spec-derived oracle), not regenerated.

import assert from "node:assert/strict";
import { test } from "node:test";
import {
	arrayVal,
	boolVal,
	type Diagnostic,
	datetimeVal,
	dateVal,
	floatVal,
	intVal,
	newDiagnostic,
	newPath,
	nullVal,
	numberVal,
	recordVal,
	slotIdentifier,
	slotInt,
	slotList,
	slotPath,
	slotString,
	slotSuggestion,
	slotValue,
	slotVersion,
	stepKey,
	stringVal,
	timeVal,
	type Value,
} from "../dist/diag.js";
import { RenderError, render, renderValue } from "../dist/render.js";

const GOLDEN: Array<[Diagnostic, string]> = [
	[
		newDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", newPath(stepKey("count")), {
			got: slotString("float"),
		}),
		"Expected an integer at $.count, got float.",
	],
	[
		newDiagnostic("STRICTSPEC_TYPE_MISMATCH", newPath(stepKey("canvas")), {
			expected: slotString("record"),
			got: slotString("array"),
		}),
		"Expected record at $.canvas, got array.",
	],
	[
		newDiagnostic("STRICTSPEC_KEY_UNKNOWN", newPath(stepKey("config")), {
			key: slotString("colour"),
		}),
		"Unknown key colour at $.config.",
	],
	[
		newDiagnostic("STRICTSPEC_KEY_UNKNOWN", newPath(stepKey("config")), {
			key: slotString("colr"),
			suggestion: slotSuggestion("colr", ["color", "width", "height"]),
		}),
		"Unknown key colr at $.config. Did you mean color?",
	],
	[
		newDiagnostic("STRICTSPEC_VALUE_NUM_TOO_SMALL", newPath(stepKey("age")), {
			actual: slotValue(intVal(3)),
			limit: slotValue(intVal(18)),
		}),
		"Value 3 at $.age is below the minimum 18.",
	],
	[
		newDiagnostic("STRICTSPEC_VALUE_STRING_TOO_LONG", newPath(stepKey("bio")), {
			actual: slotInt(200),
			limit: slotInt(64),
		}),
		"String at $.bio has 200 code points; maximum is 64.",
	],
	[
		newDiagnostic("STRICTSPEC_VALUE_STRING_REGEX", newPath(stepKey("slug")), {
			actual: slotValue(stringVal("Hello World")),
			pattern: slotValue(stringVal("^[a-z-]+$")),
		}),
		'String "Hello World" at $.slug does not match the required pattern "^[a-z-]+$".',
	],
	[
		newDiagnostic(
			"STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
			newPath(stepKey("color")),
			{
				got: slotValue(stringVal("gren")),
				expected: slotList([
					stringVal("red"),
					stringVal("green"),
					stringVal("blue"),
					stringVal("cyan"),
				]),
				suggestion: slotSuggestion("gren", ["red", "green", "blue", "cyan"]),
			},
		),
		'Value "gren" at $.color is not one of ["red", "green", "blue", ...]. Did you mean green or red?',
	],
	[
		newDiagnostic("STRICTSPEC_GATE_UNSUPPORTED", newPath(), {
			got: slotVersion(2),
			schema: slotIdentifier("canvas"),
			expected: slotVersion(3),
			migset: slotIdentifier("canvas_v2_v3"),
			invocation: slotString(
				"strictspec migrate --schema canvas --to 3 doc.json",
			),
		}),
		"Document `format_version` is 2, but schema canvas accepts exactly 3 (migration set canvas_v2_v3). Run: strictspec migrate --schema canvas --to 3 doc.json",
	],
	[
		newDiagnostic(
			"STRICTSPEC_INTRA_FORBIDDEN_WHEN",
			newPath(stepKey("legacy")),
			{
				key: slotString("legacy"),
				condition: slotString('mode == "strict"'),
			},
		),
		'Field legacy at $.legacy is forbidden when mode == "strict".',
	],
	[
		newDiagnostic(
			"STRICTSPEC_INTRA_EXACTLY_ONE_OF",
			newPath(stepKey("payment")),
			{
				fields: slotList([stringVal("card"), stringVal("bank")]),
				actual: slotList([stringVal("card"), stringVal("bank")]),
			},
		),
		'Exactly one of ["card", "bank"] must be present at $.payment; found ["card", "bank"].',
	],
	[
		newDiagnostic("STRICTSPEC_NUM_SAFE_INTEGER", newPath(stepKey("id")), {
			actual: slotValue(intVal(9007199254740993n)),
		}),
		"Integer 9007199254740993 at $.id exceeds the safe-integer range (|n| >= 2^53) required by `safe_integers`.",
	],
	[
		newDiagnostic(
			"STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN",
			newPath(stepKey("shape")),
			{
				got: slotValue(stringVal("circl")),
				expected: slotList([stringVal("circle"), stringVal("square")]),
				suggestion: slotSuggestion("circl", ["circle", "square"]),
			},
		),
		'Discriminator "circl" at $.shape is not one of ["circle", "square"]. Did you mean circle?',
	],
	[
		newDiagnostic(
			"STRICTSPEC_PARSE_JSONL_LINE_SYNTAX",
			newPath().withAnchor(3, 12),
			{
				line: slotInt(3),
				detail: slotString("unexpected end of input"),
			},
		),
		"JSONL parse error on line 3 at $@L3:12: unexpected end of input.",
	],
	[
		newDiagnostic(
			"STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON",
			newPath(stepKey("tags")),
			{ actual: slotInt(3) },
		),
		"unwrap_singleton at $.tags requires a single-element array; found 3 elements.",
	],
	[
		newDiagnostic("STRICTSPEC_ALIAS_BOTH_PRESENT", newPath(stepKey("node")), {
			alias: slotIdentifier("colour"),
			canonical: slotIdentifier("color"),
		}),
		"Both colour and canonical color are present at $.node; provide exactly one.",
	],
	[
		newDiagnostic("STRICTSPEC_TYPE_NOT_LITERAL", newPath(stepKey("version")), {
			expected: slotValue(intVal(1)),
			got: slotValue(intVal(2)),
		}),
		"Expected the literal 1 at $.version, got 2.",
	],
	[
		newDiagnostic("STRICTSPEC_INTRA_UNIQUE_BY", newPath(stepKey("users")), {
			value: slotValue(stringVal("alice")),
			field: slotString("username"),
			normalization: slotString("case-fold"),
		}),
		'Duplicate value "alice" for unique-by username at $.users (normalization: case-fold).',
	],
];

test("render golden", () => {
	for (const [d, want] of GOLDEN) {
		assert.equal(render(d), want, d.code);
	}
});

test("render value", () => {
	const negZero = -0;
	const cases: Array<[Value, string]> = [
		[intVal(1000), "1000"],
		[intVal(-42), "-42"],
		[intVal(0), "0"],
		[floatVal({ lexeme: "1e3", hasLexeme: true }), "1e3"],
		[floatVal({ lexeme: "5.0", hasLexeme: true }), "5.0"],
		[floatVal({ f: 5.0 }), "5.0"],
		[floatVal({ f: negZero }), "-0.0"],
		[numberVal("007", true), "007"],
		[numberVal("1.50", false), "1.50"],
		[stringVal("line\nbreak"), '"line\\nbreak"'],
		[boolVal(true), "true"],
		[boolVal(false), "false"],
		[nullVal(), "null"],
		[dateVal("2026-07-27"), "2026-07-27"],
		[timeVal("13:37:00"), "13:37:00"],
		[datetimeVal("2026-07-27T13:37:00+00:00"), "2026-07-27T13:37:00+00:00"],
		[arrayVal([]), "[]"],
		[arrayVal([intVal(1), intVal(2), intVal(3)]), "[1, 2, 3]"],
		[arrayVal([intVal(1), intVal(2), intVal(3), intVal(4)]), "[1, 2, 3, ...]"],
		[recordVal([], []), "{}"],
		[
			recordVal(["a", "weird key"], [intVal(1), boolVal(true)]),
			'{a: 1, "weird key": true}',
		],
		[
			recordVal(
				["a", "b", "c", "d"],
				[intVal(1), intVal(2), intVal(3), intVal(4)],
			),
			"{a: 1, b: 2, c: 3, ...}",
		],
		[arrayVal([arrayVal([arrayVal([intVal(1)])])]), "[[[...]]]"],
	];
	for (const [v, want] of cases) {
		assert.equal(renderValue(v), want);
	}
});

test("string truncation boundary (via renderValue)", () => {
	const s63 = "a".repeat(63);
	assert.equal(renderValue(stringVal(s63)), `"${s63}"`);
	const s64 = "a".repeat(64);
	assert.equal(renderValue(stringVal(s64)), `"${s64}"`);
	const s65 = "a".repeat(65);
	assert.equal(renderValue(stringVal(s65)), `"${"a".repeat(64)}..."`);
	const emoji = "\u{1F600}".repeat(65);
	assert.equal(renderValue(stringVal(emoji)), `"${"\u{1F600}".repeat(64)}..."`);
	const nl = "\n".repeat(65);
	assert.equal(renderValue(stringVal(nl)), `"${"\\n".repeat(64)}..."`);
});

test("render throws on programmer errors", () => {
	assert.throws(
		() => render(newDiagnostic("STRICTSPEC_NOT_A_REAL_CODE", newPath(), {})),
		RenderError,
	);
	assert.throws(
		() =>
			render(
				newDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", newPath(stepKey("x")), {}),
			),
		RenderError,
	);
	assert.throws(
		() =>
			render(
				newDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", newPath(stepKey("x")), {
					got: slotString("float"),
					bogus: slotString("x"),
				}),
			),
		RenderError,
	);
	assert.throws(
		() =>
			render(
				newDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", newPath(stepKey("x")), {
					got: slotString("float"),
					path: slotPath(newPath()),
				}),
			),
		RenderError,
	);
});
