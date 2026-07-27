// The JSON/JSONL backend of the strictspec document model.
//
// A faithful, hand-written, lossless port of go/internal/jsondoc (and python
// _jsondoc). It parses JSON (and, via the JSONL functions, JSONL) bytes and folds
// the result into the format-neutral doc model: every value node maps to a
// tagged, lexeme-retaining Node, object keys resolve into an ordered record while
// spans point at the source, and the original bytes round-trip byte-identically
// via Document.bytes().
//
// JSON.parse is deliberately NOT used: it is lossy (it normalizes numbers,
// decodes strings, and last-wins on duplicate keys). This backend retains the
// EXACT source lexeme of every scalar, computes byte-accurate spans (columns and
// offsets count BYTES, matching the doc model), and raises a hard error on
// duplicate object keys. Entry points take raw text (encoded to UTF-8 bytes) or
// raw bytes; JSON.parse never touches the input.

import {
	type Document,
	type Entry,
	FORMAT_JSON,
	FORMAT_JSONL,
	Kind,
	type Node,
	newArray,
	newDocument,
	newRecord,
	newScalar,
	ParseError,
	type Position,
	type Span,
} from "./doc.js";

// MAX_DEPTH bounds nesting to keep parsing safe on adversarial input. This is a
// stack-safety guard, NOT a document-size limit. Go/Python use 10000; on V8 a
// recursive-descent parser exhausts the native stack far below that, so the cap
// is lowered here and a native RangeError is surfaced as the SAME clean
// ParseError (see guardRecursion). No conformance fixture nests remotely this
// deep, and the IR's own MAX_VALIDATION_DEPTH (128) fires first for validation.
export const MAX_DEPTH = 1000;

const decoder = new TextDecoder("utf-8", { fatal: false });

function decodeBytes(data: Uint8Array, start: number, end: number): string {
	return decoder.decode(data.subarray(start, end));
}

const HEXDIGITS = "0123456789ABCDEF";

// Decode one UTF-8 rune at data[i]. Returns [codepoint, size]. On an invalid
// lead/continuation byte returns [-1, 1] (matching Go's RuneError,1).
function decodeRune(data: Uint8Array, i: number): [number, number] {
	const b0 = data[i] as number;
	if (b0 < 0x80) {
		return [b0, 1];
	}
	if (b0 >= 0xc2 && b0 <= 0xdf) {
		if (
			i + 1 < data.length &&
			(data[i + 1] as number) >= 0x80 &&
			(data[i + 1] as number) <= 0xbf
		) {
			return [((b0 & 0x1f) << 6) | ((data[i + 1] as number) & 0x3f), 2];
		}
		return [-1, 1];
	}
	if (b0 >= 0xe0 && b0 <= 0xef) {
		if (
			i + 2 < data.length &&
			(data[i + 1] as number) >= 0x80 &&
			(data[i + 1] as number) <= 0xbf &&
			(data[i + 2] as number) >= 0x80 &&
			(data[i + 2] as number) <= 0xbf
		) {
			const cp =
				((b0 & 0x0f) << 12) |
				(((data[i + 1] as number) & 0x3f) << 6) |
				((data[i + 2] as number) & 0x3f);
			if ((cp >= 0xd800 && cp <= 0xdfff) || cp < 0x800) {
				return [-1, 1];
			}
			return [cp, 3];
		}
		return [-1, 1];
	}
	if (b0 >= 0xf0 && b0 <= 0xf4) {
		if (
			i + 3 < data.length &&
			(data[i + 1] as number) >= 0x80 &&
			(data[i + 1] as number) <= 0xbf &&
			(data[i + 2] as number) >= 0x80 &&
			(data[i + 2] as number) <= 0xbf &&
			(data[i + 3] as number) >= 0x80 &&
			(data[i + 3] as number) <= 0xbf
		) {
			const cp =
				((b0 & 0x07) << 18) |
				(((data[i + 1] as number) & 0x3f) << 12) |
				(((data[i + 2] as number) & 0x3f) << 6) |
				((data[i + 3] as number) & 0x3f);
			if (cp < 0x10000 || cp > 0x10ffff) {
				return [-1, 1];
			}
			return [cp, 4];
		}
		return [-1, 1];
	}
	return [-1, 1];
}

