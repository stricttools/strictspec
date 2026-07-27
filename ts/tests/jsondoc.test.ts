// JSON/JSONL backend tests (port of go/internal/jsondoc/*_test.go and python
// test_jsondoc.py): torture document, number-classification table, the 17-case
// invalid-input table, and JSONL framing/global-offset behavior.

import assert from "node:assert/strict";
import { test } from "node:test";
import { Kind, type Node, ParseError } from "../dist/doc.js";
import {
	MAX_DEPTH,
	parse,
	parseLines,
	parseStreamBytes,
} from "../dist/jsondoc.js";

const enc = (s: string): Uint8Array => new TextEncoder().encode(s);
const dec = (b: Uint8Array): string => new TextDecoder().decode(b);

// assert.throws() returns void; capture the thrown error for position assertions.
function grab(fn: () => unknown): ParseError {
	try {
		fn();
	} catch (e) {
		return e as ParseError;
	}
	throw new Error("expected a throw, got none");
}

// Byte-identical copy of the Go/Python torture document.
const TORTURE =
	"{\n" +
	'  "title": "basic \\"quoted\\" string",\n' +
	'  "escapes": "tab\\tnewline\\nreturn\\rbackslash\\\\slash\\/bfk\\b\\fbmpéastral\u{1d11e}",\n' +
	'  "ünîcodé": "key written in raw UTF-8",\n' +
	'  "empty_string": "",\n' +
	'  "empty_object": {},\n' +
	'  "empty_array": [],\n' +
	'  "numbers": {\n' +
	'    "int": 42,\n' +
	'    "neg": -17,\n' +
	'    "zero": 0,\n' +
	'    "neg_zero": -0,\n' +
	'    "float": 3.14,\n' +
	'    "neg_zero_float": -0.0,\n' +
	'    "exp_lower": 1e5,\n' +
	'    "exp_upper": 1E-5,\n' +
	'    "exp_signed": 6.626e-34,\n' +
	'    "small_frac": 0.1,\n' +
	'    "big_beyond_f64": 123456789012345678901234567890,\n' +
	'    "big_float": 1.7976931348623159e308\n' +
	"  },\n" +
	'  "flags": [true, false, null],\n' +
	'  "nested": {\n' +
	'    "level1": {\n' +
	'      "level2": {\n' +
	'        "items": [1, 2, [3, 4, {"deep": "value"}]]\n' +
	"      }\n" +
	"    }\n" +
	"  },\n" +
	'  "long": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"\n' +
	"}";

function field(n: Node, key: string): Node {
	for (const e of n.entries) {
		if (e.key === key) {
			return e.value;
		}
	}
	throw new Error(`key ${key} not found`);
}

function walkScalars(n: Node, f: (n: Node) => void): void {
	if (n.kind === Kind.Record) {
		for (const e of n.entries) {
			walkScalars(e.value, f);
		}
	} else if (n.kind === Kind.Array) {
		for (const it of n.items) {
			walkScalars(it, f);
		}
	} else {
		f(n);
	}
}

test("roundtrip byte identity", () => {
	const d = parse(enc(TORTURE));
	assert.equal(dec(d.bytes()), TORTURE);
	assert.equal(d.format, "json");
});

test("span lexeme exactness", () => {
	const d = parse(enc(TORTURE));
	const src = d.bytes();
	let count = 0;
	walkScalars(d.root, (n) => {
		count += 1;
		const sp = n.span;
		assert.ok(sp.start.line >= 1);
		const s = sp.start.byteOffset;
		const e = sp.end.byteOffset;
		assert.ok(0 <= s && s <= e && e <= src.length);
		assert.equal(dec(src.subarray(s, e)), n.lexeme);
	});
	assert.ok(count > 0);
});

test("key span exactness", () => {
	const d = parse(enc(TORTURE));
	const src = d.bytes();
	let seen = 0;
	const check = (n: Node): void => {
		if (n.kind === Kind.Record) {
			for (const e of n.entries) {
				const sp = e.keySpan;
				assert.ok(sp.start.line >= 1);
				const raw = src.subarray(sp.start.byteOffset, sp.end.byteOffset);
				assert.ok(
					raw.length >= 2 && raw[0] === 0x22 && raw[raw.length - 1] === 0x22,
				);
				seen += 1;
				check(e.value);
			}
		} else if (n.kind === Kind.Array) {
			for (const it of n.items) {
				check(it);
			}
		}
	};
	check(d.root);
	assert.ok(seen > 0);
});

