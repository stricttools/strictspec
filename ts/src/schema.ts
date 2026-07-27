// The strictspec meta-schema reader.
//
// A faithful port of go/internal/schema (model.go + sval.go + read.go + load.go)
// and python _schema. Parses a schema file (or a type-definition file) authored
// in the pinned TOML surface into a typed Schema model, and emits the catalogued
// STRICTSPEC_SCHEMA_*/STRICTSPEC_IMPORT_* authoring diagnostics.
//
// Type-reference resolution is DEFERRED to the IR executor, which resolves
// against builtins + named types + the manifest's custom scalars. The reader
// records reference names verbatim; it never emits a dangling-ref diagnostic, so
// it can load a schema without its manifest (the examples/ sweep relies on this).
//
// This module is browser-safe: it operates only over in-memory FileSets. Disk
// loading is a Node-scoped concern kept out of the runtime.

import * as diag from "./diag.js";
import { type Diagnostic, Path } from "./diag.js";
import type * as doc from "./doc.js";
import { Kind, type Node } from "./doc.js";
import { decodeTOML } from "./strdecode.js";
import * as tomldoc from "./tomldoc.js";

export enum SKind {
	Ref = 0,
	Record = 1,
	Map = 2,
	Array = 3,
	Tuple = 4,
	Enum = 5,
	Literal = 6,
	DiscriminatedUnion = 7,
	NodeKindUnion = 8,
	Nullable = 9,
	Opaque = 10,
}

// A schema-authored literal value: an enum member, a `literal` value, a
// numeric/datetime bound, or a condition operand.
export class SVal {
	kind: Kind = Kind.Null;
	lexeme = "";
	s = "";
	i: bigint = 0n;
	isInt = false;
	f = 0;
	b = false;
}

export function svalFromNode(n: Node | null): SVal {
	const s = new SVal();
	if (n === null) {
		return s;
	}
	s.kind = n.kind;
	s.lexeme = n.lexeme;
	if (n.kind === Kind.String) {
		s.s = decodeTOML(n.lexeme);
	} else if (n.kind === Kind.Integer) {
		const v = goParseInt(n.lexeme);
		if (v !== null) {
			s.i = v;
			s.isInt = true;
		}
	} else if (n.kind === Kind.Float) {
		const f = goParseFloat(n.lexeme);
		if (f !== null) {
			s.f = f;
		}
	} else if (n.kind === Kind.Bool) {
		s.b = n.lexeme === "true";
	}
	return s;
}

export class Field {
	name: string;
	type: Type;
	required = false;
	aliases: string[] = [];
	constructor(name: string, type: Type) {
		this.name = name;
		this.type = type;
	}
}

export class Arm {
	name: string;
	type: Type;
	constructor(name: string, type: Type) {
		this.name = name;
		this.type = type;
	}
}

export class Condition {
	field = "";
	predicate = "";
	value: SVal = new SVal();
	hasValue = false;
	values: SVal[] = [];
}

export class Constraint {
	form = "";
	field = "";
	when: Condition | null = null;
	equalsLiteral: SVal = new SVal();
	hasEquals = false;
	fields: string[] = [];
	left = "";
	right = "";
	collection = "";
	uniqField = "";
	normalization = "";
	start = "";
	length = "";
	less = "";
	than = "";
	reference = "";
	resolvesInto = "";
	resolvesBy = "";
	source = "";
	selection = "";
	compare = "";
	limit: SVal = new SVal();
	hasLimit = false;
	sumField = "";
}

export class Type {
	kind: SKind = SKind.Ref;
	ref = "";
	// scalar refinements
	min: SVal | null = null;
	max: SVal | null = null;
	exclusiveMin: SVal | null = null;
	exclusiveMax: SVal | null = null;
	minLength: number | null = null;
	maxLength: number | null = null;
	nonEmpty = false;
	regex = "";
	hasRegex = false;
	datetimeKind = "";
	// record / discriminated
	fields: Field[] = [];
	// map
	keyPattern = "";
	order = "";
	value: Type | null = null;
	// array
	minLen: number | null = null;
	maxLen: number | null = null;
	item: Type | null = null;
	// tuple
	elements: string[] = [];
	// enum
	enumValues: SVal[] = [];
	sourced = false;
	baked: string[] = [];
	sourceDoc = "";
	sourceSel = "";
	// literal
	literal: SVal = new SVal();
	// union
	discriminator = "";
	arms: Arm[] = [];
	// nullable
	inner: Type | null = null;
	// opaque
	consumerCheck = "";
	hasConsumerCheck = false;
	unchecked = false;
	hasUnchecked = false;
	uncheckedReason = "";
	hasReason = false;
	// constraints
	constraints: Constraint[] = [];
	// location within the schema document
	schemaPath: Path = diag.newPath();
}

