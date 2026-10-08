// The options built-ins (.strictmetadata/docs/appendix-options.md).
//
// Three toolchain-shipped built-in schemas -- options-entries (a subject
// document under .strictmetadata/options/), options-registry (a tool's registry
// of the options it offers), and upstream (.strictmetadata/upstream/upstream.toml)
// -- plus the readers that validate a document's SHAPE against them and bind it
// to typed values. Shape diagnostics are ordinary catalogued STRICTSPEC_*
// diagnostics from the shared executor, identical across the Go, Python, and
// TypeScript runtimes. The readers take document text: this module uses no
// Node-specific API (ts/DESIGN.md), so reading the files is the caller's job.
//
// The rules beyond shape (the ranking parser, the registry rules, the
// per-namespace entry validator, and the classification) follow the readers.
// Every refusal is a catalogued STRICTSPEC_OPTIONS_* diagnostic
// (appendix-error-codes.md, section 21a) rendered from its pinned template. The
// same rules, over the same shared test cases, exist in the Go and Python
// runtimes.

import {
	OPTIONS_ENTRIES_SCHEMA,
	OPTIONS_REGISTRY_SCHEMA,
	UPSTREAM_SCHEMA,
} from "./builtins.generated.js";
import * as diag from "./diag.js";
import {
	compileFromSource,
	type Diagnostic,
	loadValue,
	type Program,
	type Value,
} from "./index.js";
import { render, renderValue } from "./render.js";

export { OPTIONS_ENTRIES_SCHEMA, OPTIONS_REGISTRY_SCHEMA, UPSTREAM_SCHEMA };

// Where a repository's option subject documents live, relative to its root.
export const OPTIONS_DIR = ".strictmetadata/options";
// Where a fork declares its upstream, relative to the repository root.
export const UPSTREAM_FILE = ".strictmetadata/upstream/upstream.toml";
// The options directory's ownership manifest; not a subject document.
const OPTIONS_MANIFEST_FILE = "manifest.toml";

const programs = new Map<string, Program>();

function builtin(fileName: string, src: string): Program {
	let p = programs.get(fileName);
	if (p === undefined) {
		try {
			p = compileFromSource({ [fileName]: src }, fileName);
		} catch (e) {
			// The built-ins are part of this runtime; one failing the meta-schema
			// is a strictspec bug, never a consumer condition.
			throw new Error(
				`strictspec: built-in schema ${fileName} fails the meta-schema: ${(e as Error).message}`,
			);
		}
		programs.set(fileName, p);
	}
	return p;
}

// The compiled built-in options-entries schema.
export function optionsEntriesProgram(): Program {
	return builtin("options-entries.schema.toml", OPTIONS_ENTRIES_SCHEMA);
}

// The compiled built-in options-registry schema.
export function optionsRegistryProgram(): Program {
	return builtin("options-registry.schema.toml", OPTIONS_REGISTRY_SCHEMA);
}

// The compiled built-in upstream schema.
export function upstreamProgram(): Program {
	return builtin("upstream.schema.toml", UPSTREAM_SCHEMA);
}

// One [[option]] of a tool's options registry. requires names the other
// options of the same registry the option depends on.
export interface OptionDeclaration {
	readonly name: string;
	readonly subject: string;
	readonly values: string;
	readonly default: string;
	readonly scope: string;
	readonly requires: readonly string[];
	readonly description: string;
}

// A tool's shape-valid options registry.
export interface OptionsRegistry {
	readonly options: readonly OptionDeclaration[];
}

// One [[entry]] of a subject document. file is the subject document's file
// name (for example "changelog.toml") and index the entry's position in it;
// scope is null when the entry carries none.
export interface OptionsEntry {
	readonly file: string;
	readonly index: number;
	readonly id: string;
	readonly scope: string | null;
	readonly current: string;
	readonly ideal: string;
	readonly reason: string;
}

// A fork's shape-valid upstream declaration.
export interface Upstream {
	readonly host: string;
	readonly owner: string;
	readonly repo: string;
	readonly branch: string;
}

function validateToml(p: Program, text: string): [Value | null, Diagnostic[]] {
	const res = p.validate(text, "toml");
	if (!res.valid) {
		return [null, [...res.diagnostics]];
	}
	return [loadValue(text, "toml"), []];
}