class Parser {
	data: Uint8Array;
	i = 0;
	line: number;
	col: number;
	baseOffset: number;
	format: string;

	constructor(
		data: Uint8Array,
		line: number,
		col: number,
		baseOffset: number,
		format: string,
	) {
		this.data = data;
		this.line = line;
		this.col = col;
		this.baseOffset = baseOffset;
		this.format = format;
	}

	pos(): Position {
		return {
			line: this.line,
			column: this.col,
			byteOffset: this.baseOffset + this.i,
		};
	}

	atEnd(): boolean {
		return this.i >= this.data.length;
	}

	peek(): number {
		return this.data[this.i] as number;
	}

	next(): number {
		const b = this.data[this.i] as number;
		this.i += 1;
		if (b === 0x0a) {
			this.line += 1;
			this.col = 1;
		} else {
			this.col += 1;
		}
		return b;
	}

	skipWs(): void {
		while (!this.atEnd() && isJsonSpace(this.peek())) {
			this.next();
		}
	}

	errAt(pos: Position, msg: string): ParseError {
		return new ParseError(this.format, pos, msg);
	}

	parseDocument(): Node {
		this.skipWs();
		if (this.atEnd()) {
			throw this.errAt(
				{ line: 1, column: 1, byteOffset: 0 },
				"empty input: expected a JSON document, found none",
			);
		}
		const root = this.parseValue(0);
		this.skipWs();
		if (!this.atEnd()) {
			throw this.errAt(
				this.pos(),
				"unexpected trailing content after the JSON document",
			);
		}
		return root;
	}

	parseValue(depth: number): Node {
		this.skipWs();
		if (this.atEnd()) {
			throw this.errAt(this.pos(), "unexpected end of input, expected a value");
		}
		const b = this.peek();
		if (b === 0x7b) {
			return this.parseObject(depth);
		}
		if (b === 0x5b) {
			return this.parseArray(depth);
		}
		if (b === 0x22) {
			const [span, lexeme] = this.scanString();
			return newScalar(Kind.String, lexeme, span);
		}
		if (b === 0x74) {
			return this.parseLiteral("true", Kind.Bool);
		}
		if (b === 0x66) {
			return this.parseLiteral("false", Kind.Bool);
		}
		if (b === 0x6e) {
			return this.parseLiteral("null", Kind.Null);
		}
		if (b === 0x2d || (b >= 0x30 && b <= 0x39)) {
			return this.parseNumber();
		}
		if (b === 0x4e || b === 0x49) {
			throw this.errAt(this.pos(), "NaN and Infinity are not valid JSON");
		}
		throw this.errAt(this.pos(), `unexpected character ${quoteByte(b)}`);
	}