export class Import {
	file = "";
	types: string[] = [];
}

export class Scalar {
	name = "";
	base = "";
	lexemeRule = "";
	lenMin: number | null = null;
	lenMax: number | null = null;
	nonEmpty = false;
}

export class Schema {
	name = "";
	metaVersion = 0;
	hasMetaVersion = false;
	metaVersionKind: Kind = Kind.Null;
	formatVersion = 0;
	hasFormatVersion = false;
	formatVersionKind: Kind = Kind.Null;
	documentSyntax = "";
	role = "";
	description = "";
	root = "";
	targets: string[] = [];
	safeIntegers = false;
	imports: Import[] = [];
	types = new Map<string, Type>();
	typeOrder: string[] = [];
	dir = "";

	lookupType(name: string): Type | undefined {
		return this.types.get(name);
	}
}

const COMPLEX_KINDS: Record<string, SKind> = {
	record: SKind.Record,
	map: SKind.Map,
	array: SKind.Array,
	tuple: SKind.Tuple,
	enum: SKind.Enum,
	literal: SKind.Literal,
	"discriminated-union": SKind.DiscriminatedUnion,
	"node-kind-union": SKind.NodeKindUnion,
	nullable: SKind.Nullable,
	opaque: SKind.Opaque,
};

class Reader {
	diags = new diag.Diagnostics();
	isType = false;
	fileName = "";
}

export function readSchema(
	root: Node | null,
	directory: string,
): [Schema, Diagnostic[]] {
	const s = new Schema();
	s.dir = directory;
	const r = new Reader();
	if (root === null || root.kind !== Kind.Record) {
		return [s, r.diags.all()];
	}
	parseHeader(s, root);
	r.isType = s.role === "type-definitions";
	r.fileName = `${s.name}.toml`;

	if (!s.hasMetaVersion) {
		r.diags.emitCode("STRICTSPEC_SCHEMA_MISSING_META_VERSION", diag.newPath(), {
			schema: diag.slotIdentifier(s.name),
		});
	}
	if (s.role === "schema" && !s.hasFormatVersion) {
		r.diags.emitCode(
			"STRICTSPEC_SCHEMA_MISSING_FORMAT_VERSION",
			diag.newPath(),
			{
				schema: diag.slotIdentifier(s.name),
			},
		);
	}
	if (r.isType && s.imports.length > 0) {
		r.diags.emitCode("STRICTSPEC_IMPORT_TRANSITIVE", diag.newPath(), {
			file: diag.slotString(r.fileName),
		});
	}

	const typesNode = entryOf(root, "types");
	if (typesNode !== null && typesNode.kind === Kind.Record) {
		for (const e of typesNode.entries) {
			const name = e.key;
			const sp = diag.newPath(diag.stepKey("types"), diag.stepKey(name));
			const t = parseType(r, e.value, sp);
			s.types.set(name, t);
			s.typeOrder.push(name);
			if (r.isType && hasAnyConstraint(t)) {
				r.diags.emitCode(
					"STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
					diag.newPath(),
					{
						file: diag.slotString(r.fileName),
					},
				);
				r.isType = false; // emit once
			}
		}
	}
	return [s, r.diags.all()];
}

