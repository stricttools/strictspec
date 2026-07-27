// The strictspec shared emitter IR and its executor.
//
// A faithful port of go/internal/ir (program + execute + walk + values + scalars
// + constraints + condition + datetime) and python _ir. It is the single
// intermediate representation from which every target's validator is driven: the
// same executor that backs the reference interpreter backs the generated
// validators, which is the mechanism behind the four-target verdict + code + path
// + message identity.
//
// Ordering is a property of the IR, not of any target: the executor fixes the
// traversal and emission order once (gate first and terminal; document-order
// present keys with anchored missing-required interleaving; phase-2 constraints
// over records whose phase 1 passed), so every target accumulates diagnostics in
// the identical order.

import type { Diagnostic, Path, Slot, Value } from "./diag.js";
import * as diag from "./diag.js";
import * as doc from "./doc.js";
import { Kind, type Node } from "./doc.js";
import * as schema from "./schema.js";
import { decodeJSON, decodeTOML } from "./strdecode.js";

// The pinned recursion-depth cap (fired before stack exhaustion).
export const MAX_VALIDATION_DEPTH = 128;

export class Program {
	schema: schema.Schema;
	scalars: Map<string, schema.Scalar>;
	constructor(s: schema.Schema, scalars: Map<string, schema.Scalar>) {
		this.schema = s;
		this.scalars = scalars;
	}
	schemaName(): string {
		return this.schema.name;
	}
	formatVersion(): number {
		return this.schema.formatVersion;
	}
}

export function compileProgram(
	s: schema.Schema,
	scalars: Map<string, schema.Scalar> | null,
): Program {
	return new Program(s, scalars ?? new Map());
}

export interface EvidenceDoc {
	readonly [key: string]: unknown;
}
export type Evidence = Record<string, readonly EvidenceDoc[]>;

export interface ExecOptions {
	format?: string;
	evidence?: Evidence | null;
	structuralOnly?: boolean;
	jsonl?: boolean;
	line?: number;
	lineStart?: number;
}

interface P2Task {
	typ: schema.Type;
	rec: Node;
	path: Path;
}

const REGEX_CACHE = new Map<string, RegExp>();

function compileRegex(pattern: string): RegExp {
	const cached = REGEX_CACHE.get(pattern);
	if (cached !== undefined) {
		return cached;
	}
	let re: RegExp;
	try {
		re = new RegExp(pattern);
	} catch {
		re = /^$/; // matches only the empty string (fails closed)
	}
	REGEX_CACHE.set(pattern, re);
	return re;
}

type PresentAction =
	| {
			kind: "present";
			name: string;
			typ: schema.Type;
			key: string;
			declIdx: number;
			docPos: number;
			before: string[];
	  }
	| {
			kind: "alias";
			name: string;
			alias: string;
			declIdx: number;
			docPos: number;
			before: string[];
	  };

class Exec {
	p: Program;
	s: schema.Schema;
	scalars: Map<string, schema.Scalar>;
	root: Node | null;
	format: string;
	evidence: Evidence;
	jsonl: boolean;
	line: number;
	lineStart: number;
	diags = new diag.Diagnostics();
	phase2: P2Task[] = [];
	clean = new Map<Node, boolean>();
	depth = 0;

	constructor(p: Program, root: Node | null, opts: ExecOptions) {
		this.p = p;
		this.s = p.schema;
		this.scalars = p.scalars;
		this.root = root;
		this.format = opts.format ?? doc.FORMAT_JSON;
		this.evidence = opts.evidence ?? {};
		this.jsonl = opts.jsonl ?? false;
		this.line = opts.line ?? 0;
		this.lineStart = opts.lineStart ?? 0;
	}

	// --- gate ----------------------------------------------------------------

	gate(root: Node | null): boolean {
		const invocation = `strictspec migrate --schema ${this.s.name} --to ${this.s.formatVersion} <paths>`;
		const base: Record<string, Slot> = {
			schema: diag.slotIdentifier(this.s.name),
			expected: diag.slotVersion(this.s.formatVersion),
			invocation: diag.slotString(invocation),
		};
		const fv = entryOf(root, "format_version");
		if (fv === null) {
			this.emit("STRICTSPEC_GATE_ABSENT", diag.newPath(), root, base);
			return false;
		}
		if (fv.kind !== Kind.Integer) {
			const slots: Record<string, Slot> = {
				...base,
				got: diag.slotValue(this.valueOf(fv)),
			};
			this.emit("STRICTSPEC_GATE_WRONG_TYPE", diag.newPath(), root, slots);
			return false;
		}
		const got = svalIntLexeme(fv.lexeme);
		if (got !== BigInt(this.s.formatVersion)) {
			const slots: Record<string, Slot> = {
				...base,
				got: diag.slotVersion(Number(got)),
				migset: diag.slotIdentifier(this.s.name),
			};
			this.emit("STRICTSPEC_GATE_UNSUPPORTED", diag.newPath(), root, slots);
			return false;
		}
		return true;
	}

	// --- emit ----------------------------------------------------------------

	emit(
		code: string,
		path: Path,
		anchorNode: Node | null,
		slots: Record<string, Slot>,
	): void {
		let p = path;
		if (this.jsonl) {
			let off = 0;
			if (anchorNode !== null) {
				const sp = anchorNode.span;
				if (doc.positionIsValid(sp.start)) {
					off = sp.start.byteOffset - this.lineStart;
					if (off < 0) {
						off = 0;
					}
				}
			}
			p = path.withAnchor(this.line, off);
		}
		this.diags.emitCode(code, p, slots);
	}

	// --- walk (depth-guard + type dispatch) ----------------------------------

	walk(t: schema.Type, n: Node | null, path: Path): boolean {
		this.depth += 1;
		try {
			const before = this.diags.length;
			if (this.depth > MAX_VALIDATION_DEPTH) {
				this.emit("STRICTSPEC_DEPTH_EXCEEDED", path, n, {
					limit: diag.slotInt(MAX_VALIDATION_DEPTH),
				});
				return false;
			}
			this.walkInner(t, n, path);
			const clean = this.diags.length === before;
			if (n !== null) {
				this.clean.set(n, clean);
			}
			return clean;
		} finally {
			this.depth -= 1;
		}
	}