	parseObject(depth: number): Node {
		if (depth >= MAX_DEPTH) {
			throw this.errAt(this.pos(), "maximum nesting depth exceeded");
		}
		const startPos = this.pos();
		this.next(); // '{'
		const entries: Entry[] = [];
		const seen = new Set<string>();

		this.skipWs();
		if (!this.atEnd() && this.peek() === 0x7d) {
			this.next();
			return newRecord(entries, { start: startPos, end: this.pos() });
		}

		for (;;) {
			this.skipWs();
			if (this.atEnd()) {
				throw this.errAt(
					this.pos(),
					"unterminated object, expected a key or '}'",
				);
			}
			if (this.peek() !== 0x22) {
				throw this.errAt(
					this.pos(),
					`expected a string key, found ${quoteByte(this.peek())}`,
				);
			}
			const [keySpan, , decoded] = this.scanString();
			if (seen.has(decoded)) {
				throw this.errAt(
					keySpan.start,
					`duplicate key "${decoded}" in JSON object`,
				);
			}
			seen.add(decoded);

			this.skipWs();
			if (this.atEnd() || this.peek() !== 0x3a) {
				throw this.errAt(this.pos(), "expected ':' after object key");
			}
			this.next();

			const value = this.parseValue(depth + 1);
			entries.push({ key: decoded, value, keySpan });

			this.skipWs();
			if (this.atEnd()) {
				throw this.errAt(
					this.pos(),
					"unterminated object, expected ',' or '}'",
				);
			}
			const c = this.peek();
			if (c === 0x2c) {
				this.next();
			} else if (c === 0x7d) {
				this.next();
				return newRecord(entries, { start: startPos, end: this.pos() });
			} else {
				throw this.errAt(
					this.pos(),
					`expected ',' or '}' in object, found ${quoteByte(c)}`,
				);
			}
		}
	}

	parseArray(depth: number): Node {
		if (depth >= MAX_DEPTH) {
			throw this.errAt(this.pos(), "maximum nesting depth exceeded");
		}
		const startPos = this.pos();
		this.next(); // '['
		const items: Node[] = [];

		this.skipWs();
		if (!this.atEnd() && this.peek() === 0x5d) {
			this.next();
			return newArray(items, { start: startPos, end: this.pos() });
		}

		for (;;) {
			const value = this.parseValue(depth + 1);
			items.push(value);
			this.skipWs();
			if (this.atEnd()) {
				throw this.errAt(this.pos(), "unterminated array, expected ',' or ']'");
			}
			const c = this.peek();
			if (c === 0x2c) {
				this.next();
			} else if (c === 0x5d) {
				this.next();
				return newArray(items, { start: startPos, end: this.pos() });
			} else {
				throw this.errAt(
					this.pos(),
					`expected ',' or ']' in array, found ${quoteByte(c)}`,
				);
			}
		}
	}

	parseLiteral(word: string, kind: Kind): Node {
		const startPos = this.pos();
		const startI = this.i;
		for (let k = 0; k < word.length; k++) {
			if (this.atEnd() || this.peek() !== word.charCodeAt(k)) {
				throw this.errAt(startPos, `invalid literal, expected "${word}"`);
			}
			this.next();
		}
		return newScalar(kind, decodeBytes(this.data, startI, this.i), {
			start: startPos,
			end: this.pos(),
		});
	}

	parseNumber(): Node {
		const startPos = this.pos();
		const startI = this.i;
		let isFloat = false;

		if (this.peek() === 0x2d) {
			this.next();
		}
		if (this.atEnd()) {
			throw this.errAt(startPos, "invalid number, expected a digit");
		}
		const b = this.peek();
		if (b === 0x30) {
			this.next();
		} else if (b >= 0x31 && b <= 0x39) {
			this.next();
			while (!this.atEnd() && isDigit(this.peek())) {
				this.next();
			}
		} else {
			throw this.errAt(
				this.pos(),
				`invalid number, expected a digit, found ${quoteByte(b)}`,
			);
		}
		// Fraction.
		if (!this.atEnd() && this.peek() === 0x2e) {
			isFloat = true;
			this.next();
			if (this.atEnd() || !isDigit(this.peek())) {
				throw this.errAt(
					this.pos(),
					"invalid number, expected a digit after the decimal point",
				);
			}
			while (!this.atEnd() && isDigit(this.peek())) {
				this.next();
			}
		}
		// Exponent.
		if (!this.atEnd() && (this.peek() === 0x65 || this.peek() === 0x45)) {
			isFloat = true;
			this.next();
			if (!this.atEnd() && (this.peek() === 0x2b || this.peek() === 0x2d)) {
				this.next();
			}
			if (this.atEnd() || !isDigit(this.peek())) {
				throw this.errAt(
					this.pos(),
					"invalid number, expected a digit in the exponent",
				);
			}
			while (!this.atEnd() && isDigit(this.peek())) {
				this.next();
			}
		}

		const kind = isFloat ? Kind.Float : Kind.Integer;
		return newScalar(kind, decodeBytes(this.data, startI, this.i), {
			start: startPos,
			end: this.pos(),
		});
	}