function parseHeader(s: Schema, root: Node): void {
	for (const e of root.entries) {
		const k = e.key;
		const v = e.value;
		if (k === "name") {
			s.name = decodeStr(v);
		} else if (k === "meta_version") {
			s.hasMetaVersion = true;
			s.metaVersionKind = v.kind;
			const iv = intOf(v);
			if (iv !== null) {
				s.metaVersion = iv;
			}
		} else if (k === "format_version") {
			s.hasFormatVersion = true;
			s.formatVersionKind = v.kind;
			const iv = intOf(v);
			if (iv !== null) {
				s.formatVersion = iv;
			}
		} else if (k === "document_syntax") {
			s.documentSyntax = decodeStr(v);
		} else if (k === "role") {
			s.role = decodeStr(v);
		} else if (k === "description") {
			s.description = decodeStr(v);
		} else if (k === "root") {
			s.root = decodeStr(v);
		} else if (k === "safe_integers") {
			s.safeIntegers = v.kind === Kind.Bool && v.lexeme === "true";
		} else if (k === "targets") {
			for (const it of items(v)) {
				s.targets.push(decodeStr(it));
			}
		} else if (k === "imports") {
			for (const it of items(v)) {
				const imp = new Import();
				imp.file = decodeStr(child(it, "file"));
				for (const t of items(child(it, "types"))) {
					imp.types.push(decodeStr(t));
				}
				s.imports.push(imp);
			}
		}
	}
}

function parseType(r: Reader, node: Node | null, sp: Path): Type {
	const t = new Type();
	t.schemaPath = sp;
	if (node === null || node.kind !== Kind.Record) {
		return t;
	}
	const [typeName, hasType] = strEntry(node, "type");

	if (hasType) {
		const ck = COMPLEX_KINDS[typeName];
		if (ck !== undefined) {
			t.kind = ck;
		} else {
			t.kind = SKind.Ref;
			t.ref = typeName;
		}
	} else {
		t.kind = inferKind(node);
	}

	parseRefinements(t, node);

	if (t.kind === SKind.Record) {
		parseFields(r, t, node, sp);
	} else if (t.kind === SKind.Map) {
		const [kp, okKp] = strEntry(node, "key_pattern");
		if (okKp) {
			t.keyPattern = kp;
		}
		const [o, okO] = strEntry(node, "order");
		if (okO) {
			t.order = o;
		}
		const v = entryOf(node, "value");
		if (v !== null) {
			t.value = parseType(r, v, appendKey(sp, "value"));
		}
	} else if (t.kind === SKind.Array) {
		let v = entryOf(node, "min_len");
		if (v !== null) {
			t.minLen = intPtr(v);
		}
		v = entryOf(node, "max_len");
		if (v !== null) {
			t.maxLen = intPtr(v);
		}
		const it = entryOf(node, "item");
		if (it !== null) {
			t.item = parseType(r, it, appendKey(sp, "item"));
		}
	} else if (t.kind === SKind.Tuple) {
		for (const el of items(child(node, "elements"))) {
			t.elements.push(decodeStr(el));
		}
	} else if (t.kind === SKind.Enum) {
		const vs = entryOf(node, "values");
		if (vs !== null) {
			for (const v of items(vs)) {
				t.enumValues.push(svalFromNode(v));
			}
		}
		let srcNode: Node | null = null;
		const src = entryOf(node, "source");
		if (src !== null && src.kind === Kind.Record) {
			t.sourced = true;
			srcNode = src;
			t.sourceDoc = strOr(src, "document");
			t.sourceSel = strOr(src, "selector");
		}
		const b = entryOf(node, "baked");
		if (b !== null) {
			t.sourced = true;
			for (const v of items(b)) {
				t.baked.push(decodeStr(v));
			}
		} else if (srcNode !== null) {
			const b2 = entryOf(srcNode, "baked");
			if (b2 !== null) {
				for (const v of items(b2)) {
					t.baked.push(decodeStr(v));
				}
			}
		}
	} else if (t.kind === SKind.Literal) {
		const v = entryOf(node, "value");
		if (v !== null) {
			t.literal = svalFromNode(v);
		}
	} else if (
		t.kind === SKind.DiscriminatedUnion ||
		t.kind === SKind.NodeKindUnion
	) {
		const [d, okD] = strEntry(node, "discriminator");
		if (okD) {
			t.discriminator = d;
		}
		const arms = entryOf(node, "arms");
		if (arms !== null && arms.kind === Kind.Record) {
			for (const e of arms.entries) {
				const armSp = appendKey(appendKey(sp, "arms"), e.key);
				t.arms.push(new Arm(e.key, parseType(r, e.value, armSp)));
			}
		}
	} else if (t.kind === SKind.Nullable) {
		const inn = entryOf(node, "inner");
		if (inn !== null) {
			t.inner = parseType(r, inn, appendKey(sp, "inner"));
		}
	} else if (t.kind === SKind.Opaque) {
		const [cc, okCc] = strEntry(node, "consumer_check");
		if (okCc) {
			t.consumerCheck = cc;
			t.hasConsumerCheck = true;
		}
		const u = entryOf(node, "unchecked");
		if (u !== null) {
			t.hasUnchecked = true;
			t.unchecked = u.kind === Kind.Bool && u.lexeme === "true";
		}
		const [ur, okUr] = strEntry(node, "unchecked_reason");
		if (okUr) {
			t.uncheckedReason = ur;
			t.hasReason = true;
		}
		checkOpaqueStance(r, t);
	}

	parseConstraints(t, node);
	return t;
}