test("number lexeme classification", () => {
	const d = parse(enc(TORTURE));
	const nums = field(d.root, "numbers");
	const cases: Array<[string, Kind, string]> = [
		["int", Kind.Integer, "42"],
		["neg", Kind.Integer, "-17"],
		["zero", Kind.Integer, "0"],
		["neg_zero", Kind.Integer, "-0"],
		["float", Kind.Float, "3.14"],
		["neg_zero_float", Kind.Float, "-0.0"],
		["exp_lower", Kind.Float, "1e5"],
		["exp_upper", Kind.Float, "1E-5"],
		["exp_signed", Kind.Float, "6.626e-34"],
		["small_frac", Kind.Float, "0.1"],
		["big_beyond_f64", Kind.Integer, "123456789012345678901234567890"],
		["big_float", Kind.Float, "1.7976931348623159e308"],
	];
	for (const [key, kind, lexeme] of cases) {
		const n = field(nums, key);
		assert.equal(n.kind, kind, key);
		assert.equal(n.lexeme, lexeme, key);
	}
});

test("ordered entries preserved", () => {
	const d = parse(enc('{"z":1,"a":2,"m":3,"b":4}'));
	assert.deepEqual(
		d.root.entries.map((e) => e.key),
		["z", "a", "m", "b"],
	);
});

test("escape decoding", () => {
	const cases: Array<[Uint8Array, string]> = [
		[enc('{"a\\tb":1}'), "a\tb"],
		[enc('{"quote\\"end":1}'), 'quote"end'],
		[enc('{"bmp\\u00e9":1}'), "bmpé"],
		[enc('{"clef\\ud834\\udd1e":1}'), "clef\u{1d11e}"],
		[enc('{"slash\\/end":1}'), "slash/end"],
		[enc('{"nl\\ncr\\r":1}'), "nl\ncr\r"],
	];
	for (const [src, decoded] of cases) {
		const d = parse(src);
		assert.equal(d.root.entries[0]?.key, decoded);
		assert.deepEqual(d.bytes(), src);
	}
});

test("bare scalar and array documents", () => {
	const cases: Array<[string, Kind]> = [
		["42", Kind.Integer],
		["3.14", Kind.Float],
		['"hi"', Kind.String],
		["true", Kind.Bool],
		["null", Kind.Null],
		["[1,2,3]", Kind.Array],
		['  "padded"  ', Kind.String],
	];
	for (const [src, kind] of cases) {
		const d = parse(enc(src));
		assert.equal(d.root.kind, kind);
		assert.equal(dec(d.bytes()), src);
	}
});

test("duplicate key error", () => {
	const e = grab(() => parse(enc('{"a":1,"a":2}')));
	assert.equal(e.position.byteOffset, 7);
	assert.equal(e.position.line, 1);
	assert.equal(e.position.column, 8);
	assert.throws(() => parse(enc('{"ab":1,"ab":2}')), ParseError);
});

test("parse errors with positions (17-case table)", () => {
	const cases: Array<[string, Uint8Array, number, number, number]> = [
		["unterminated object", enc('{"a":1'), 1, 7, 6],
		["unterminated array", enc("[1,2"), 1, 5, 4],
		["unterminated string", enc('"nope'), 1, 6, 5],
		["missing colon", enc('{"a" 1}'), 1, 6, 5],
		["missing value", enc('{"a":}'), 1, 6, 5],
		["trailing content", enc("123 456"), 1, 5, 4],
		["invalid literal", enc("nul"), 1, 1, 0],
		["trailing comma array", enc("[1,]"), 1, 4, 3],
		["trailing comma object", enc('{"a":1,}'), 1, 8, 7],
		["bare NaN", enc("NaN"), 1, 1, 0],
		["bare Infinity", enc("Infinity"), 1, 1, 0],
		["bad escape", enc('"\\x"'), 1, 2, 1],
		["fraction no digit", enc("1."), 1, 3, 2],
		["exponent no digit", enc("1e"), 1, 3, 2],
		["control char in string", enc('"line\nbreak"'), 1, 6, 5],
		["empty input", enc(""), 1, 1, 0],
		["whitespace only", enc("   \n  "), 1, 1, 0],
	];
	for (const [name, src, line, col, offset] of cases) {
		const e = grab(() => parse(src));
		assert.equal(e.format, "json", name);
		assert.deepEqual(
			[e.position.line, e.position.column, e.position.byteOffset],
			[line, col, offset],
			name,
		);
		assert.ok(e.position.line >= 1);
	}
});

test("invalid utf8 position", () => {
	const e = grab(() => parse(Uint8Array.from([0x22, 0xff, 0x22])));
	assert.deepEqual(
		[e.position.byteOffset, e.position.line, e.position.column],
		[1, 1, 2],
	);
});

