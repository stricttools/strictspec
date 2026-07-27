// The TOML backend of the strictspec document model, built on toml-eslint-parser.
//
// Parses TOML text with toml-eslint-parser (the sanctioned TS-runtime TOML
// dependency) and folds the result into the format-neutral doc model, using the
// AST-ranges + text-splicing technique proven by the phase-0 spike
// (conformance/spikes/toml-eslint-parser): every TOML value node maps to a
// tagged, lexeme-retaining Node whose exact lexeme is the raw source between the
// node's range, dotted keys / [table] / [[array-table]] headers resolve into the
// record tree per TOML semantics (via the parser's resolvedKey), and the original
// bytes round-trip byte-identically via Document.bytes().
//
// The spike surfaced the key caveat: node.number / node.value are NORMALIZED
// (underscores stripped, datetimes reparsed), so this backend NEVER reads them --
// it recovers the exact lexeme from the source range only. That is the
// byte-lossless path.
//
// SPANS (byte offsets): the parser's ranges are UTF-16 code-unit offsets into the
// text; this backend converts them to byte offsets so source[span] === lexeme
// holds even for non-ASCII sources. TOML node spans are never consumed by any
// diagnostic (the emitter attaches source positions only for JSONL @L anchors,
// and TOML is never JSONL; TOML parse-error positions come from the parser's
// exception, not from node spans).

import { parseTOML } from "toml-eslint-parser";
import {
	type Document,
	type Entry,
	FORMAT_TOML,
	Kind,
	type Node,
	newArray,
	newDocument,
	newRecord,
	newScalar,
	ParseError,
	type Position,
	type Span,
	spanIsValid,
} from "./doc.js";

// --- minimal structural typing over the toml-eslint-parser AST ---------------

type Range = readonly [number, number];

interface KeyEl {
	readonly type: string;
	readonly name?: string;
	readonly value?: unknown;
}
interface TKey {
	readonly keys: readonly KeyEl[];
	readonly range: Range;
}
interface TValue {
	readonly type: "TOMLValue";
	readonly kind: string;
	readonly range: Range;
}
interface TArray {
	readonly type: "TOMLArray";
	readonly elements: readonly TValueNode[];
	readonly range: Range;
}
interface TInline {
	readonly type: "TOMLInlineTable";
	readonly body: readonly TKeyValue[];
	readonly range: Range;
}
type TValueNode = TValue | TArray | TInline;
interface TKeyValue {
	readonly type: "TOMLKeyValue";
	readonly key: TKey;
	readonly value: TValueNode;
}
interface TTable {
	readonly type: "TOMLTable";
	readonly kind: "standard" | "array";
	readonly resolvedKey: readonly (string | number)[];
	readonly body: readonly TKeyValue[];
}
type TopChild = TKeyValue | TTable;
interface TProgram {
	readonly body: readonly { readonly body: readonly TopChild[] }[];
}

interface TomlParseErrorLike {
	message: string;
	lineNumber?: number;
	column?: number;
	index?: number;
}

// Parse TOML source into a Document. Throws ParseError with a source position on
// any syntax or TOML-semantic error (including duplicate keys, which the parser
// rejects).
export function parse(src: Uint8Array | string): Document {
	const text =
		typeof src === "string" ? src : new TextDecoder("utf-8").decode(src);
	const bytes =
		typeof src === "string" ? new TextEncoder().encode(src) : src.slice();
	let program: TProgram;
	try {
		program = parseTOML(text, { tomlVersion: "1.0" }) as unknown as TProgram;
	} catch (e) {
		throw toParseError(e as TomlParseErrorLike);
	}
	const conv = new Converter(text);
	const root = conv.buildRoot(program);
	return newDocument(FORMAT_TOML, root, bytes);
}

function toParseError(e: TomlParseErrorLike): ParseError {
	const line = e.lineNumber ?? 0;
	const col0 = e.column ?? 0; // toml-eslint-parser column is 0-based
	const off = e.index ?? 0;
	return new ParseError(
		FORMAT_TOML,
		{ line, column: col0 + 1, byteOffset: off },
		e.message,
	);
}

