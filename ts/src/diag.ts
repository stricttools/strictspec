// The strictspec diagnostic model: the path grammar, the document-value and slot
// tagged unions, and the Diagnostic / Diagnostics types.
//
// A faithful port of go/internal/diag (and python _diag). It is the shared
// vocabulary the emitter IR populates and the render module consumes. It contains
// no message templates and no rendering of message text (that is render.ts) --
// only the structured data a diagnostic carries and the path/value primitives
// whose rendering is fixed by appendix-rendering.md.

// --- string primitives (A.2 escaping, identifier-shaped rule) ----------------

// Whether s is identifier-shaped: [A-Za-z_][A-Za-z0-9_-]* . Identifier-shaped
// keys render bare (`.name`); others switch to the quoted map-key form
// (`["name"]`). Candidates render bare when ident-shaped.
export function isIdentShaped(s: string): boolean {
	if (s === "") {
		return false;
	}
	for (let i = 0; i < s.length; i++) {
		const ch = s[i] as string;
		if (i === 0) {
			if (!(ch === "_" || isAsciiLetter(ch))) {
				return false;
			}
			continue;
		}
		if (
			!(
				ch === "_" ||
				ch === "-" ||
				isAsciiLetter(ch) ||
				(ch >= "0" && ch <= "9")
			)
		) {
			return false;
		}
	}
	return true;
}

function isAsciiLetter(ch: string): boolean {
	return (ch >= "A" && ch <= "Z") || (ch >= "a" && ch <= "z");
}

// Apply the A.2 string-escaping table. Does NOT add surrounding quotes and never
// truncates. Exactly the A.2 escapes are produced; all other code points --
// including non-ASCII -- are emitted verbatim.
export function escapeString(s: string): string {
	let out = "";
	for (const ch of s) {
		if (ch === '"') {
			out += '\\"';
		} else if (ch === "\\") {
			out += "\\\\";
		} else if (ch === "\n") {
			out += "\\n";
		} else if (ch === "\r") {
			out += "\\r";
		} else if (ch === "\t") {
			out += "\\t";
		} else {
			const o = ch.codePointAt(0) as number;
			if (o <= 0x1f) {
				out += `\\u00${o.toString(16).padStart(2, "0")}`;
			} else {
				out += ch;
			}
		}
	}
	return out;
}

// The number of Unicode code points in s (NOT UTF-16 code units).
export function codePointLength(s: string): number {
	let n = 0;
	for (const _ of s) {
		n += 1;
	}
	return n;
}

// The first n code points of s.
export function codePointSlice(s: string, n: number): string {
	let out = "";
	let count = 0;
	for (const ch of s) {
		if (count >= n) {
			break;
		}
		out += ch;
		count += 1;
	}
	return out;
}

// --- path steps --------------------------------------------------------------

export type Step =
	| { readonly s: "root" }
	| { readonly s: "key"; readonly name: string }
	| { readonly s: "index"; readonly n: number }
	| { readonly s: "mapkey"; readonly name: string }
	| { readonly s: "arm"; readonly name: string };

export function stepRoot(): Step {
	return { s: "root" };
}
export function stepKey(name: string): Step {
	return { s: "key", name };
}
export function stepIndex(n: number): Step {
	return { s: "index", n };
}
export function stepMapKey(name: string): Step {
	return { s: "mapkey", name };
}
export function stepArm(name: string): Step {
	return { s: "arm", name };
}

// Addresses a value within a JSONL stream: "@L<line>:<offset>". Line is one-based;
// offset is a zero-based byte offset within the line.
export interface JSONLAnchor {
	readonly line: number;
	readonly offset: number;
}

// A diagnostic path: an ordered sequence of steps rooted at the document root,
// optionally anchored to a JSONL stream position.
export class Path {
	readonly steps: readonly Step[];
	readonly anchor: JSONLAnchor | null;

	constructor(steps: readonly Step[] = [], anchor: JSONLAnchor | null = null) {
		this.steps = steps;
		this.anchor = anchor;
	}

	render(): string {
		let out = "";
		for (const s of this.steps) {
			switch (s.s) {
				case "root":
					out += "$";
					break;
				case "key":
					out += isIdentShaped(s.name)
						? `.${s.name}`
						: `["${escapeString(s.name)}"]`;
					break;
				case "index":
					out += `[${s.n}]`;
					break;
				case "mapkey":
					out += `["${escapeString(s.name)}"]`;
					break;
				case "arm":
					out += `(${s.name})`;
					break;
			}
		}
		if (this.anchor !== null) {
			out += `@L${this.anchor.line}:${this.anchor.offset}`;
		}
		return out;
	}

	withAnchor(line: number, offset: number): Path {
		return new Path(this.steps, { line, offset });
	}
}

// Build a Path rooted at "$" with the given steps (Root prepended).
export function newPath(...steps: Step[]): Path {
	return new Path([stepRoot(), ...steps]);
}