function str(rec: Value, key: string): string | null {
	const [f, ok] = rec.field(key);
	if (!ok) {
		return null;
	}
	return f.string()[0];
}

// Validate a registry document's shape against the built-in options-registry
// schema and bind it. Non-empty diagnostics mean a null registry.
export function readOptionsRegistry(
	text: string,
): [OptionsRegistry | null, Diagnostic[]] {
	const [root, diags] = validateToml(optionsRegistryProgram(), text);
	if (root === null) {
		return [null, diags];
	}
	const options = root
		.field("option")[0]
		.items()
		.map((o) => ({
			name: str(o, "name") as string,
			subject: str(o, "subject") as string,
			values: str(o, "values") as string,
			default: str(o, "default") as string,
			scope: str(o, "scope") as string,
			requires: o
				.field("requires")[0]
				.items()
				.map((r) => r.string()[0] as string),
			description: str(o, "description") as string,
		}));
	return [{ options }, []];
}

// Validate one subject document's shape against the built-in options-entries
// schema and bind its entries, attributing each to file (the document's file
// name, for example "changelog.toml"). Non-empty diagnostics mean no entries.
export function readOptionsEntries(
	file: string,
	text: string,
): [OptionsEntry[], Diagnostic[]] {
	const [root, diags] = validateToml(optionsEntriesProgram(), text);
	if (root === null) {
		return [[], diags];
	}
	const entries = root
		.field("entry")[0]
		.items()
		.map((e, index) => ({
			file,
			index,
			id: str(e, "id") as string,
			scope: str(e, "scope"),
			current: str(e, "current") as string,
			ideal: str(e, "ideal") as string,
			reason: str(e, "reason") as string,
		}));
	return [entries, []];
}

// Validate an upstream document's shape against the built-in upstream schema
// and bind it. Non-empty diagnostics mean a null upstream.
export function readUpstream(text: string): [Upstream | null, Diagnostic[]] {
	const [root, diags] = validateToml(upstreamProgram(), text);
	if (root === null) {
		return [null, diags];
	}
	return [
		{
			host: str(root, "host") as string,
			owner: str(root, "owner") as string,
			repo: str(root, "repo") as string,
			branch: str(root, "branch") as string,
		},
		[],
	];
}

// --- the rules beyond shape ------------------------------------------------

// The reserved ideal value meaning "the right value is one the tool does not
// offer yet". A ranking never declares it.
export const OPTIONS_NON_EXISTENT = "non-existent";
const SCOPE_NONE = "none";
// The one value that switches an option off: an entry setting an option's
// current or ideal to it requires every dependent of the option to be off too.
// No other value affects dependents.
const OFF = "off";
const VALUE_NAME = /^[a-z0-9-]+$/;

// A parsed ranking string. values lists the declared values in the order
// written; level maps each to its rank, 0 being the strongest, with values of
// equal rank sharing a level.
export interface OptionsRanking {
	readonly values: readonly string[];
	readonly level: ReadonlyMap<string, number>;
}

function publicDiagnostics(ds: readonly diag.Diagnostic[]): Diagnostic[] {
	return ds.map((d) => ({
		code: d.code,
		path: d.path.render(),
		message: render(d),
	}));
}

function strVal(s: string): diag.Slot {
	return diag.slotValue(diag.stringVal(s));
}

