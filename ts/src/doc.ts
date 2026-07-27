// The format-neutral, tagged, lexeme-retaining document model.
//
// A faithful TypeScript port of go/internal/doc (and python/src/strictspec/_doc.py).
// It is the shared contract every backend (TOML via toml-eslint-parser,
// JSON/JSONL via a hand-written ordered decoder) populates: one model, three
// syntaxes. Values carry their lexeme class (Kind) and their exact source bytes
// (lexeme), which is what makes both verdict identity (read side) and byte-stable
// writes possible.
//
// This module is dependency-light and knows nothing about diagnostics, schemas,
// or validation. Parse failures surface as the typed ParseError below (with a
// source position); conversion to toolchain diagnostics happens at the validator
// layer.

// The lexeme class of a document-model node. FORMAT-NEUTRAL and LEXICAL: it
// records how the value was written (integer vs float lexeme, which datetime
// flavor), never a schema-level interpretation. The NUMBER scalar is a
// schema-level concept layered over Integer and Float; there is no Number kind.
export enum Kind {
	Record = 0,
	Array = 1,
	String = 2,
	Integer = 3,
	Float = 4,
	Bool = 5,
	Null = 6,
	DateTimeOffset = 7,
	DateTimeLocal = 8,
	DateLocal = 9,
	TimeLocal = 10,
}

const KIND_NAMES: Record<Kind, string> = {
	[Kind.Record]: "Record",
	[Kind.Array]: "Array",
	[Kind.String]: "String",
	[Kind.Integer]: "Integer",
	[Kind.Float]: "Float",
	[Kind.Bool]: "Bool",
	[Kind.Null]: "Null",
	[Kind.DateTimeOffset]: "DateTimeOffset",
	[Kind.DateTimeLocal]: "DateTimeLocal",
	[Kind.DateLocal]: "DateLocal",
	[Kind.TimeLocal]: "TimeLocal",
};

export function kindString(k: Kind): string {
	return KIND_NAMES[k] ?? `Kind(${k})`;
}

export function isScalarKind(k: Kind): boolean {
	return k !== Kind.Record && k !== Kind.Array;
}

// A single location in a source document. Line and column are 1-based (column
// counts bytes, matching the TOML substrate); byteOffset is a 0-based offset into
// the source bytes. The zero Position (line === 0) is the "no source position"
// sentinel.
export interface Position {
	readonly line: number;
	readonly column: number;
	readonly byteOffset: number;
}

export function newPosition(line = 0, column = 0, byteOffset = 0): Position {
	return { line, column, byteOffset };
}

export function positionIsValid(p: Position): boolean {
	return p.line >= 1;
}

// The half-open source range [start, end) covered by a node. For any scalar node
// parsed from source, the bytes in [start.byteOffset, end.byteOffset) are exactly
// the node's lexeme.
export interface Span {
	readonly start: Position;
	readonly end: Position;
}

export function newSpan(
	start: Position = newPosition(),
	end: Position = newPosition(),
): Span {
	return { start, end };
}

export function spanIsValid(s: Span): boolean {
	return positionIsValid(s.start);
}

// One ordered key/value binding inside a Record. key is the DECODED (unquoted,
// code-point) key string; keySpan points at the key's source location; value is
// the bound node.
export interface Entry {
	readonly key: string;
	readonly value: Node;
	readonly keySpan: Span;
}

export function newEntry(
	key: string,
	value: Node,
	keySpan: Span = newSpan(),
): Entry {
	return { key, value, keySpan };
}

// A tagged, immutable document-model value. One model, three syntaxes: a
// backend's parser is the only thing that constructs Nodes, via
// newScalar/newRecord/newArray. Read-only by convention; there is no mutation API.
export class Node {
	readonly kind: Kind;
	readonly lexeme: string;
	readonly span: Span;
	readonly entries: readonly Entry[];
	readonly items: readonly Node[];

	constructor(
		kind: Kind,
		lexeme: string,
		span: Span,
		entries: readonly Entry[],
		items: readonly Node[],
	) {
		this.kind = kind;
		this.lexeme = lexeme;
		this.span = span;
		this.entries = entries;
		this.items = items;
	}
}

// Build a scalar Node. Throws on a container kind (a backend bug).
export function newScalar(kind: Kind, lexeme: string, span: Span): Node {
	if (!isScalarKind(kind)) {
		throw new Error(`newScalar: ${kindString(kind)} is not a scalar kind`);
	}
	return new Node(kind, lexeme, span, [], []);
}

export function newRecord(entries: readonly Entry[], span: Span): Node {
	return new Node(Kind.Record, "", span, entries, []);
}

export function newArray(items: readonly Node[], span: Span): Node {
	return new Node(Kind.Array, "", span, [], items);
}

export const FORMAT_TOML = "toml";
export const FORMAT_JSON = "json";
export const FORMAT_JSONL = "jsonl";

// One parsed document: its source format, its root Node, and its exact source
// bytes.
export class Document {
	readonly format: string;
	readonly root: Node;
	private readonly source: Uint8Array;

	constructor(format: string, root: Node, source: Uint8Array) {
		this.format = format;
		this.root = root;
		this.source = source.slice(); // private, immutable snapshot
	}

	// The exact source bytes the document was parsed from (a fresh copy).
	bytes(): Uint8Array {
		return this.source.slice();
	}
}

export function newDocument(
	format: string,
	root: Node,
	source: Uint8Array,
): Document {
	return new Document(format, root, source);
}

// A typed parse failure with a source position. The ONLY error a backend's parse
// raises. Deliberately does not reference the toolchain's diagnostic types.
export class ParseError extends Error {
	readonly format: string;
	readonly position: Position;
	readonly detail: string;

	constructor(format: string, position: Position, message: string) {
		super(
			`${format} parse error at line ${position.line}, column ${position.column}: ${message}`,
		);
		this.name = "ParseError";
		this.format = format;
		this.position = position;
		this.detail = message;
	}
}