test("deep nesting bounded", () => {
	const n = MAX_DEPTH + 50;
	const src = enc("[".repeat(n) + "]".repeat(n));
	assert.throws(() => parse(src), ParseError);
	const ok = enc("[".repeat(100) + "1" + "]".repeat(100));
	parse(ok); // must not throw
});

// --- JSONL -----------------------------------------------------------------

test("jsonl multiline valid", () => {
	const src = enc('{"a":1}\n[1,2,3]\n"bare"\n42\ntrue\n');
	const docs = parseLines(src);
	assert.equal(docs.length, 5);
	const kinds = [Kind.Record, Kind.Array, Kind.String, Kind.Integer, Kind.Bool];
	const wbytes = ['{"a":1}', "[1,2,3]", '"bare"', "42", "true"];
	docs.forEach((d, i) => {
		assert.equal(d.format, "jsonl");
		assert.equal(d.root.kind, kinds[i]);
		assert.equal(dec(d.bytes()), wbytes[i]);
	});
});

test("jsonl global positions", () => {
	const src = enc('{"a":1}\n{"b":2}\n{"c":333}\n');
	const docs = parseLines(src);
	assert.equal(docs.length, 3);
	const c = field((docs[2] as { root: Node }).root, "c");
	assert.equal(c.lexeme, "333");
	assert.equal(c.span.start.line, 3);
	assert.equal(c.span.start.byteOffset, 21);
	assert.equal(
		dec(src.subarray(c.span.start.byteOffset, c.span.end.byteOffset)),
		"333",
	);
});

test("jsonl final line without LF", () => {
	assert.equal(parseLines(enc('{"a":1}\n{"b":2}\n')).length, 2);
	assert.equal(parseLines(enc('{"a":1}\n{"b":2}')).length, 2);
	const single = parseLines(enc("42"));
	assert.equal(single.length, 1);
	assert.equal(single[0]?.root.lexeme, "42");
});

test("jsonl empty input", () => {
	assert.deepEqual(parseLines(enc("")), []);
});

test("jsonl blank line error", () => {
	const cases: Array<[string, Uint8Array, number, number]> = [
		["empty middle line", enc("{}\n\n{}\n"), 2, 3],
		["whitespace middle line", enc("{}\n   \n{}\n"), 2, 3],
		["leading blank line", enc("\n{}\n"), 1, 0],
	];
	for (const [name, src, line, offset] of cases) {
		const e = grab(() => parseLines(src));
		assert.equal(e.format, "jsonl", name);
		assert.deepEqual(
			[e.position.line, e.position.byteOffset],
			[line, offset],
			name,
		);
		assert.ok(e.detail.includes("lank line"));
	}
});

test("jsonl trailing CR error", () => {
	const e = grab(() => parseLines(enc("{}\r\n{}\n")));
	assert.deepEqual(
		[e.position.line, e.position.column, e.position.byteOffset],
		[1, 3, 2],
	);
	assert.ok(e.detail.includes("carriage return"));
});

test("jsonl duplicate key per line", () => {
	const e = grab(() => parseLines(enc('{"a":1}\n{"b":2,"b":3}\n')));
	assert.equal(e.position.line, 2);
	assert.ok(e.detail.includes("duplicate"));
});

function flatten(n: Node): string {
	const out: string[] = [];
	const walk = (n: Node): void => {
		if (n.kind === Kind.Record) {
			out.push("{");
			for (const e of n.entries) {
				out.push(`${e.key}=`);
				walk(e.value);
				out.push(";");
			}
			out.push("}");
		} else if (n.kind === Kind.Array) {
			out.push("[");
			for (const it of n.items) {
				walk(it);
				out.push(";");
			}
			out.push("]");
		} else {
			out.push(
				`${n.kind}:${n.lexeme}@${n.span.start.byteOffset}-${n.span.end.byteOffset}`,
			);
		}
	};
	walk(n);
	return out.join("");
}

test("stream slice parity", () => {
	const src = enc('{"a":1}\n[1,2,3]\n"x"\n{"nested":{"k":true}}\n42\n');
	const sliceDocs = parseLines(src);
	const streamed: Array<{ root: Node; bytes(): Uint8Array }> = [];
	parseStreamBytes(src, (d) => streamed.push(d));
	assert.equal(sliceDocs.length, streamed.length);
	for (let i = 0; i < sliceDocs.length; i++) {
		assert.equal(
			flatten((sliceDocs[i] as { root: Node }).root),
			flatten((streamed[i] as { root: Node }).root),
		);
		assert.deepEqual(
			(sliceDocs[i] as { bytes(): Uint8Array }).bytes(),
			(streamed[i] as { bytes(): Uint8Array }).bytes(),
		);
	}
});