function checkOpaqueStance(r: Reader, t: Type): void {
	if (t.hasConsumerCheck) {
		return;
	}
	if (t.hasUnchecked && t.unchecked) {
		if (!t.hasReason) {
			r.diags.emitCode(
				"STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON",
				t.schemaPath,
				null,
			);
		}
		return;
	}
	r.diags.emitCode("STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE", t.schemaPath, null);
}

function parseFields(r: Reader, t: Type, node: Node, sp: Path): void {
	const fnode = entryOf(node, "fields");
	if (fnode === null || fnode.kind !== Kind.Record) {
		return;
	}
	for (const e of fnode.entries) {
		const fsp = appendKey(appendKey(sp, "fields"), e.key);
		const ft = parseType(r, e.value, fsp);
		const f = new Field(e.key, ft);
		const req = entryOf(e.value, "required");
		if (req !== null) {
			f.required = req.kind === Kind.Bool && req.lexeme === "true";
		}
		for (const a of items(child(e.value, "aliases"))) {
			f.aliases.push(decodeStr(a));
		}
		t.fields.push(f);
	}
}

function parseRefinements(t: Type, node: Node): void {
	const boundKeys: Array<
		["min" | "max" | "exclusiveMin" | "exclusiveMax", string]
	> = [
		["min", "min"],
		["max", "max"],
		["exclusiveMin", "exclusive_min"],
		["exclusiveMax", "exclusive_max"],
	];
	for (const [attr, key] of boundKeys) {
		const v = entryOf(node, key);
		if (v !== null) {
			t[attr] = svalFromNode(v);
		}
	}
	let v = entryOf(node, "min_length");
	if (v !== null) {
		t.minLength = intPtr(v);
	}
	v = entryOf(node, "max_length");
	if (v !== null) {
		t.maxLength = intPtr(v);
	}
	v = entryOf(node, "non_empty");
	if (v !== null) {
		t.nonEmpty = v.kind === Kind.Bool && v.lexeme === "true";
	}
	const [rg, okRg] = strEntry(node, "regex");
	if (okRg) {
		t.regex = rg;
		t.hasRegex = true;
	}
	const [dk, okDk] = strEntry(node, "datetime_kind");
	if (okDk) {
		t.datetimeKind = dk;
	}
}

function parseConstraints(t: Type, node: Node): void {
	const cnode = entryOf(node, "constraints");
	if (cnode === null) {
		return;
	}
	for (const c of items(cnode)) {
		if (c.kind !== Kind.Record) {
			continue;
		}
		const con = new Constraint();
		con.form = strOr(c, "form");
		con.field = strOr(c, "field");
		con.left = strOr(c, "left");
		con.right = strOr(c, "right");
		con.collection = strOr(c, "collection");
		con.uniqField = strOr(c, "field");
		con.normalization = strOr(c, "normalization");
		con.start = strOr(c, "start");
		con.length = strOr(c, "length");
		con.less = strOr(c, "less");
		con.than = strOr(c, "than");
		con.reference = strOr(c, "reference");
		con.resolvesInto = strOr(c, "resolves_into");
		con.resolvesBy = strOr(c, "resolves_by");
		con.source = strOr(c, "source");
		con.selection = strOr(c, "selection");
		con.compare = strOr(c, "compare");
		con.sumField = strOr(c, "sum_field");
		const lim = entryOf(c, "limit");
		if (lim !== null) {
			con.limit = svalFromNode(lim);
			con.hasLimit = true;
		}
		const el = entryOf(c, "equals_literal");
		if (el !== null) {
			con.equalsLiteral = svalFromNode(el);
			con.hasEquals = true;
		}
		for (const f of items(child(c, "fields"))) {
			con.fields.push(decodeStr(f));
		}
		const w = entryOf(c, "when");
		if (w !== null && w.kind === Kind.Record) {
			con.when = parseCondition(w);
		}
		t.constraints.push(con);
	}
}