function parseRanking(
	s: string,
	at: diag.Path,
): [OptionsRanking | null, diag.Diagnostic[]] {
	const toks = s.split(" ");
	const malformed = [
		diag.newDiagnostic("STRICTSPEC_OPTIONS_RANKING_MALFORMED", at, {
			ranking: strVal(s),
		}),
	];
	if (toks.length % 2 === 0) {
		return [null, malformed];
	}
	for (let i = 0; i < toks.length; i++) {
		const t = toks[i] as string;
		const isOp = t === ">" || t === "=";
		if (t === "" || (i % 2 === 1) !== isOp) {
			return [null, malformed];
		}
	}
	const values: string[] = [];
	const level = new Map<string, number>();
	const ds: diag.Diagnostic[] = [];
	const refuse = (code: string, v: string): void => {
		ds.push(diag.newDiagnostic(code, at, { value: strVal(v) }));
	};
	let lv = 0;
	for (let i = 0; i < toks.length; i += 2) {
		if (i > 0 && toks[i - 1] === ">") {
			lv++;
		}
		const v = toks[i] as string;
		if (!VALUE_NAME.test(v)) {
			refuse("STRICTSPEC_OPTIONS_RANKING_VALUE_NAME", v);
		} else if (v === OPTIONS_NON_EXISTENT) {
			refuse("STRICTSPEC_OPTIONS_RANKING_RESERVED", v);
		} else if (level.has(v)) {
			refuse("STRICTSPEC_OPTIONS_RANKING_DUPLICATE", v);
		} else {
			values.push(v);
			level.set(v, lv);
		}
	}
	if (ds.length > 0) {
		return [null, ds];
	}
	return [{ values, level }, []];
}

// Parse a ranking string: value names separated by single spaces around `>`
// (stronger than) or `=` (equal rank), for example "npm = pypi = jsr > none". A
// string that is not that alternation is one
// STRICTSPEC_OPTIONS_RANKING_MALFORMED diagnostic; otherwise every value name
// outside the grammar, every use of the reserved non-existent, and every
// repeated value is refused. The diagnostics' path is "$". Non-empty
// diagnostics mean a null ranking.
export function parseOptionsRanking(
	s: string,
): [OptionsRanking | null, Diagnostic[]] {
	const [rk, ds] = parseRanking(s, diag.newPath());
	return [rk, publicDiagnostics(ds)];
}

// A registry option that passed every registry rule, with its parsed ranking.
export interface CheckedOption {
	readonly declaration: OptionDeclaration;
	readonly ranking: OptionsRanking;
}

// Set by CheckedOptionsRegistry's static block, so validateOptionsRegistry can
// call the private constructor and nothing outside this module can.
let makeCheckedRegistry: (
	options: ReadonlyMap<string, CheckedOption>,
	dependents: ReadonlyMap<string, readonly string[]>,
) => CheckedOptionsRegistry;

// Set by the same static block, so validateOptionsNamespace can read a
// checked registry's dependents and nothing outside this module can.
let dependentsOf: (
	reg: CheckedOptionsRegistry,
	name: string,
) => readonly string[];

// A registry that passed every registry rule. Only validateOptionsRegistry
// makes one.
export class CheckedOptionsRegistry {
	readonly #options: ReadonlyMap<string, CheckedOption>;
	// Each option's dependents: the options that require it, directly or
	// through other options, in declaration order.
	readonly #dependents: ReadonlyMap<string, readonly string[]>;

	private constructor(
		options: ReadonlyMap<string, CheckedOption>,
		dependents: ReadonlyMap<string, readonly string[]>,
	) {
		this.#options = options;
		this.#dependents = dependents;
	}

	static {
		makeCheckedRegistry = (options, dependents) =>
			new CheckedOptionsRegistry(options, dependents);
		dependentsOf = (reg, name) => reg.#dependents.get(name) ?? [];
	}

	// The checked option named name (the tool's own name for it, without the
	// tool prefix), or undefined.
	option(name: string): CheckedOption | undefined {
		return this.#options.get(name);
	}