function keySegments(key: TKey): string[] {
	return key.keys.map((k) =>
		k.type === "TOMLBare" ? (k.name as string) : String(k.value),
	);
}

// --- intermediate record builder ---------------------------------------------

class BSlot {
	keySpan: Span;
	value: Node | null = null;
	sub: Builder | null = null;
	arr: Builder[] = [];
	constructor(keySpan: Span) {
		this.keySpan = keySpan;
	}
}

class Builder {
	readonly keys: string[] = [];
	readonly byKey = new Map<string, BSlot>();

	note(key: string, keySpan: Span): BSlot {
		let s = this.byKey.get(key);
		if (s !== undefined) {
			return s;
		}
		s = new BSlot(keySpan);
		this.byKey.set(key, s);
		this.keys.push(key);
		return s;
	}

	finalize(span: Span): Node {
		const entries: Entry[] = [];
		for (const k of this.keys) {
			const s = this.byKey.get(k) as BSlot;
			let v: Node;
			if (s.value !== null) {
				v = s.value;
			} else if (s.arr.length > 0) {
				const items = s.arr.map((b) => b.finalize(emptySpan()));
				v = newArray(items, cover(items));
			} else if (s.sub !== null) {
				v = s.sub.finalize(emptySpan());
			} else {
				v = newRecord([], s.keySpan);
			}
			entries.push({ key: k, value: v, keySpan: s.keySpan });
		}
		let sp = span;
		if (!spanIsValid(sp)) {
			sp = cover(entries.map((e) => e.value));
		}
		return newRecord(entries, sp);
	}
}

function emptySpan(): Span {
	return {
		start: { line: 0, column: 0, byteOffset: 0 },
		end: { line: 0, column: 0, byteOffset: 0 },
	};
}

function cover(nodes: readonly Node[]): Span {
	const spans = nodes.map((n) => n.span).filter((s) => spanIsValid(s));
	if (spans.length === 0) {
		return emptySpan();
	}
	return {
		start: (spans[0] as Span).start,
		end: (spans[spans.length - 1] as Span).end,
	};
}

class Converter {
	private readonly text: string;
	private readonly byteAt: number[]; // UTF-16 index -> byte offset
	private readonly lineStarts: number[]; // byte offset of each line start

	constructor(text: string) {
		this.text = text;
		this.byteAt = new Array<number>(text.length + 1);
		let b = 0;
		for (let i = 0; i < text.length; ) {
			const cur = b;
			this.byteAt[i] = cur;
			const cp = text.codePointAt(i) as number;
			const units = cp > 0xffff ? 2 : 1;
			b += utf8Len(cp);
			if (units === 2) {
				this.byteAt[i + 1] = cur; // surrogate-pair filler (never queried)
			}
			i += units;
		}
		this.byteAt[text.length] = b;
		// Line starts as byte offsets, scanning the text for '\n'.
		this.lineStarts = [0];
		let bo = 0;
		for (let i = 0; i < text.length; ) {
			const cp = text.codePointAt(i) as number;
			const units = cp > 0xffff ? 2 : 1;
			const len = utf8Len(cp);
			if (cp === 0x0a) {
				this.lineStarts.push(bo + len);
			}
			bo += len;
			i += units;
		}
	}

	private byteOffset(charIndex: number): number {
		return this.byteAt[charIndex] ?? 0;
	}

	private pos(charIndex: number): Position {
		const off = this.byteOffset(charIndex);
		let li = 0;
		// binary search over lineStarts
		let lo = 0;
		let hi = this.lineStarts.length - 1;
		while (lo <= hi) {
			const mid = (lo + hi) >> 1;
			if ((this.lineStarts[mid] as number) <= off) {
				li = mid;
				lo = mid + 1;
			} else {
				hi = mid - 1;
			}
		}
		return {
			line: li + 1,
			column: off - (this.lineStarts[li] as number) + 1,
			byteOffset: off,
		};
	}

	private span(range: Range): Span {
		return { start: this.pos(range[0]), end: this.pos(range[1]) };
	}