	scanString(): [Span, string, string] {
		const startPos = this.pos();
		const startI = this.i;
		this.next(); // opening '"'
		const dec: string[] = [];

		for (;;) {
			if (this.atEnd()) {
				throw this.errAt(this.pos(), "unterminated string");
			}
			const b = this.peek();
			if (b === 0x22) {
				this.next();
				const span: Span = { start: startPos, end: this.pos() };
				return [span, decodeBytes(this.data, startI, this.i), dec.join("")];
			}
			if (b === 0x5c) {
				this.scanEscape(dec);
			} else if (b < 0x20) {
				throw this.errAt(
					this.pos(),
					`raw control character U+${hex4(b)} in string; it must be escaped`,
				);
			} else if (b < 0x80) {
				this.next();
				dec.push(String.fromCharCode(b));
			} else {
				const [r, size] = decodeRune(this.data, this.i);
				if (r < 0 && size === 1) {
					throw this.errAt(this.pos(), "invalid UTF-8 byte in string");
				}
				for (let k = 0; k < size; k++) {
					this.next();
				}
				dec.push(String.fromCodePoint(r));
			}
		}
	}

	scanEscape(dec: string[]): void {
		const escPos = this.pos();
		this.next(); // '\'
		if (this.atEnd()) {
			throw this.errAt(escPos, "unterminated escape sequence");
		}
		const e = this.next();
		const simple: Record<number, string> = {
			34: '"',
			92: "\\",
			47: "/",
			98: "\b",
			102: "\f",
			110: "\n",
			114: "\r",
			116: "\t",
		};
		if (e in simple) {
			dec.push(simple[e] as string);
			return;
		}
		if (e === 0x75) {
			const r1 = this.readHex4();
			if (r1 === null) {
				throw this.errAt(
					escPos,
					"invalid \\u escape: expected four hexadecimal digits",
				);
			}
			if (r1 >= 0xd800 && r1 <= 0xdbff) {
				// High surrogate: try to pair with a following \uXXXX low surrogate.
				if (
					this.i + 1 < this.data.length &&
					this.data[this.i] === 0x5c &&
					this.data[this.i + 1] === 0x75
				) {
					const save: [number, number, number] = [this.i, this.line, this.col];
					this.next(); // '\'
					this.next(); // 'u'
					const r2 = this.readHex4();
					if (r2 !== null && r2 >= 0xdc00 && r2 <= 0xdfff) {
						dec.push(
							String.fromCodePoint(
								0x10000 + ((r1 - 0xd800) << 10) + (r2 - 0xdc00),
							),
						);
						return;
					}
					[this.i, this.line, this.col] = save;
				}
				dec.push("�");
			} else if (r1 >= 0xdc00 && r1 <= 0xdfff) {
				dec.push("�");
			} else {
				dec.push(String.fromCodePoint(r1));
			}
			return;
		}
		throw this.errAt(
			escPos,
			`invalid escape sequence \\${String.fromCharCode(e)}`,
		);
	}

	readHex4(): number | null {
		if (this.i + 4 > this.data.length) {
			return null;
		}
		let r = 0;
		for (let k = 0; k < 4; k++) {
			const v = hexVal(this.data[this.i + k] as number);
			if (v === null) {
				return null;
			}
			r = (r << 4) | v;
		}
		for (let k = 0; k < 4; k++) {
			this.next();
		}
		return r;
	}
}

function isJsonSpace(b: number): boolean {
	return b === 0x20 || b === 0x09 || b === 0x0a || b === 0x0d;
}