	// The registry's option names in declaration order.
	names(): string[] {
		return [...this.#options.keys()];
	}
}

// A registry subject names a subject file the entry loader reads: a
// value-name-grammar stem that is not the options directory's own manifest.
function validSubject(s: string): boolean {
	return VALUE_NAME.test(s) && `${s}.toml` !== OPTIONS_MANIFEST_FILE;
}

// The {dependents} / {options} slot text: each name rendered as a value slot
// renders it, joined by ", ", never truncated as a whole.
function renderedList(names: readonly string[]): string {
	return names.map((n) => renderValue(diag.stringVal(n))).join(", ");
}

// The options reachable from name through requires, following only declared
// names other than the option itself.
function reachFrom(
	name: string,
	requires: ReadonlyMap<string, readonly string[]>,
): Set<string> {
	const seen = new Set<string>();
	const stack = [name];
	while (stack.length > 0) {
		const n = stack.pop() as string;
		for (const r of requires.get(n) ?? []) {
			if (!seen.has(r)) {
				seen.add(r);
				stack.push(r);
			}
		}
	}
	return seen;
}

// Apply the registry rules to a registry: each option's values parse as a
// ranking, its default is a declared value, its subject is a valid subject file
// stem, and its requires name only other options the registry declares, with
// no cycle among them. Option names are unique by the built-in schema, and so
// are the names in one option's requires; a registry handed over directly with
// a repeat draws the same STRICTSPEC_INTRA_UNIQUE_BY or
// STRICTSPEC_INTRA_PAIRWISE_DISTINCT diagnostic the shape reader reports. Paths
// locate the refused field in the registry document. Non-empty diagnostics mean
// a null registry.
export function validateOptionsRegistry(
	reg: OptionsRegistry,
): [CheckedOptionsRegistry | null, Diagnostic[]] {
	const out = new Map<string, CheckedOption>();
	const ds: diag.Diagnostic[] = [];
	const declared = new Map<string, number>();
	reg.options.forEach((o, index) => {
		if (!declared.has(o.name)) {
			declared.set(o.name, index);
		}
	});
	// Per first-declared option, the declared names it requires other than
	// itself: the edges the cycle rule and the dependents follow.
	const requires = new Map<string, string[]>();
	reg.options.forEach((o, index) => {
		const at = diag.newPath(diag.stepKey("option"), diag.stepIndex(index));
		if (out.has(o.name)) {
			ds.push(
				diag.newDiagnostic(
					"STRICTSPEC_INTRA_UNIQUE_BY",
					diag.newPath(diag.stepKey("option")),
					{
						value: strVal(o.name),
						field: diag.slotString("name"),
						normalization: diag.slotString("none"),
					},
				),
			);
		}
		const [rk, rds] = parseRanking(o.values, diag.appendKey(at, "values"));
		ds.push(...rds);
		if (rk !== null && !rk.level.has(o.default)) {
			ds.push(
				diag.newDiagnostic(
					"STRICTSPEC_OPTIONS_DEFAULT_UNDECLARED",
					diag.appendKey(at, "default"),
					{ value: strVal(o.default), ranking: strVal(o.values) },
				),
			);
		}
		if (!validSubject(o.subject)) {
			ds.push(
				diag.newDiagnostic(
					"STRICTSPEC_OPTIONS_SUBJECT_INVALID",
					diag.appendKey(at, "subject"),
					{ value: strVal(o.subject) },
				),
			);
		}
		const reqAt = diag.appendKey(at, "requires");
		const seen = new Set<string>();
		for (const r of o.requires) {
			if (seen.has(r)) {
				ds.push(
					diag.newDiagnostic("STRICTSPEC_INTRA_PAIRWISE_DISTINCT", reqAt, {
						value: strVal(r),
						normalization: diag.slotString("none"),
					}),
				);
				break;
			}
			seen.add(r);
		}
		const candidates = reg.options
			.map((n) => n.name)
			.filter((n) => n !== o.name);
		const edges: string[] = [];
		o.requires.forEach((r, j) => {
			const rAt = diag.appendIndex(reqAt, j);
			if (r === o.name) {
				ds.push(
					diag.newDiagnostic("STRICTSPEC_OPTIONS_REQUIRES_SELF", rAt, {
						name: strVal(o.name),
					}),
				);
			} else if (!declared.has(r)) {
				ds.push(
					diag.newDiagnostic("STRICTSPEC_OPTIONS_REQUIRES_UNDECLARED", rAt, {
						name: strVal(o.name),
						value: strVal(r),
						suggestion: diag.slotSuggestion(r, candidates),
					}),
				);
			} else {
				edges.push(r);
			}
		});
		if (!requires.has(o.name)) {
			requires.set(o.name, edges);
			if (rk !== null) {
				out.set(o.name, { declaration: o, ranking: rk });
			}
		}
	});
	// A cycle is a set of two or more options each depending on all the
	// others, reported once, at the requires of its first-declared member.
	const names = [...requires.keys()];
	const reach = new Map(names.map((n) => [n, reachFrom(n, requires)]));
	const inCycle = new Set<string>();
	for (const n of names) {
		const fromN = reach.get(n) as Set<string>;
		if (inCycle.has(n) || !fromN.has(n)) {
			continue;
		}
		const members = names.filter(
			(m) => m === n || (fromN.has(m) && reach.get(m)?.has(n) === true),
		);
		for (const m of members) {
			inCycle.add(m);
		}
		ds.push(
			diag.newDiagnostic(
				"STRICTSPEC_OPTIONS_REQUIRES_CYCLE",
				diag.newPath(
					diag.stepKey("option"),
					diag.stepIndex(declared.get(n) as number),
					diag.stepKey("requires"),
				),
				{ options: diag.slotString(renderedList(members)) },
			),
		);
	}
	if (ds.length > 0) {
		return [null, publicDiagnostics(ds)];
	}
	const dependents = new Map(
		names.map((n) => [n, names.filter((m) => reach.get(m)?.has(n) === true)]),
	);
	return [makeCheckedRegistry(out, dependents), []];
}

// The ranking classification of an accepted entry: current and ideal have
// equal rank ("settled"), current ranks below ideal ("debt"), or ideal is
// non-existent, a value the tool does not offer yet ("waiting-on-tool").
export type OptionsClass = "settled" | "debt" | "waiting-on-tool";

// An accepted entry of the validated namespace with its classification.
export interface ClassifiedOptionsEntry {
	readonly entry: OptionsEntry;
	readonly class: OptionsClass;
}

// The repository-relative path of a subject document.
function optionsFile(name: string): string {
	return `${OPTIONS_DIR}/${name}`;
}

function entryPath(e: OptionsEntry): diag.Path {
	return diag.newPath(diag.stepKey("entry"), diag.stepIndex(e.index));
}

// Judge the entries of one tool's namespace (`<tool>:*`) against that tool's
// checked registry, in the order given; tool is the tool's name. Entries of
// other namespaces are not judged. Returns the accepted entries, classified,
// and a diagnostic for every refusal; an entry with any refusal is not
// classified. Each diagnostic's path locates the entry within its subject
// document, and its message names that document.
export function validateOptionsNamespace(
	tool: string,
	reg: CheckedOptionsRegistry,
	entries: readonly OptionsEntry[],
): [ClassifiedOptionsEntry[], Diagnostic[]] {
	const prefix = `${tool}:`;
	const candidates = reg.names().map((n) => prefix + n);
	const first = new Map<string, OptionsEntry>();
	const byId = new Map<string, OptionsEntry[]>();
	for (const e of entries) {
		const list = byId.get(e.id);
		if (list === undefined) {
			byId.set(e.id, [e]);
		} else {
			list.push(e);
		}
	}
	// The dependents of the option of entry e (declaration decl) that are not
	// off in the field (current or ideal) value reads.
	const missing = (
		e: OptionsEntry,
		decl: OptionDeclaration,
		value: (f: OptionsEntry) => string,
	): string[] => {
		const out: string[] = [];
		for (const dep of dependentsOf(reg, decl.name)) {
			const depDecl = (reg.option(dep) as CheckedOption).declaration;
			let off = false;
			let other = false;
			for (const f of byId.get(prefix + dep) ?? []) {
				if (value(f) !== OFF) {
					other = true;
				} else if (
					f.scope === null ||
					(e.scope !== null &&
						f.scope === e.scope &&
						depDecl.scope === decl.scope)
				) {
					off = true;
				}
			}
			if (!off && (depDecl.default !== OFF || other)) {
				out.push(prefix + dep);
			}
		}
		return out;
	};
	const accepted: ClassifiedOptionsEntry[] = [];
	const ds: diag.Diagnostic[] = [];
	for (const e of entries) {
		if (!e.id.startsWith(prefix)) {
			continue;
		}
		const before = ds.length;
		const at = entryPath(e);
		const refuse = (
			code: string,
			path: diag.Path,
			slots: Record<string, diag.Slot>,
		): void => {
			slots.file = diag.slotString(optionsFile(e.file));
			if (code !== "STRICTSPEC_OPTIONS_UNKNOWN_OPTION") {
				slots.id = strVal(e.id);
			}
			ds.push(diag.newDiagnostic(code, path, slots));
		};
		const key = JSON.stringify([e.id, e.scope]);
		const f = first.get(key);
		if (f !== undefined) {
			refuse("STRICTSPEC_OPTIONS_DUPLICATE_ENTRY", at, {
				first: diag.slotPath(entryPath(f)),
				first_file: diag.slotString(optionsFile(f.file)),
			});
		} else {
			first.set(key, e);
		}
		const opt = reg.option(e.id.slice(prefix.length));
		if (opt === undefined) {
			refuse("STRICTSPEC_OPTIONS_UNKNOWN_OPTION", diag.appendKey(at, "id"), {
				id: strVal(e.id),
				tool: diag.slotString(tool),
				suggestion: diag.slotSuggestion(e.id, candidates),
			});
		} else {
			const decl = opt.declaration;
			const level = opt.ranking.level;
			const want = `${decl.subject}.toml`;
			if (e.file !== want) {
				refuse("STRICTSPEC_OPTIONS_WRONG_SUBJECT", at, {
					subject: diag.slotString(optionsFile(want)),
				});
			}
			if (e.scope !== null && decl.scope === SCOPE_NONE) {
				refuse(
					"STRICTSPEC_OPTIONS_SCOPE_NOT_ACCEPTED",
					diag.appendKey(at, "scope"),
					{ value: strVal(e.scope) },
				);
			}
			const currentOk = level.has(e.current);
			if (!currentOk) {
				refuse(
					"STRICTSPEC_OPTIONS_UNDECLARED_CURRENT",
					diag.appendKey(at, "current"),
					{ value: strVal(e.current), ranking: strVal(decl.values) },
				);
			}
			const waiting = e.ideal === OPTIONS_NON_EXISTENT;
			const idealOk = waiting || level.has(e.ideal);
			if (!idealOk) {
				refuse(
					"STRICTSPEC_OPTIONS_UNDECLARED_IDEAL",
					diag.appendKey(at, "ideal"),
					{ value: strVal(e.ideal), ranking: strVal(decl.values) },
				);
			}
			if (currentOk && idealOk) {
				if (e.current === decl.default && e.ideal === decl.default) {
					refuse("STRICTSPEC_OPTIONS_REDUNDANT", at, {
						value: strVal(e.current),
					});
				} else if (
					!waiting &&
					(level.get(e.current) as number) < (level.get(e.ideal) as number)
				) {
					refuse("STRICTSPEC_OPTIONS_CURRENT_ABOVE_IDEAL", at, {
						current: strVal(e.current),
						ideal: strVal(e.ideal),
						ranking: strVal(decl.values),
					});
				}
			}
			if (e.current === OFF && currentOk) {
				const deps = missing(e, decl, (f) => f.current);
				if (deps.length > 0) {
					refuse(
						"STRICTSPEC_OPTIONS_DEPENDENTS_NOT_OFF",
						diag.appendKey(at, "current"),
						{ dependents: diag.slotString(renderedList(deps)) },
					);
				}
			}
			if (e.ideal === OFF && idealOk) {
				const deps = missing(e, decl, (f) => f.ideal);
				if (deps.length > 0) {
					refuse(
						"STRICTSPEC_OPTIONS_DEPENDENTS_IDEAL_NOT_OFF",
						diag.appendKey(at, "ideal"),
						{ dependents: diag.slotString(renderedList(deps)) },
					);
				}
			}
		}
		if (e.reason === "") {
			refuse(
				"STRICTSPEC_OPTIONS_EMPTY_REASON",
				diag.appendKey(at, "reason"),
				{},
			);
		}
		if (ds.length > before || opt === undefined) {
			continue;
		}
		const level = opt.ranking.level;
		let cls: OptionsClass = "debt";
		if (e.ideal === OPTIONS_NON_EXISTENT) {
			cls = "waiting-on-tool";
		} else if (level.get(e.current) === level.get(e.ideal)) {
			cls = "settled";
		}
		accepted.push({ entry: e, class: cls });
	}
	return [accepted, publicDiagnostics(ds)];
}