	walkInner(t: schema.Type, n: Node | null, path: Path): void {
		switch (t.kind) {
			case schema.SKind.Ref: {
				const named = this.s.types.get(t.ref);
				if (named !== undefined) {
					this.walkInner(named, n, path);
					return;
				}
				this.walkScalar(t, n, path);
				return;
			}
			case schema.SKind.Record:
				this.walkRecord(t, n, path);
				return;
			case schema.SKind.Map:
				this.walkMap(t, n, path);
				return;
			case schema.SKind.Array:
				this.walkArray(t, n, path);
				return;
			case schema.SKind.Tuple:
				this.walkTuple(t, n, path);
				return;
			case schema.SKind.Enum:
				this.walkEnum(t, n, path);
				return;
			case schema.SKind.Literal:
				this.walkLiteral(t, n, path);
				return;
			case schema.SKind.DiscriminatedUnion:
				this.walkDiscriminated(t, n, path);
				return;
			case schema.SKind.NodeKindUnion:
				this.walkNodeKind(t, n, path);
				return;
			case schema.SKind.Nullable:
				this.walkNullable(t, n, path);
				return;
			case schema.SKind.Opaque:
				return; // verbatim leaf: never introspected
		}
	}

	walkRecord(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Record) {
			this.emit("STRICTSPEC_TYPE_NOT_RECORD", path, n, {
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const isRoot = path.steps.length === 1;

		if (t.constraints.length > 0) {
			this.phase2.push({ typ: t, rec: n, path });
		}

		const fieldNames = t.fields.map((f) => f.name);
		const matched = new Set<string>();

		const docIndex = new Map<string, number>();
		n.entries.forEach((e, i) => {
			if (!docIndex.has(e.key)) {
				docIndex.set(e.key, i);
			}
		});

		const present: PresentAction[] = [];
		const missing: string[] = [];
		const missingIdx: number[] = [];

		t.fields.forEach((f, i) => {
			const found: string[] = [];
			if (hasKey(n, f.name)) {
				found.push(f.name);
			}
			for (const a of f.aliases) {
				if (hasKey(n, a)) {
					found.push(a);
				}
			}
			if (found.length >= 2) {
				let aliasName = found[0] as string;
				for (const fn of found) {
					if (fn !== f.name) {
						aliasName = fn;
						break;
					}
				}
				let pos = docIndex.get(found[0] as string) as number;
				for (const fn of found) {
					matched.add(fn);
					const di = docIndex.get(fn) as number;
					if (di < pos) {
						pos = di;
					}
				}
				present.push({
					kind: "alias",
					name: f.name,
					alias: aliasName,
					declIdx: i,
					docPos: pos,
					before: [],
				});
				return;
			}
			if (found.length === 1) {
				const key = found[0] as string;
				matched.add(key);
				present.push({
					kind: "present",
					name: f.name,
					typ: f.type,
					key,
					declIdx: i,
					docPos: docIndex.get(key) as number,
					before: [],
				});
				return;
			}
			if (f.required) {
				missing.push(f.name);
				missingIdx.push(i);
			}
		});

		// Anchor each missing-required field before the first present field
		// declared after it (declaration-order merge; present still in decl order).
		let mi = 0;
		for (const pa of present) {
			while (mi < missing.length && (missingIdx[mi] as number) < pa.declIdx) {
				pa.before.push(missing[mi] as string);
				mi += 1;
			}
		}
		const trailing = missing.slice(mi);

		// Reorder present-key emissions to document order (stable).
		present.sort((a, b) => a.docPos - b.docPos);

		const emitMissing = (names: string[]): void => {
			for (const name of names) {
				this.emit("STRICTSPEC_TYPE_MISSING_REQUIRED", path, n, {
					key: diag.slotString(name),
				});
			}
		};

		for (const pa of present) {
			emitMissing(pa.before);
			if (pa.kind === "alias") {
				this.emit("STRICTSPEC_ALIAS_BOTH_PRESENT", path, n, {
					alias: diag.slotIdentifier(pa.alias),
					canonical: diag.slotIdentifier(pa.name),
				});
			} else {
				const val = entryOf(n, pa.key);
				this.walk(pa.typ, val, diag.appendKey(path, pa.name));
			}
		}
		emitMissing(trailing);

		// Unknown-key pass (document order). Root format_version is exempt.
		for (const e of n.entries) {
			if (matched.has(e.key)) {
				continue;
			}
			if (isRoot && e.key === "format_version") {
				continue;
			}
			this.emit("STRICTSPEC_KEY_UNKNOWN", path, n, {
				key: diag.slotString(e.key),
				suggestion: diag.slotSuggestion(e.key, fieldNames),
			});
		}
	}

	walkMap(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Record) {
			this.emit("STRICTSPEC_TYPE_NOT_MAP", path, n, {
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const keyRe = t.keyPattern !== "" ? compileRegex(t.keyPattern) : null;
		for (const e of n.entries) {
			const kp = diag.appendMapKey(path, e.key);
			if (keyRe !== null && !keyRe.test(e.key)) {
				this.emit("STRICTSPEC_VALUE_MAP_KEY_REGEX", kp, e.value, {
					key: diag.slotString(e.key),
					pattern: diag.slotValue(diag.stringVal(t.keyPattern)),
				});
			}
			if (t.value !== null) {
				this.walk(t.value, e.value, kp);
			}
		}
	}

	walkArray(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Array) {
			this.emit("STRICTSPEC_TYPE_NOT_ARRAY", path, n, {
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const its = n.items;
		if (t.minLen !== null && its.length < t.minLen) {
			this.emit("STRICTSPEC_VALUE_ARRAY_TOO_SHORT", path, n, {
				actual: diag.slotInt(its.length),
				limit: diag.slotInt(t.minLen),
			});
		}
		if (t.maxLen !== null && its.length > t.maxLen) {
			this.emit("STRICTSPEC_VALUE_ARRAY_TOO_LONG", path, n, {
				actual: diag.slotInt(its.length),
				limit: diag.slotInt(t.maxLen),
			});
		}
		if (t.item !== null) {
			its.forEach((it, i) => {
				this.walk(t.item as schema.Type, it, diag.appendIndex(path, i));
			});
		}
	}

	walkTuple(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Array) {
			this.emit("STRICTSPEC_TYPE_MISMATCH", path, n, {
				expected: diag.slotString("tuple"),
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const its = n.items;
		if (its.length !== t.elements.length) {
			this.emit("STRICTSPEC_TYPE_TUPLE_ARITY", path, n, {
				expected: diag.slotInt(t.elements.length),
				got: diag.slotInt(its.length),
			});
			return;
		}
		t.elements.forEach((elemRef, i) => {
			const et = new schema.Type();
			et.kind = schema.SKind.Ref;
			et.ref = elemRef;
			this.walk(et, its[i] as Node, diag.appendIndex(path, i));
		});
	}

	walkNullable(t: schema.Type, n: Node | null, path: Path): void {
		if (n !== null && n.kind === Kind.Null) {
			return;
		}
		if (t.inner !== null) {
			this.walkInner(t.inner, n, path);
		}
	}

	walkDiscriminated(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Record) {
			this.emit("STRICTSPEC_TYPE_MISMATCH", path, n, {
				expected: diag.slotString("record"),
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const discStrs: Value[] = [];
		const armDisc: string[] = [];
		for (const arm of t.arms) {
			const dv = this.armDiscriminator(arm, t.discriminator);
			armDisc.push(dv);
			discStrs.push(diag.stringVal(dv));
		}
		const discNode = entryOf(n, t.discriminator);
		if (discNode === null) {
			this.emit("STRICTSPEC_UNION_DISCRIMINATOR_MISSING", path, n, {
				key: diag.slotString(t.discriminator),
				expected: diag.slotList(discStrs),
			});
			return;
		}
		const got = this.scalarKeyString(discNode);
		for (let i = 0; i < t.arms.length; i++) {
			if (armDisc[i] === got) {
				this.walkInner(
					(t.arms[i] as schema.Arm).type,
					n,
					diag.appendArm(path, (t.arms[i] as schema.Arm).name),
				);
				return;
			}
		}
		this.emit("STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN", path, discNode, {
			got: diag.slotValue(this.valueOf(discNode)),
			expected: diag.slotList(discStrs),
			suggestion: diag.slotSuggestion(got, armDisc),
		});
	}

	walkNodeKind(t: schema.Type, n: Node | null, path: Path): void {
		const cat = nodeCategory(kindOf(n));
		const kinds: Value[] = [];
		for (const arm of t.arms) {
			const ac = this.armCategory(arm.type);
			kinds.push(diag.stringVal(ac));
			if (ac === cat) {
				this.walkInner(arm.type, n, path);
				return;
			}
		}
		this.emit("STRICTSPEC_UNION_NODE_KIND", path, n, {
			got: diag.slotString(cat),
			expected: diag.slotList(kinds),
		});
	}

	walkEnum(t: schema.Type, n: Node | null, path: Path): void {
		const members = this.enumMembers(t);
		const memberVals = members.map((m) => diag.stringVal(m));
		if (t.sourced || allStringEnum(t)) {
			if (n === null || n.kind !== Kind.String) {
				this.emitEnumMiss(n, path, members, memberVals);
				return;
			}
			const val = this.decodeString(n);
			if (members.includes(val)) {
				return;
			}
			this.emitEnumMiss(n, path, members, memberVals);
			return;
		}
		if (n === null) {
			this.emitEnumMiss(n, path, members, memberVals);
			return;
		}
		for (const ev of t.enumValues) {
			if (this.sameScalar(ev, n)) {
				return;
			}
		}
		this.emitEnumMiss(n, path, members, memberVals);
	}

	emitEnumMiss(
		n: Node | null,
		path: Path,
		members: string[],
		memberVals: Value[],
	): void {
		let got = "";
		if (n !== null && n.kind === Kind.String) {
			got = this.decodeString(n);
		}
		this.emit("STRICTSPEC_TYPE_NOT_ENUM_MEMBER", path, n, {
			got: diag.slotValue(this.valueOf(n)),
			expected: diag.slotList(memberVals),
			suggestion: diag.slotSuggestion(got, members),
		});
	}

	walkLiteral(t: schema.Type, n: Node | null, path: Path): void {
		if (n !== null && this.sameScalar(t.literal, n)) {
			return;
		}
		this.emit("STRICTSPEC_TYPE_NOT_LITERAL", path, n, {
			expected: diag.slotValue(svalToValue(t.literal)),
			got: diag.slotValue(this.valueOf(n)),
		});
	}

	// --- union / enum helpers ------------------------------------------------

	armDiscriminator(arm: schema.Arm, discField: string): string {
		const rec = this.resolveRecord(arm.type);
		if (rec !== null) {
			for (const f of rec.fields) {
				if (f.name === discField && f.type.kind === schema.SKind.Literal) {
					return svalKeyString(f.type.literal);
				}
			}
		}
		return arm.name;
	}

	resolveRecord(tIn: schema.Type | null): schema.Type | null {
		let t = tIn;
		let seen = 0;
		while (t !== null && t.kind === schema.SKind.Ref && seen < 32) {
			const named = this.s.types.get(t.ref);
			if (named === undefined) {
				return null;
			}
			t = named;
			seen += 1;
		}
		return t;
	}

	armCategory(tIn: schema.Type | null): string {
		let t = tIn;
		let seen = 0;
		while (t !== null && t.kind === schema.SKind.Ref) {
			const named = this.s.types.get(t.ref);
			if (named !== undefined && seen < 32) {
				t = named;
				seen += 1;
				continue;
			}
			return "scalar";
		}
		if (t === null) {
			return "scalar";
		}
		if (t.kind === schema.SKind.Record || t.kind === schema.SKind.Map) {
			return "record";
		}
		if (t.kind === schema.SKind.Array || t.kind === schema.SKind.Tuple) {
			return "array";
		}
		return "scalar";
	}

	enumMembers(t: schema.Type): string[] {
		if (t.sourced) {
			return t.baked;
		}
		return t.enumValues.map((ev) => svalKeyString(ev));
	}

	scalarKeyString(n: Node | null): string {
		if (n === null) {
			return "";
		}
		if (n.kind === Kind.String) {
			return this.decodeString(n);
		}
		return n.lexeme;
	}

	sameScalar(sv: schema.SVal, n: Node | null): boolean {
		if (n === null) {
			return false;
		}
		if (sv.kind === Kind.String) {
			return n.kind === Kind.String && this.decodeString(n) === sv.s;
		}
		if (sv.kind === Kind.Integer) {
			return n.kind === Kind.Integer && svalIntLexeme(n.lexeme) === sv.i;
		}
		if (sv.kind === Kind.Bool) {
			return n.kind === Kind.Bool && (n.lexeme === "true") === sv.b;
		}
		if (sv.kind === Kind.Float) {
			return n.kind === Kind.Float && n.lexeme === sv.lexeme;
		}
		return false;
	}

	// --- string/value helpers ------------------------------------------------

	decodeString(n: Node | null): string {
		if (n === null) {
			return "";
		}
		if (this.format === doc.FORMAT_TOML) {
			return decodeTOML(n.lexeme);
		}
		return decodeJSON(n.lexeme);
	}

	valueOf(n: Node | null): Value {
		if (n === null) {
			return diag.nullVal();
		}
		switch (n.kind) {
			case Kind.String:
				return diag.stringVal(this.decodeString(n));
			case Kind.Integer:
				return diag.numberVal(n.lexeme, true);
			case Kind.Float:
				return diag.floatVal({ lexeme: n.lexeme, hasLexeme: true });
			case Kind.Bool:
				return diag.boolVal(n.lexeme === "true");
			case Kind.Null:
				return diag.nullVal();
			case Kind.DateLocal:
				return diag.dateVal(this.decodeString(n));
			case Kind.TimeLocal:
				return diag.timeVal(this.decodeString(n));
			case Kind.DateTimeOffset:
			case Kind.DateTimeLocal:
				return diag.datetimeVal(this.decodeString(n));
			default:
				return diag.stringVal(n.lexeme);
		}
	}

	// --- scalar validation ---------------------------------------------------

	walkScalar(t: schema.Type, n: Node | null, path: Path): void {
		const ref = t.ref;
		if (ref === "string") {
			this.validateString(t, n, path);
		} else if (ref === "integer") {
			this.validateInteger(t, n, path);
		} else if (ref === "float") {
			this.validateFloat(t, n, path);
		} else if (ref === "number") {
			this.validateNumber(t, n, path);
		} else if (ref === "boolean") {
			this.validateBool(n, path);
		} else if (ref === "date" || ref === "time" || ref === "datetime") {
			this.validateDatetime(t, n, path);
		} else {
			const cs = this.scalars.get(ref);
			if (cs !== undefined) {
				this.validateCustomScalar(cs, n, path);
			}
			// Unknown ref: skip silently.
		}
	}

	validateString(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.String) {
			this.emit("STRICTSPEC_TYPE_NOT_STRING", path, n, {
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		const val = this.decodeString(n);
		const length = diag.codePointLength(val);
		if (t.nonEmpty && length === 0) {
			this.emit("STRICTSPEC_VALUE_STRING_EMPTY", path, n, {});
		}
		if (t.minLength !== null && length < t.minLength) {
			this.emit("STRICTSPEC_VALUE_STRING_TOO_SHORT", path, n, {
				actual: diag.slotInt(length),
				limit: diag.slotInt(t.minLength),
			});
		}
		if (t.maxLength !== null && length > t.maxLength) {
			this.emit("STRICTSPEC_VALUE_STRING_TOO_LONG", path, n, {
				actual: diag.slotInt(length),
				limit: diag.slotInt(t.maxLength),
			});
		}
		if (t.hasRegex && !compileRegex(t.regex).test(val)) {
			this.emit("STRICTSPEC_VALUE_STRING_REGEX", path, n, {
				actual: diag.slotValue(diag.stringVal(val)),
				pattern: diag.slotValue(diag.stringVal(t.regex)),
			});
		}
	}

	validateInteger(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Integer) {
			let got = nodeKindName(kindOf(n));
			if (n !== null && n.kind === Kind.Float) {
				got = "float";
			}
			this.emit("STRICTSPEC_TYPE_NOT_INTEGER", path, n, {
				got: diag.slotString(got),
			});
			return;
		}
		const iv = schema.goParseInt(n.lexeme);
		if (iv === null) {
			this.emit("STRICTSPEC_NUM_INT_OVERFLOW", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
			});
			return;
		}
		if (this.s.safeIntegers && absBig(iv) >= 1n << 53n) {
			this.emit("STRICTSPEC_NUM_SAFE_INTEGER", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
			});
		}
		this.checkNumericBounds(t, n, path, Number(iv));
	}

	validateFloat(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Float) {
			let got = nodeKindName(kindOf(n));
			if (n !== null && n.kind === Kind.Integer) {
				got = "integer";
			}
			this.emit("STRICTSPEC_TYPE_NOT_FLOAT", path, n, {
				got: diag.slotString(got),
			});
			return;
		}
		const f = schema.goParseFloat(n.lexeme);
		if (f === null) {
			this.emit("STRICTSPEC_NUM_FLOAT_OVERFLOW", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
			});
			return;
		}
		this.checkNumericBounds(t, n, path, f);
	}

	validateNumber(t: schema.Type, n: Node | null, path: Path): void {
		if (n === null || (n.kind !== Kind.Integer && n.kind !== Kind.Float)) {
			this.emit("STRICTSPEC_TYPE_MISMATCH", path, n, {
				expected: diag.slotString("number"),
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
			return;
		}
		if (n.kind === Kind.Integer && !exactlyRepresentable(n.lexeme)) {
			this.emit("STRICTSPEC_NUM_UNREPRESENTABLE", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
			});
			return;
		}
		const f = schema.goParseFloat(n.lexeme);
		if (f === null) {
			this.emit("STRICTSPEC_NUM_UNREPRESENTABLE", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
			});
			return;
		}
		this.checkNumericBounds(t, n, path, f);
	}

	validateBool(n: Node | null, path: Path): void {
		if (n === null || n.kind !== Kind.Bool) {
			this.emit("STRICTSPEC_TYPE_NOT_BOOLEAN", path, n, {
				got: diag.slotString(nodeKindName(kindOf(n))),
			});
		}
	}

	checkNumericBounds(t: schema.Type, n: Node, path: Path, val: number): void {
		if (t.min !== null && val < svalNum(t.min)) {
			this.emit("STRICTSPEC_VALUE_NUM_TOO_SMALL", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
				limit: diag.slotValue(svalToValue(t.min)),
			});
		}
		if (t.exclusiveMin !== null && val <= svalNum(t.exclusiveMin)) {
			this.emit("STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
				limit: diag.slotValue(svalToValue(t.exclusiveMin)),
			});
		}
		if (t.max !== null && val > svalNum(t.max)) {
			this.emit("STRICTSPEC_VALUE_NUM_TOO_LARGE", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
				limit: diag.slotValue(svalToValue(t.max)),
			});
		}
		if (t.exclusiveMax !== null && val >= svalNum(t.exclusiveMax)) {
			this.emit("STRICTSPEC_VALUE_NUM_TOO_LARGE_EXCLUSIVE", path, n, {
				actual: diag.slotValue(this.valueOf(n)),
				limit: diag.slotValue(svalToValue(t.exclusiveMax)),
			});
		}
	}

	validateCustomScalar(cs: schema.Scalar, n: Node | null, path: Path): void {
		if (cs.base === "string" && (n === null || n.kind !== Kind.String)) {
			this.emit("STRICTSPEC_SCALAR_BASE_MISMATCH", path, n, {
				expected: diag.slotString(cs.base),
				name: diag.slotIdentifier(cs.name),
			});
			return;
		}
		const val = this.decodeString(n);
		const length = diag.codePointLength(val);
		if (cs.lenMin !== null && length < cs.lenMin) {
			this.emitScalarLength(cs, n, path, length, cs.lenMin);
			return;
		}
		if (cs.nonEmpty && length === 0) {
			this.emitScalarLength(cs, n, path, length, 1);
			return;
		}
		if (cs.lenMax !== null && length > cs.lenMax) {
			this.emitScalarLength(cs, n, path, length, cs.lenMax);
			return;
		}
		if (cs.lexemeRule !== "" && !compileRegex(cs.lexemeRule).test(val)) {
			this.emit("STRICTSPEC_SCALAR_LEXEME", path, n, {
				actual: diag.slotValue(diag.stringVal(val)),
				name: diag.slotIdentifier(cs.name),
				pattern: diag.slotValue(diag.stringVal(cs.lexemeRule)),
			});
		}
	}

	emitScalarLength(
		cs: schema.Scalar,
		n: Node | null,
		path: Path,
		actual: number,
		limit: number,
	): void {
		this.emit("STRICTSPEC_SCALAR_LENGTH", path, n, {
			name: diag.slotIdentifier(cs.name),
			actual: diag.slotInt(actual),
			limit: diag.slotInt(limit),
		});
	}

	// --- datetime ------------------------------------------------------------

	validateDatetime(t: schema.Type, n: Node | null, path: Path): void {
		let form = "";
		if (n !== null && n.kind === Kind.String) {
			form = classifyRFC3339(this.decodeString(n));
		} else if (n !== null) {
			switch (n.kind) {
				case Kind.DateLocal:
					form = "date";
					break;
				case Kind.TimeLocal:
					form = "time";
					break;
				case Kind.DateTimeOffset:
					form = "datetime-offset";
					break;
				case Kind.DateTimeLocal:
					form = "datetime-local";
					break;
			}
		}
		const ref = t.ref;
		if (ref === "date") {
			if (form !== "date") {
				this.emit("STRICTSPEC_TYPE_NOT_DATE", path, n, {
					got: diag.slotString(formGot(form, n)),
				});
			}
			return;
		}
		if (ref === "time") {
			if (form !== "time") {
				this.emit("STRICTSPEC_TYPE_NOT_TIME", path, n, {
					got: diag.slotString(formGot(form, n)),
				});
			}
			return;
		}
		if (ref === "datetime") {
			if (form !== "datetime-offset" && form !== "datetime-local") {
				this.emit("STRICTSPEC_TYPE_NOT_DATETIME", path, n, {
					got: diag.slotString(formGot(form, n)),
				});
				return;
			}
			const want = t.datetimeKind; // "offset" | "local"
			const got = form === "datetime-local" ? "local" : "offset";
			if (want !== "" && want !== got) {
				this.emit("STRICTSPEC_TYPE_DATETIME_KIND", path, n, {
					expected: diag.slotString(want),
					got: diag.slotString(got),
				});
				return;
			}
			this.checkDatetimeRange(t, n as Node, path);
		}
	}

	checkDatetimeRange(t: schema.Type, n: Node, path: Path): void {
		const val = this.decodeString(n);
		const vi = parseInstant(val);
		if (vi === null) {
			return;
		}
		if (t.min !== null && t.min.kind === Kind.String) {
			const mi = parseInstant(t.min.s);
			if (mi !== null && vi < mi) {
				this.emit("STRICTSPEC_VALUE_DATETIME_BEFORE", path, n, {
					actual: diag.slotValue(this.valueOf(n)),
					limit: diag.slotValue(diag.datetimeVal(t.min.s)),
				});
			}
		}
		if (t.max !== null && t.max.kind === Kind.String) {
			const ma = parseInstant(t.max.s);
			if (ma !== null && vi > ma) {
				this.emit("STRICTSPEC_VALUE_DATETIME_AFTER", path, n, {
					actual: diag.slotValue(this.valueOf(n)),
					limit: diag.slotValue(diag.datetimeVal(t.max.s)),
				});
			}
		}
	}

	// --- conditions ----------------------------------------------------------

	evalCondition(rec: Node, c: schema.Condition | null): boolean {
		if (c === null) {
			return false;
		}
		const fn = entryOf(rec, c.field);
		const present = fn !== null;
		const p = c.predicate;
		if (p === "present") {
			return present;
		}
		if (p === "absent") {
			return !present;
		}
		if (p === "equals") {
			return present && this.sameScalar(c.value, fn);
		}
		if (p === "not-equals") {
			return present && !this.sameScalar(c.value, fn);
		}
		if (p === "in") {
			if (!present) {
				return false;
			}
			return c.values.some((val) => this.sameScalar(val, fn));
		}
		if (p === "not-in") {
			if (!present) {
				return false;
			}
			return !c.values.some((val) => this.sameScalar(val, fn));
		}
		return false;
	}

	// --- phase 2 constraints -------------------------------------------------

	runConstraints(task: P2Task): void {
		const rec = task.rec;
		const path = task.path;
		for (const c of task.typ.constraints) {
			switch (c.form) {
				case "conditional-required":
					if (this.evalCondition(rec, c.when) && !hasKey(rec, c.field)) {
						this.emit("STRICTSPEC_INTRA_CONDITIONAL_REQUIRED", path, rec, {
							key: diag.slotString(c.field),
							condition: diag.slotString(renderCondition(c.when)),
						});
					}
					break;
				case "forbidden-when":
					if (this.evalCondition(rec, c.when) && hasKey(rec, c.field)) {
						const fn = entryOf(rec, c.field);
						this.emit(
							"STRICTSPEC_INTRA_FORBIDDEN_WHEN",
							diag.appendKey(path, c.field),
							fn,
							{
								key: diag.slotString(c.field),
								condition: diag.slotString(renderCondition(c.when)),
							},
						);
					}
					break;
				case "conditional-value": {
					const fn = entryOf(rec, c.field);
					if (
						fn !== null &&
						this.evalCondition(rec, c.when) &&
						!this.sameScalar(c.equalsLiteral, fn)
					) {
						this.emit(
							"STRICTSPEC_INTRA_CONDITIONAL_VALUE",
							diag.appendKey(path, c.field),
							fn,
							{
								key: diag.slotString(c.field),
								expected: diag.slotValue(svalToValue(c.equalsLiteral)),
								got: diag.slotValue(this.valueOf(fn)),
								condition: diag.slotString(renderCondition(c.when)),
							},
						);
					}
					break;
				}
				case "exactly-one-of": {
					const present = presentOf(rec, c.fields);
					if (present.length !== 1) {
						this.emit("STRICTSPEC_INTRA_EXACTLY_ONE_OF", path, rec, {
							fields: strList(c.fields),
							actual: strList(present),
						});
					}
					break;
				}
				case "at-least-one-of":
					if (presentOf(rec, c.fields).length === 0) {
						this.emit("STRICTSPEC_INTRA_AT_LEAST_ONE_OF", path, rec, {
							fields: strList(c.fields),
						});
					}
					break;
				case "co-presence": {
					const present = presentOf(rec, c.fields);
					if (present.length !== 0 && present.length !== c.fields.length) {
						this.emit("STRICTSPEC_INTRA_CO_PRESENCE", path, rec, {
							fields: strList(c.fields),
							actual: strList(present),
						});
					}
					break;
				}
				case "mutual-exclusion": {
					const present = presentOf(rec, c.fields);
					if (present.length >= 2) {
						this.emit("STRICTSPEC_INTRA_MUTUAL_EXCLUSION", path, rec, {
							fields: strList(c.fields),
							actual: strList(present),
						});
					}
					break;
				}
				case "collections-disjoint":
					this.collectionsDisjoint(rec, path, c);
					break;
				case "unique-by":
					this.uniqueBy(rec, path, c);
					break;
				case "pairwise-distinct":
					this.pairwiseDistinct(rec, path, c);
					break;
				case "ranges-disjoint":
					this.rangesDisjoint(rec, path, c);
					break;
				case "ordered-pair":
					this.orderedPair(rec, path, c);
					break;
				case "intra-document-references":
					this.intraReferences(rec, path, c);
					break;
				case "count-limit":
					this.countLimit(path, c);
					break;
				case "sum-limit":
					this.sumLimit(path, c);
					break;
			}
		}
	}

	collectionsDisjoint(rec: Node, path: Path, c: schema.Constraint): void {
		const left = this.stringElems(rec, c.left);
		const right = this.stringElems(rec, c.right);
		const seen = new Set(left.map((s) => normalize(s, c.normalization)));
		for (const s of right) {
			if (seen.has(normalize(s, c.normalization))) {
				this.emit("STRICTSPEC_INTRA_COLLECTIONS_DISJOINT", path, rec, {
					fields: strList([c.left, c.right]),
					value: diag.slotValue(diag.stringVal(s)),
					normalization: diag.slotString(normOr(c.normalization)),
				});
				return;
			}
		}
	}

	uniqueBy(rec: Node, path: Path, c: schema.Constraint): void {
		const coll = entryOf(rec, c.collection);
		if (coll === null || coll.kind !== Kind.Array) {
			return;
		}
		const seen = new Set<string>();
		for (const elem of coll.items) {
			const fn = entryOf(elem, c.uniqField);
			if (fn === null || fn.kind !== Kind.String) {
				continue;
			}
			const val = this.decodeString(fn);
			const key = normalize(val, c.normalization);
			if (seen.has(key)) {
				this.emit(
					"STRICTSPEC_INTRA_UNIQUE_BY",
					diag.appendKey(path, c.collection),
					coll,
					{
						value: diag.slotValue(diag.stringVal(val)),
						field: diag.slotString(c.uniqField),
						normalization: diag.slotString(normOr(c.normalization)),
					},
				);
				return;
			}
			seen.add(key);
		}
	}

	pairwiseDistinct(rec: Node, path: Path, c: schema.Constraint): void {
		const coll = entryOf(rec, c.collection);
		if (coll === null || coll.kind !== Kind.Array) {
			return;
		}
		const seen = new Set<string>();
		for (const elem of coll.items) {
			if (elem.kind !== Kind.String) {
				continue;
			}
			const val = this.decodeString(elem);
			const key = normalize(val, c.normalization);
			if (seen.has(key)) {
				this.emit(
					"STRICTSPEC_INTRA_PAIRWISE_DISTINCT",
					diag.appendKey(path, c.collection),
					coll,
					{
						value: diag.slotValue(diag.stringVal(val)),
						normalization: diag.slotString(normOr(c.normalization)),
					},
				);
				return;
			}
			seen.add(key);
		}
	}

	rangesDisjoint(rec: Node, path: Path, c: schema.Constraint): void {
		const coll = entryOf(rec, c.collection);
		if (coll === null || coll.kind !== Kind.Array) {
			return;
		}
		const ranges: Array<[number, number]> = [];
		for (const elem of coll.items) {
			const s = intField(elem, c.start);
			const length = intField(elem, c.length);
			if (s === null || length === null) {
				continue;
			}
			ranges.push([s, s + length]);
		}
		const fmtRange = (r: [number, number]): string => `[${r[0]}, ${r[1]})`;
		for (let i = 0; i < ranges.length; i++) {
			for (let j = 0; j < i; j++) {
				const a = ranges[j] as [number, number];
				const b = ranges[i] as [number, number];
				if (a[0] < b[1] && b[0] < a[1]) {
					this.emit(
						"STRICTSPEC_INTRA_RANGES_DISJOINT",
						diag.appendKey(path, c.collection),
						coll,
						{
							value: diag.slotString(fmtRange(b)),
							actual: diag.slotString(fmtRange(a)),
						},
					);
					return;
				}
			}
		}
	}

	orderedPair(rec: Node, path: Path, c: schema.Constraint): void {
		const ln = numField(rec, c.less);
		const tn = numField(rec, c.than);
		if (ln === null || tn === null) {
			return;
		}
		if (!(ln < tn)) {
			this.emit("STRICTSPEC_INTRA_ORDERED_PAIR", path, rec, {
				actual: diag.slotString(c.less),
				value: diag.slotString(c.than),
			});
		}
	}

	intraReferences(rec: Node, path: Path, c: schema.Constraint): void {
		const keyset = this.rootKeyset(c.resolvesInto);
		if (keyset === null) {
			return;
		}
		if (c.reference.includes("[].")) {
			const idx = c.reference.indexOf("[].");
			const p0 = c.reference.slice(0, idx);
			const p1 = c.reference.slice(idx + 3);
			const coll = entryOf(this.root, p0);
			if (coll === null || coll.kind !== Kind.Array) {
				return;
			}
			coll.items.forEach((elem, i) => {
				const fn = entryOf(elem, p1);
				if (fn === null || fn.kind !== Kind.String) {
					return;
				}
				const val = this.decodeString(fn);
				if (!keyset.has(val)) {
					const p = diag.appendKey(
						diag.appendIndex(diag.appendKey(diag.newPath(), p0), i),
						p1,
					);
					this.emit("STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", p, fn, {
						value: diag.slotValue(diag.stringVal(val)),
					});
				}
			});
			return;
		}
		const refNode = entryOf(rec, c.reference);
		if (refNode === null) {
			return;
		}
		if (refNode.kind === Kind.Array) {
			refNode.items.forEach((elem, i) => {
				if (elem.kind !== Kind.String) {
					return;
				}
				const val = this.decodeString(elem);
				if (!keyset.has(val)) {
					this.emit(
						"STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
						diag.appendIndex(diag.appendKey(path, c.reference), i),
						elem,
						{ value: diag.slotValue(diag.stringVal(val)) },
					);
				}
			});
		} else if (refNode.kind === Kind.String) {
			const val = this.decodeString(refNode);
			if (!keyset.has(val)) {
				this.emit(
					"STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
					diag.appendKey(path, c.reference),
					refNode,
					{
						value: diag.slotValue(diag.stringVal(val)),
					},
				);
			}
		} else if (refNode.kind === Kind.Record) {
			for (const e of refNode.entries) {
				if (!keyset.has(e.key)) {
					this.emit(
						"STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
						diag.appendKey(path, c.reference),
						refNode,
						{
							value: diag.slotValue(diag.stringVal(e.key)),
						},
					);
				}
			}
		}
		// Null: nullable reference short-circuits.
	}

	rootKeyset(name: string): Set<string> | null {
		const coll = entryOf(this.root, name);
		if (coll === null) {
			return null;
		}
		const s = new Set<string>();
		if (coll.kind === Kind.Record) {
			for (const e of coll.entries) {
				s.add(e.key);
			}
		} else if (coll.kind === Kind.Array) {
			for (const elem of coll.items) {
				if (elem.kind === Kind.String) {
					s.add(this.decodeString(elem));
				} else if (elem.kind === Kind.Record) {
					const nn = entryOf(elem, "name");
					if (nn !== null && nn.kind === Kind.String) {
						s.add(this.decodeString(nn));
					}
				}
			}
		}
		return s;
	}

	countLimit(path: Path, c: schema.Constraint): void {
		const docs = this.evidence[c.selection];
		if (docs === undefined) {
			return;
		}
		const count = docs.length;
		const limit = Number(c.limit.i);
		const violated =
			(c.compare === "<=" && count > limit) ||
			(c.compare === ">=" && count < limit);
		if (violated) {
			this.emit("STRICTSPEC_CROSS_COUNT_LIMIT", path, this.root, {
				actual: diag.slotInt(count),
				source: diag.slotString(c.selection),
				limit: diag.slotInt(limit),
			});
		}
	}

	sumLimit(path: Path, c: schema.Constraint): void {
		const docs = this.evidence[c.selection];
		if (docs === undefined) {
			return;
		}
		let total = 0;
		let allInt = true;
		for (const d of docs) {
			const present = c.sumField in d;
			const [f, numeric] = asFloat(d[c.sumField]);
			if (!present || !numeric) {
				this.emit("STRICTSPEC_CROSS_SUM_FIELD_MISSING", path, this.root, {
					source: diag.slotString(c.selection),
					field: diag.slotString(c.sumField),
					actual: diag.slotString(docName(d)),
				});
				return;
			}
			if (f !== Math.trunc(f)) {
				allInt = false;
			}
			total += f;
		}
		const limit = svalNum(c.limit);
		const violated =
			(c.compare === "<=" && total > limit) ||
			(c.compare === ">=" && total < limit);
		if (violated) {
			this.emit("STRICTSPEC_CROSS_SUM_LIMIT", path, this.root, {
				field: diag.slotString(c.sumField),
				source: diag.slotString(c.selection),
				actual: diag.slotValue(sumValue(total, allInt)),
				limit: diag.slotValue(svalToValue(c.limit)),
			});
		}
	}

	stringElems(rec: Node, fieldName: string): string[] {
		const n = entryOf(rec, fieldName);
		if (n === null || n.kind !== Kind.Array) {
			return [];
		}
		return n.items
			.filter((e) => e.kind === Kind.String)
			.map((e) => this.decodeString(e));
	}
}

export function execute(
	p: Program,
	root: Node | null,
	opts: ExecOptions,
): Diagnostic[] {
	const v = new Exec(p, root, opts);
	if (!v.gate(root)) {
		return v.diags.all();
	}
	const rt = v.s.types.get(v.s.root);
	if (rt === undefined) {
		return v.diags.all();
	}
	v.walk(rt, root, diag.newPath());
	if (!(opts.structuralOnly ?? false)) {
		for (const task of v.phase2) {
			if (v.clean.get(task.rec)) {
				v.runConstraints(task);
			}
		}
	}
	return v.diags.all();
}

// Run only the gate against a root node, returning [ok, diagnostics].
export function runGate(
	p: Program,
	root: Node | null,
	opts: ExecOptions,
): [boolean, Diagnostic[]] {
	const v = new Exec(p, root, opts);
	const ok = v.gate(root);
	return [ok, v.diags.all()];
}

// --- module-level helpers ----------------------------------------------------

function kindOf(n: Node | null): Kind {
	return n === null ? Kind.Null : n.kind;
}

function nodeKindName(k: Kind): string {
	switch (k) {
		case Kind.Record:
			return "record";
		case Kind.Array:
			return "array";
		case Kind.String:
			return "string";
		case Kind.Integer:
			return "integer";
		case Kind.Float:
			return "float";
		case Kind.Bool:
			return "boolean";
		case Kind.Null:
			return "null";
		case Kind.DateTimeOffset:
		case Kind.DateTimeLocal:
			return "datetime";
		case Kind.DateLocal:
			return "date";
		case Kind.TimeLocal:
			return "time";
		default:
			return "value";
	}
}

function nodeCategory(k: Kind): string {
	if (k === Kind.Record) {
		return "record";
	}
	if (k === Kind.Array) {
		return "array";
	}
	return "scalar";
}

function entryOf(rec: Node | null, key: string): Node | null {
	if (rec === null || rec.kind !== Kind.Record) {
		return null;
	}
	for (const e of rec.entries) {
		if (e.key === key) {
			return e.value;
		}
	}
	return null;
}

function hasKey(rec: Node | null, key: string): boolean {
	return entryOf(rec, key) !== null;
}

function svalIntLexeme(lexeme: string): bigint {
	let n = 0n;
	let neg = false;
	for (let i = 0; i < lexeme.length; i++) {
		const c = lexeme[i] as string;
		if (i === 0 && c === "-") {
			neg = true;
			continue;
		}
		if (c < "0" || c > "9") {
			break;
		}
		n = n * 10n + BigInt(c.charCodeAt(0) - 48);
	}
	return neg ? -n : n;
}

function exactlyRepresentable(lexeme: string): boolean {
	const i = schema.goParseInt(lexeme);
	if (i === null) {
		return false;
	}
	// |i| < 2^53 is always exactly representable; at/above, require exact
	// round-trip through float64 (Number).
	if (absBig(i) < 1n << 53n) {
		return true;
	}
	const f = Number(i);
	return BigInt(f) === i;
}

function svalToValue(sv: schema.SVal): Value {
	if (sv.kind === Kind.String) {
		return diag.stringVal(sv.s);
	}
	if (sv.kind === Kind.Integer) {
		return diag.numberVal(sv.lexeme, true);
	}
	if (sv.kind === Kind.Float) {
		return diag.floatVal({ lexeme: sv.lexeme, hasLexeme: true });
	}
	if (sv.kind === Kind.Bool) {
		return diag.boolVal(sv.b);
	}
	return diag.stringVal(sv.s);
}

function svalNum(sv: schema.SVal): number {
	if (sv.kind === Kind.Integer) {
		return Number(sv.i);
	}
	return sv.f;
}

function svalKeyString(sv: schema.SVal): string {
	if (sv.kind === Kind.String) {
		return sv.s;
	}
	return sv.lexeme;
}

function allStringEnum(t: schema.Type): boolean {
	for (const ev of t.enumValues) {
		if (ev.kind !== Kind.String) {
			return false;
		}
	}
	return t.enumValues.length > 0;
}

function presentOf(rec: Node, fields: string[]): string[] {
	return fields.filter((f) => hasKey(rec, f));
}

function strList(names: string[]): Slot {
	return diag.slotList(names.map((n) => diag.stringVal(n)));
}

function intField(rec: Node, fieldName: string): number | null {
	const n = entryOf(rec, fieldName);
	if (n === null || n.kind !== Kind.Integer) {
		return null;
	}
	return Number(svalIntLexeme(n.lexeme));
}

function numField(rec: Node, fieldName: string): number | null {
	const n = entryOf(rec, fieldName);
	if (n === null) {
		return null;
	}
	if (n.kind === Kind.Integer || n.kind === Kind.Float) {
		return schema.goParseFloat(n.lexeme);
	}
	return null;
}

function normalize(s: string, mode: string): string {
	if (mode === "case-fold") {
		return s.toLowerCase();
	}
	if (mode === "trim") {
		return s.trim();
	}
	return s;
}

function normOr(mode: string): string {
	return mode === "" ? "none" : mode;
}

function asFloat(v: unknown): [number, boolean] {
	if (typeof v === "boolean") {
		return [0, false];
	}
	if (typeof v === "number") {
		return [v, true];
	}
	return [0, false];
}

function sumValue(total: number, allInt: boolean): Value {
	if (allInt) {
		return diag.numberVal(String(Math.trunc(total)), true);
	}
	return diag.floatVal({ f: total });
}

function docName(d: EvidenceDoc): string {
	const n = d.name;
	if (typeof n === "string") {
		return n;
	}
	return "<document>";
}

function renderCondition(c: schema.Condition | null): string {
	if (c === null) {
		return "";
	}
	const p = c.predicate;
	if (p === "present") {
		return `${c.field} present`;
	}
	if (p === "absent") {
		return `${c.field} absent`;
	}
	if (p === "equals") {
		return `${c.field} == ${renderLiteral(c.value)}`;
	}
	if (p === "not-equals") {
		return `${c.field} != ${renderLiteral(c.value)}`;
	}
	if (p === "in") {
		return `${c.field} in [${joinLiterals(c.values)}]`;
	}
	if (p === "not-in") {
		return `${c.field} not in [${joinLiterals(c.values)}]`;
	}
	return "";
}

function joinLiterals(vals: schema.SVal[]): string {
	return vals.map((sv) => renderLiteral(sv)).join(", ");
}

function renderLiteral(sv: schema.SVal): string {
	if (sv.kind === Kind.String) {
		return `"${diag.escapeString(sv.s)}"`;
	}
	if (sv.kind === Kind.Bool) {
		return sv.b ? "true" : "false";
	}
	return sv.lexeme;
}

function classifyRFC3339(s: string): string {
	if (/^\d{4}-\d{2}-\d{2}$/.test(s)) {
		return "date";
	}
	if (/^\d{2}:\d{2}:\d{2}(\.\d+)?$/.test(s)) {
		return "time";
	}
	if (s.includes("T")) {
		if (/(Z|[+-]\d{2}:\d{2})$/.test(s)) {
			return "datetime-offset";
		}
		return "datetime-local";
	}
	return "";
}

function formGot(form: string, n: Node | null): string {
	if (form === "datetime-offset" || form === "datetime-local") {
		return "datetime";
	}
	if (form !== "") {
		return form;
	}
	return nodeKindName(kindOf(n));
}

// Parse an RFC 3339 date-time (offset or local) into a comparable instant
// (nanoseconds, bigint). Local forms are interpreted as UTC for a naive
// comparison, mirroring Go's time.Parse / Python's fromisoformat behaviour.
function parseInstant(s: string): bigint | null {
	const m =
		/^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})?$/.exec(
			s,
		);
	if (m === null) {
		return null;
	}
	const y = Number(m[1]);
	const mo = Number(m[2]);
	const d = Number(m[3]);
	const h = Number(m[4]);
	const mi = Number(m[5]);
	const sec = Number(m[6]);
	const frac = m[7];
	const off = m[8];
	const ms = Date.UTC(y, mo - 1, d, h, mi, sec);
	if (Number.isNaN(ms)) {
		return null;
	}
	let ns = BigInt(ms) * 1_000_000n;
	if (frac !== undefined) {
		const padded = (frac + "000000000").slice(0, 9);
		ns += BigInt(padded);
	}
	if (off !== undefined && off !== "Z") {
		const sign = off[0] === "-" ? -1n : 1n;
		const oh = BigInt(off.slice(1, 3));
		const om = BigInt(off.slice(4, 6));
		ns -= sign * (oh * 3600n + om * 60n) * 1_000_000_000n;
	}
	return ns;
}

function absBig(n: bigint): bigint {
	return n < 0n ? -n : n;
}