function parseCondition(w: Node): Condition {
	const c = new Condition();
	c.field = strOr(w, "field");
	c.predicate = strOr(w, "predicate");
	const v = entryOf(w, "value");
	if (v !== null) {
		c.value = svalFromNode(v);
		c.hasValue = true;
	}
	for (const vv of items(child(w, "values"))) {
		c.values.push(svalFromNode(vv));
	}
	return c;
}

function inferKind(node: Node): SKind {
	if (entryOf(node, "fields") !== null) {
		return SKind.Record;
	}
	if (entryOf(node, "arms") !== null) {
		return SKind.DiscriminatedUnion;
	}
	if (entryOf(node, "item") !== null) {
		return SKind.Array;
	}
	if (entryOf(node, "value") !== null) {
		return SKind.Map;
	}
	if (entryOf(node, "inner") !== null) {
		return SKind.Nullable;
	}
	if (entryOf(node, "elements") !== null) {
		return SKind.Tuple;
	}
	return SKind.Record;
}

function hasAnyConstraint(t: Type | null): boolean {
	if (t === null) {
		return false;
	}
	if (t.constraints.length > 0) {
		return true;
	}
	for (const f of t.fields) {
		if (hasAnyConstraint(f.type)) {
			return true;
		}
	}
	for (const a of t.arms) {
		if (hasAnyConstraint(a.type)) {
			return true;
		}
	}
	if (
		hasAnyConstraint(t.item) ||
		hasAnyConstraint(t.value) ||
		hasAnyConstraint(t.inner)
	) {
		return true;
	}
	return false;
}

// --- small node accessors ----------------------------------------------------