export function appendStep(p: Path, s: Step): Path {
	return new Path([...p.steps, s], p.anchor);
}
export function appendKey(p: Path, key: string): Path {
	return appendStep(p, stepKey(key));
}
export function appendMapKey(p: Path, key: string): Path {
	return appendStep(p, stepMapKey(key));
}
export function appendIndex(p: Path, i: number): Path {
	return appendStep(p, stepIndex(i));
}
export function appendArm(p: Path, name: string): Path {
	return appendStep(p, stepArm(name));
}

// --- document values (payloads of value-typed slots and list<T> elements) ----

export type Value =
	| { readonly v: "int"; readonly n: bigint }
	| {
			readonly v: "float";
			readonly f: number;
			readonly lexeme: string;
			readonly hasLexeme: boolean;
	  }
	| {
			readonly v: "number";
			readonly lexeme: string;
			readonly intClass: boolean;
	  }
	| { readonly v: "string"; readonly s: string }
	| { readonly v: "bool"; readonly b: boolean }
	| { readonly v: "null" }
	| { readonly v: "date"; readonly s: string }
	| { readonly v: "time"; readonly s: string }
	| { readonly v: "datetime"; readonly s: string }
	| { readonly v: "array"; readonly elems: readonly Value[] }
	| {
			readonly v: "record";
			readonly keys: readonly string[];
			readonly vals: readonly Value[];
	  };

export function intVal(n: bigint | number): Value {
	return { v: "int", n: typeof n === "bigint" ? n : BigInt(n) };
}
export function floatVal(opts: {
	f?: number;
	lexeme?: string;
	hasLexeme?: boolean;
}): Value {
	return {
		v: "float",
		f: opts.f ?? 0,
		lexeme: opts.lexeme ?? "",
		hasLexeme: opts.hasLexeme ?? false,
	};
}
export function numberVal(lexeme: string, intClass: boolean): Value {
	return { v: "number", lexeme, intClass };
}
export function stringVal(s: string): Value {
	return { v: "string", s };
}
export function boolVal(b: boolean): Value {
	return { v: "bool", b };
}
export function nullVal(): Value {
	return { v: "null" };
}
export function dateVal(s: string): Value {
	return { v: "date", s };
}
export function timeVal(s: string): Value {
	return { v: "time", s };
}
export function datetimeVal(s: string): Value {
	return { v: "datetime", s };
}
export function arrayVal(elems: readonly Value[]): Value {
	return { v: "array", elems };
}
export function recordVal(
	keys: readonly string[],
	vals: readonly Value[],
): Value {
	return { v: "record", keys, vals };
}

// --- slots (template placeholder bindings) -----------------------------------

export type Slot =
	| { readonly t: "string"; readonly s: string }
	| { readonly t: "int"; readonly n: number }
	| { readonly t: "code"; readonly code: string }
	| { readonly t: "identifier"; readonly name: string }
	| { readonly t: "version"; readonly v: number }
	| { readonly t: "path"; readonly p: Path }
	| { readonly t: "value"; readonly value: Value }
	| { readonly t: "list"; readonly elems: readonly Value[] }
	| {
			readonly t: "suggestion";
			readonly unknown: string;
			readonly candidates: readonly string[];
	  };

export function slotString(s: string): Slot {
	return { t: "string", s };
}
export function slotInt(n: number): Slot {
	return { t: "int", n };
}
export function slotCode(code: string): Slot {
	return { t: "code", code };
}
export function slotIdentifier(name: string): Slot {
	return { t: "identifier", name };
}
export function slotVersion(v: number): Slot {
	return { t: "version", v };
}
export function slotPath(p: Path): Slot {
	return { t: "path", p };
}
export function slotValue(value: Value): Slot {
	return { t: "value", value };
}
export function slotList(elems: readonly Value[]): Slot {
	return { t: "list", elems };
}
export function slotSuggestion(
	unknown: string,
	candidates: readonly string[],
): Slot {
	return { t: "suggestion", unknown, candidates };
}

// --- diagnostics -------------------------------------------------------------

// One emitted hard error: a STRICTSPEC_* code, the Path it is attached to, and
// the typed slot bindings that fill the remaining template placeholders. {path}
// is never present in slots -- it comes from path.
export interface Diagnostic {
	readonly code: string;
	readonly path: Path;
	readonly slots: Readonly<Record<string, Slot>>;
}

export function newDiagnostic(
	code: string,
	path: Path,
	slots: Record<string, Slot> = {},
): Diagnostic {
	return { code, path, slots };
}

// An ordered, one-pass accumulation of diagnostics in emission order. Renderers
// may not reorder.
export class Diagnostics {
	private readonly items: Diagnostic[] = [];

	emit(d: Diagnostic): void {
		this.items.push(d);
	}

	emitCode(code: string, path: Path, slots: Record<string, Slot> | null): void {
		this.items.push({ code, path, slots: slots ?? {} });
	}

	all(): Diagnostic[] {
		return this.items;
	}

	get length(): number {
		return this.items.length;
	}
}
