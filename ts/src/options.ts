// The options built-ins (stricttools/docs/appendix-options.md).
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
// per-namespace entry validator, and the classification) are exported from this
// module for its tests but not re-exported from the package entry point:
// every refusal they report needs a catalogued STRICTSPEC_* code with a pinned
// message template, and appendix-error-codes.md has no area for them yet. The
// same rules, over the same shared test cases, exist in the Go and Python
// runtimes.

import {
	OPTIONS_ENTRIES_SCHEMA,
	OPTIONS_REGISTRY_SCHEMA,
	UPSTREAM_SCHEMA,
} from "./builtins.generated.js";
import {
	compileFromSource,
	type Diagnostic,
	loadValue,
	type Program,
	type Value,
} from "./index.js";

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

// One [[option]] of a tool's options registry.
export interface OptionDeclaration {
	readonly name: string;
	readonly subject: string;
	readonly values: string;
	readonly default: string;
	readonly scope: string;
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

// --- the rules beyond shape (not re-exported; see the header) ----------------

const NON_EXISTENT = "non-existent";
const SCOPE_NONE = "none";
const VALUE_NAME = /^[a-z0-9-]+$/;

// One refusal. For registry refusals file is "" and index is the [[option]]
// position; for entry refusals file and index locate the entry. value is the
// offending token or value; detail carries what the fix needs (the right
// subject file, the first occurrence of a duplicate, the ranking string).
export interface OptionsRefusal {
	readonly rule: string;
	readonly file: string;
	readonly index: number;
	readonly value: string;
	readonly detail: string;
}

function refusal(
	rule: string,
	value: string,
	fields: Partial<OptionsRefusal> = {},
): OptionsRefusal {
	return {
		rule,
		file: fields.file ?? "",
		index: fields.index ?? 0,
		value,
		detail: fields.detail ?? "",
	};
}

// A parsed ranking string. Level 0 is the strongest; values of equal rank
// share a level.
export interface OptionsRanking {
	readonly values: readonly string[];
	readonly level: ReadonlyMap<string, number>;
}

// Parse a ranking string: value names separated by single spaces around `>`
// (stronger than) or `=` (equal rank), for example "npm = pypi = jsr > none". A
// string that is not that alternation is one ranking-malformed refusal;
// otherwise every value name outside the grammar, every use of the reserved
// non-existent, and every repeated value is refused.
export function parseOptionsRanking(
	s: string,
): [OptionsRanking | null, OptionsRefusal[]] {
	const toks = s.split(" ");
	const malformed = [refusal("ranking-malformed", s)];
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
	const refusals: OptionsRefusal[] = [];
	let lv = 0;
	for (let i = 0; i < toks.length; i += 2) {
		if (i > 0 && toks[i - 1] === ">") {
			lv++;
		}
		const v = toks[i] as string;
		if (!VALUE_NAME.test(v)) {
			refusals.push(refusal("ranking-invalid-value-name", v));
		} else if (v === NON_EXISTENT) {
			refusals.push(refusal("ranking-reserved-value", v));
		} else if (level.has(v)) {
			refusals.push(refusal("ranking-duplicate-value", v));
		} else {
			values.push(v);
			level.set(v, lv);
		}
	}
	if (refusals.length > 0) {
		return [null, refusals];
	}
	return [{ values, level }, []];
}

// A registry option whose ranking parsed and whose default and subject are
// valid.
export interface CheckedOption {
	readonly decl: OptionDeclaration;
	readonly ranking: OptionsRanking;
}

// A registry subject names a subject file the entry loader reads: a
// value-name-grammar stem that is not the options directory's own manifest.
function validSubject(s: string): boolean {
	return VALUE_NAME.test(s) && `${s}.toml` !== OPTIONS_MANIFEST_FILE;
}

// Apply the registry rules to a shape-valid registry: each option's values
// parse as a ranking, its default is a declared value, and its subject is a
// valid subject file stem. (Option names are unique by the built-in schema's
// unique-by constraint.) Any refusal means a null registry.
export function checkOptionsRegistry(
	reg: OptionsRegistry,
): [Map<string, CheckedOption> | null, OptionsRefusal[]] {
	const out = new Map<string, CheckedOption>();
	const refusals: OptionsRefusal[] = [];
	reg.options.forEach((o, index) => {
		const [rk, rr] = parseOptionsRanking(o.values);
		for (const r of rr) {
			refusals.push(refusal(r.rule, r.value, { index, detail: o.values }));
		}
		if (rk !== null && !rk.level.has(o.default)) {
			refusals.push(
				refusal("registry-default-undeclared", o.default, {
					index,
					detail: o.values,
				}),
			);
		}
		if (!validSubject(o.subject)) {
			refusals.push(refusal("registry-subject-invalid", o.subject, { index }));
		}
		if (rk !== null) {
			out.set(o.name, { decl: o, ranking: rk });
		}
	});
	if (refusals.length > 0) {
		return [null, refusals];
	}
	return [out, []];
}

// An accepted entry of the validated namespace with its classification:
// "settled", "debt", or "waiting-on-tool".
export interface ClassifiedEntry {
	readonly entry: OptionsEntry;
	readonly class: string;
}

// Judge the entries of one tool's namespace (`<tool>:*`) against that tool's
// checked registry, in the order given. Entries of other namespaces are not
// judged. Returns the accepted entries, classified, and every refusal; an entry
// with any refusal is not classified.
export function validateOptionsNamespace(
	tool: string,
	reg: Map<string, CheckedOption>,
	entries: readonly OptionsEntry[],
): [ClassifiedEntry[], OptionsRefusal[]] {
	const prefix = `${tool}:`;
	const first = new Map<string, OptionsEntry>();
	const accepted: ClassifiedEntry[] = [];
	const refusals: OptionsRefusal[] = [];
	for (const e of entries) {
		if (!e.id.startsWith(prefix)) {
			continue;
		}
		const before = refusals.length;
		const refuse = (rule: string, value: string, detail = ""): void => {
			refusals.push(
				refusal(rule, value, { file: e.file, index: e.index, detail }),
			);
		};
		const key = JSON.stringify([e.id, e.scope]);
		const f = first.get(key);
		if (f !== undefined) {
			refuse("entry-duplicate", e.id, `${f.file} entry ${f.index}`);
		} else {
			first.set(key, e);
		}
		const opt = reg.get(e.id.slice(prefix.length));
		if (opt === undefined) {
			refuse("entry-unknown-option", e.id);
			continue;
		}
		const want = `${opt.decl.subject}.toml`;
		if (e.file !== want) {
			refuse("entry-wrong-subject", e.file, want);
		}
		if (e.scope !== null && opt.decl.scope === SCOPE_NONE) {
			refuse("entry-scope-not-accepted", e.scope);
		}
		const level = opt.ranking.level;
		const currentOk = level.has(e.current);
		if (!currentOk) {
			refuse("entry-undeclared-current", e.current, opt.decl.values);
		}
		const waiting = e.ideal === NON_EXISTENT;
		const idealOk = waiting || level.has(e.ideal);
		if (!idealOk) {
			refuse("entry-undeclared-ideal", e.ideal, opt.decl.values);
		}
		if (currentOk && idealOk) {
			if (e.current === opt.decl.default && e.ideal === opt.decl.default) {
				refuse("entry-redundant", e.current);
			} else if (
				!waiting &&
				(level.get(e.current) as number) < (level.get(e.ideal) as number)
			) {
				refuse("entry-current-above-ideal", e.current, e.ideal);
			}
		}
		if (refusals.length > before) {
			continue;
		}
		let cls = "debt";
		if (waiting) {
			cls = "waiting-on-tool";
		} else if (level.get(e.current) === level.get(e.ideal)) {
			cls = "settled";
		}
		accepted.push({ entry: e, class: cls });
	}
	return [accepted, refusals];
}