	private lexeme(range: Range): string {
		return this.text.slice(range[0], range[1]);
	}

	buildRoot(program: TProgram): Node {
		const top = program.body[0];
		const rootSpan: Span = {
			start: { line: 1, column: 1, byteOffset: 0 },
			end: this.pos(this.text.length),
		};
		const b = new Builder();
		if (top === undefined) {
			return b.finalize(rootSpan);
		}
		for (const child of top.body) {
			if (child.type === "TOMLKeyValue") {
				this.insertKV(
					b,
					keySegments(child.key),
					this.span(child.key.range),
					child.value,
				);
			} else {
				// TOMLTable: standard or array-of-tables. resolvedKey fully resolves the
				// path including numeric array indices for [[table]] entries.
				const target = this.descendPath(b, child.resolvedKey);
				for (const kv of child.body) {
					this.insertKV(
						target,
						keySegments(kv.key),
						this.span(kv.key.range),
						kv.value,
					);
				}
			}
		}
		return b.finalize(rootSpan);
	}

	// Navigate to (creating as needed) the builder addressed by a container path.
	// String segments descend into sub-records; a numeric segment selects an
	// array-of-tables entry under the preceding key.
	private descendPath(
		bStart: Builder,
		segs: readonly (string | number)[],
	): Builder {
		let b = bStart;
		let i = 0;
		while (i < segs.length) {
			const seg = segs[i] as string | number;
			const next = i + 1 < segs.length ? segs[i + 1] : undefined;
			if (typeof next === "number") {
				const slot = b.note(String(seg), emptySpan());
				const idx = next;
				while (slot.arr.length <= idx) {
					slot.arr.push(new Builder());
				}
				b = slot.arr[idx] as Builder;
				i += 2;
			} else {
				const slot = b.note(String(seg), emptySpan());
				if (slot.sub === null) {
					slot.sub = new Builder();
				}
				b = slot.sub;
				i += 1;
			}
		}
		return b;
	}

	// Insert one key = value binding, expanding a dotted key into intermediate
	// implicit records.
	private insertKV(
		bStart: Builder,
		segs: readonly string[],
		keySpan: Span,
		value: TValueNode,
	): void {
		if (segs.length === 0) {
			return;
		}
		let b = bStart;
		for (let i = 0; i < segs.length - 1; i++) {
			const slot = b.note(segs[i] as string, keySpan);
			if (slot.sub === null) {
				slot.sub = new Builder();
			}
			b = slot.sub;
		}
		const last = segs[segs.length - 1] as string;
		const slot = b.note(last, keySpan);
		slot.value = this.convertValue(value);
	}

	private convertValue(node: TValueNode): Node {
		if (node.type === "TOMLArray") {
			const children = node.elements.map((el) => this.convertValue(el));
			return newArray(children, this.span(node.range));
		}
		if (node.type === "TOMLInlineTable") {
			const ib = new Builder();
			for (const kv of node.body) {
				this.insertKV(
					ib,
					keySegments(kv.key),
					this.span(kv.key.range),
					kv.value,
				);
			}
			return ib.finalize(this.span(node.range));
		}
		// TOMLValue scalar.
		return newScalar(
			scalarKind(node.kind),
			this.lexeme(node.range),
			this.span(node.range),
		);
	}
}

function scalarKind(kind: string): Kind {
	switch (kind) {
		case "string":
			return Kind.String;
		case "integer":
			return Kind.Integer;
		case "float":
			return Kind.Float;
		case "boolean":
			return Kind.Bool;
		case "offset-date-time":
			return Kind.DateTimeOffset;
		case "local-date-time":
			return Kind.DateTimeLocal;
		case "local-date":
			return Kind.DateLocal;
		case "local-time":
			return Kind.TimeLocal;
		default:
			return Kind.String;
	}
}

function utf8Len(cp: number): number {
	if (cp < 0x80) {
		return 1;
	}
	if (cp < 0x800) {
		return 2;
	}
	if (cp < 0x10000) {
		return 3;
	}
	return 4;
}