export function entryOf(rec: Node | null, key: string): Node | null {
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

function child(rec: Node | null, key: string): Node | null {
	return entryOf(rec, key);
}

export function strEntry(rec: Node | null, key: string): [string, boolean] {
	const n = entryOf(rec, key);
	if (n === null || n.kind !== Kind.String) {
		return ["", false];
	}
	return [decodeTOML(n.lexeme), true];
}

function strOr(rec: Node | null, key: string): string {
	return strEntry(rec, key)[0];
}

function decodeStr(n: Node | null): string {
	if (n === null || n.kind !== Kind.String) {
		return "";
	}
	return decodeTOML(n.lexeme);
}

function intOf(n: Node | null): number | null {
	if (n === null || n.kind !== Kind.Integer) {
		return null;
	}
	const v = goParseInt(n.lexeme);
	return v === null ? null : Number(v);
}

function intPtr(n: Node | null): number | null {
	return intOf(n);
}

function items(n: Node | null): Node[] {
	if (n === null || n.kind !== Kind.Array) {
		return [];
	}
	return [...n.items];
}

function appendKey(p: Path, key: string): Path {
	return new Path([...p.steps, diag.stepKey(key)], p.anchor);
}

// --- numeric parsing mirroring Go strconv ------------------------------------

// Mirror Go strconv.ParseInt(s, 10, 64): base-10, no underscores, int64 range.
// Returns null on failure. Uses bigint internally for exact int64 range checks;
// integer FIELDS bind plain number elsewhere (per ts/DESIGN under safe_integers),
// but literal comparison and range checks need exactness beyond 2^53.
export function goParseInt(lexeme: string): bigint | null {
	let s = lexeme;
	if (s === "") {
		return null;
	}
	let neg = false;
	if (s[0] === "+" || s[0] === "-") {
		neg = s[0] === "-";
		s = s.slice(1);
	}
	if (s === "" || !/^[0-9]+$/.test(s)) {
		return null;
	}
	let v = BigInt(s);
	if (neg) {
		v = -v;
	}
	if (v < -(2n ** 63n) || v > 2n ** 63n - 1n) {
		return null;
	}
	return v;
}

// Mirror Go strconv.ParseFloat(s, 64) closely enough for the lexemes the lossless
// parsers actually produce (decimal floats, inf/nan, underscores between digits
// in TOML). Returns null on failure.
export function goParseFloat(lexeme: string): number | null {
	const s = lexeme.trim();
	const low = s.toLowerCase();
	if (
		low === "inf" ||
		low === "+inf" ||
		low === "infinity" ||
		low === "+infinity"
	) {
		return Number.POSITIVE_INFINITY;
	}
	if (low === "-inf" || low === "-infinity") {
		return Number.NEGATIVE_INFINITY;
	}
	if (low === "nan" || low === "+nan" || low === "-nan") {
		return Number.NaN;
	}
	if (s === "") {
		return null;
	}
	const stripped = s.replace(/_/g, "");
	const n = Number(stripped);
	if (Number.isNaN(n)) {
		return null;
	}
	return n;
}

// --- in-memory FileSet loading -----------------------------------------------

export type FileSet = Record<string, string>;

export function parseFrom(
	files: FileSet,
	name: string,
): [Schema, Diagnostic[]] {
	const src = files[name];
	if (src === undefined) {
		throw new Error(
			`strictspec: embedded file ${JSON.stringify(name)} not found`,
		);
	}
	const d = tomldoc.parse(src);
	return readSchema(d.root, "");
}

export function resolveImportsFrom(s: Schema, files: FileSet): Diagnostic[] {
	const out: Diagnostic[] = [];
	for (const imp of s.imports) {
		let ts: Schema;
		let tdiags: Diagnostic[];
		try {
			[ts, tdiags] = parseFrom(files, imp.file);
		} catch {
			out.push(
				diag.newDiagnostic(
					"STRICTSPEC_IMPORT_MISSING_TYPE_FILE",
					diag.newPath(),
					{
						file: diag.slotString(imp.file),
						schema: diag.slotIdentifier(s.name),
					},
				),
			);
			continue;
		}
		out.push(...tdiags);
		for (const name of imp.types) {
			if (!ts.types.has(name)) {
				out.push(
					diag.newDiagnostic("STRICTSPEC_IMPORT_UNKNOWN_TYPE", diag.newPath(), {
						name: diag.slotIdentifier(name),
						file: diag.slotString(imp.file),
					}),
				);
			}
		}
		for (const [name, t] of ts.types) {
			if (!s.types.has(name)) {
				s.types.set(name, t);
			}
		}
	}
	return out;
}

export function loadManifestScalarsFrom(files: FileSet): Map<string, Scalar> {
	const out = new Map<string, Scalar>();
	for (const src of Object.values(files)) {
		let d: doc.Document;
		try {
			d = tomldoc.parse(src);
		} catch {
			continue;
		}
		mergeScalars(out, d.root);
	}
	return out;
}

export function mergeScalars(out: Map<string, Scalar>, root: Node): void {
	const sc = entryOf(root, "scalars");
	if (sc === null) {
		return;
	}
	for (const s of items(sc)) {
		const cs = new Scalar();
		cs.name = strOr(s, "name");
		cs.base = strOr(s, "base");
		cs.lexemeRule = strOr(s, "lexeme_rule");
		const length = entryOf(s, "length");
		if (length !== null) {
			const mn = entryOf(length, "min");
			if (mn !== null) {
				cs.lenMin = intPtr(mn);
			}
			const mx = entryOf(length, "max");
			if (mx !== null) {
				cs.lenMax = intPtr(mx);
			}
			const ne = entryOf(length, "non_empty");
			if (ne !== null) {
				cs.nonEmpty = ne.lexeme === "true";
			}
		}
		if (cs.name !== "") {
			out.set(cs.name, cs);
		}
	}
}