function isDigit(b: number): boolean {
	return b >= 0x30 && b <= 0x39;
}

function hexVal(b: number): number | null {
	if (b >= 0x30 && b <= 0x39) {
		return b - 0x30;
	}
	if (b >= 0x61 && b <= 0x66) {
		return b - 0x61 + 10;
	}
	if (b >= 0x41 && b <= 0x46) {
		return b - 0x41 + 10;
	}
	return null;
}

function quoteByte(b: number): string {
	if (b >= 0x20 && b < 0x7f) {
		return `'${String.fromCharCode(b)}'`;
	}
	return `U+${hex4(b)}`;
}

function hex4(r: number): string {
	const buf = ["0", "0", "0", "0"];
	let n = r;
	for (let k = 3; k >= 0; k--) {
		buf[k] = HEXDIGITS[n & 0xf] as string;
		n >>= 4;
	}
	return buf.join("");
}

function guardRecursion<T>(p: Parser, fn: () => T): T {
	try {
		return fn();
	} catch (e) {
		if (e instanceof RangeError) {
			throw p.errAt(p.pos(), "maximum nesting depth exceeded");
		}
		throw e;
	}
}

// Parse a single JSON document into a Document. Throws ParseError on any lexical
// or structural error.
export function parse(src: Uint8Array): Document {
	const data = src.slice();
	const p = new Parser(data, 1, 1, 0, FORMAT_JSON);
	const root = guardRecursion(p, () => p.parseDocument());
	return newDocument(FORMAT_JSON, root, data);
}

// Parse JSONL bytes into one Document per line. Throws ParseError (with a global
// position) at the first framing or parse error; empty input yields [].
export function parseLines(src: Uint8Array): Document[] {
	const docs: Document[] = [];
	parseStreamBytes(src, (d) => docs.push(d));
	return docs;
}

// Parse JSONL from an in-memory byte string, invoking emit once per successfully
// parsed line-document in order. Positions are GLOBAL.
export function parseStreamBytes(
	srcIn: Uint8Array,
	emit: (d: Document) => void,
): void {
	const src = srcIn.slice();
	let lineNo = 0;
	let offset = 0;
	const n = src.length;
	while (offset < n) {
		let nl = -1;
		for (let k = offset; k < n; k++) {
			if (src[k] === 0x0a) {
				nl = k;
				break;
			}
		}
		let chunk: Uint8Array;
		let hasLf: boolean;
		if (nl === -1) {
			chunk = src.subarray(offset);
			hasLf = false;
		} else {
			chunk = src.subarray(offset, nl + 1);
			hasLf = true;
		}
		if (chunk.length === 0) {
			return;
		}
		lineNo += 1;
		const lineStart = offset;
		offset += chunk.length;
		const line = hasLf ? chunk.subarray(0, chunk.length - 1) : chunk;

		frameLine(line, lineNo, lineStart);
		const p = new Parser(line.slice(), lineNo, 1, lineStart, FORMAT_JSONL);
		const root = guardRecursion(p, () => p.parseDocument());
		emit(newDocument(FORMAT_JSONL, root, line.slice()));
	}
}

function frameLine(line: Uint8Array, lineNo: number, lineStart: number): void {
	const n = line.length;
	if (n > 0 && line[n - 1] === 0x0d) {
		throw new ParseError(
			FORMAT_JSONL,
			{ line: lineNo, column: n, byteOffset: lineStart + n - 1 },
			"line ends with a carriage return; JSONL is LF-only",
		);
	}
	if (isBlankLine(line)) {
		throw new ParseError(
			FORMAT_JSONL,
			{ line: lineNo, column: 1, byteOffset: lineStart },
			"blank line is not a valid JSONL document",
		);
	}
}

function isBlankLine(line: Uint8Array): boolean {
	for (const b of line) {
		if (b !== 0x20 && b !== 0x09) {
			return false;
		}
	}
	return true;
}
