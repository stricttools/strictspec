// Turn a structured Diagnostic into its pinned message text.
//
// A faithful port of go/internal/render (render.go + didyoumean.go) and python
// _render. It substitutes each template slot with the value rendering fixed by
// spec/appendix-rendering.md (Part A value rendering, Part B path grammar, Part C
// did-you-mean). Templates come from the generated codes catalogue; there is no
// hand-written message string here.
//
// Programmer-error policy: an unknown code, an unknown slot binding, or a missing
// required slot at render time all throw (the Go panic-equivalent). A Diagnostic
// is constructed by slot-correct emitter code; these conditions can only mean a
// bug, never malformed user input, so they fail loudly.

import * as codes from "./codes.generated.js";
import {
	codePointLength,
	codePointSlice,
	type Diagnostic,
	escapeString,
	isIdentShaped,
	type Slot,
	type Value,
} from "./diag.js";

export class RenderError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "RenderError";
	}
}

const PLACEHOLDER_RE = /\{(\w+)\}/g;

// Produce the pinned message text for a diagnostic.
export function render(d: Diagnostic): string {
	const entry = codes.lookup(d.code);
	if (entry === undefined) {
		throw new RenderError(
			`render: unknown code ${JSON.stringify(d.code)} (not in the catalogue)`,
		);
	}

	const placeholders = placeholderSet(entry.template);
	validateSlots(d, placeholders);

	return entry.template.replace(PLACEHOLDER_RE, (_m, name: string) => {
		if (name === "path") {
			return d.path.render();
		}
		if (name === "suggestion") {
			const slot = d.slots.suggestion;
			if (slot === undefined) {
				return "";
			}
			if (slot.t !== "suggestion") {
				throw new RenderError(
					`render: code ${d.code} slot 'suggestion' must be a suggestion slot`,
				);
			}
			return renderSuggestion(slot);
		}
		const slot = d.slots[name];
		if (slot === undefined) {
			throw new RenderError(
				`render: code ${d.code} missing required slot ${JSON.stringify(name)}`,
			);
		}
		return renderSlot(d.code, name, slot);
	});
}

function placeholderSet(template: string): Set<string> {
	const out = new Set<string>();
	for (const m of template.matchAll(PLACEHOLDER_RE)) {
		out.add(m[1] as string);
	}
	return out;
}

function validateSlots(d: Diagnostic, placeholders: Set<string>): void {
	for (const name of Object.keys(d.slots)) {
		if (name === "path") {
			throw new RenderError(
				`render: code ${d.code} binds {path} manually; it is auto-injected`,
			);
		}
		if (!placeholders.has(name)) {
			throw new RenderError(
				`render: code ${d.code} has unknown slot ${JSON.stringify(name)} (not a template placeholder)`,
			);
		}
	}
	for (const name of placeholders) {
		if (name === "path" || name === "suggestion") {
			continue;
		}
		if (!(name in d.slots)) {
			throw new RenderError(
				`render: code ${d.code} missing required slot ${JSON.stringify(name)}`,
			);
		}
	}
}

function renderSlot(code: string, name: string, slot: Slot): string {
	switch (slot.t) {
		case "string":
			return slot.s;
		case "int":
			return String(slot.n);
		case "code":
			return slot.code;
		case "identifier":
			return slot.name;
		case "version":
			return String(slot.v);
		case "path":
			return slot.p.render();
		case "value":
			return renderValueAtDepth(slot.value, 1);
		case "list":
			return renderArray(slot.elems, 1);
		case "suggestion":
			return renderSuggestion(slot);
		default: {
			const _exhaustive: never = slot;
			throw new RenderError(
				`render: code ${code} slot ${JSON.stringify(name)} has unknown slot type ${JSON.stringify(_exhaustive)}`,
			);
		}
	}
}

// --- Value rendering (appendix-rendering.md Part A) --------------------------

// Render a document value per A.1 (top-level container depth = 1).
export function renderValue(v: Value): string {
	return renderValueAtDepth(v, 1);
}

function renderValueAtDepth(v: Value, depth: number): string {
	switch (v.v) {
		case "int":
			return v.n.toString();
		case "float":
			return v.hasLexeme ? v.lexeme : canonicalFloat(v.f);
		case "number":
			return v.lexeme;
		case "string":
			return renderQuotedString(v.s);
		case "bool":
			return v.b ? "true" : "false";
		case "null":
			return "null";
		case "date":
			return v.s;
		case "time":
			return v.s;
		case "datetime":
			return v.s;
		case "array":
			return depth > 2 ? "[...]" : renderArray(v.elems, depth);
		case "record":
			return depth > 2 ? "{...}" : renderRecord(v, depth);
	}
}

function renderArray(elems: readonly Value[], depth: number): string {
	let out = "[";
	const shown = Math.min(elems.length, 3);
	for (let i = 0; i < shown; i++) {
		if (i > 0) {
			out += ", ";
		}
		out += renderValueAtDepth(elems[i] as Value, depth + 1);
	}
	if (elems.length > 3) {
		out += ", ...";
	}
	out += "]";
	return out;
}

function renderRecord(
	r: Extract<Value, { v: "record" }>,
	depth: number,
): string {
	if (r.keys.length !== r.vals.length) {
		throw new RenderError(
			`render: RecordVal has ${r.keys.length} keys but ${r.vals.length} values`,
		);
	}
	let out = "{";
	const shown = Math.min(r.keys.length, 3);
	for (let i = 0; i < shown; i++) {
		if (i > 0) {
			out += ", ";
		}
		const key = r.keys[i] as string;
		out += isIdentShaped(key) ? key : `"${escapeString(key)}"`;
		out += ": ";
		out += renderValueAtDepth(r.vals[i] as Value, depth + 1);
	}
	if (r.keys.length > 3) {
		out += ", ...";
	}
	out += "}";
	return out;
}

function renderQuotedString(sIn: string): string {
	let s = sIn;
	let truncated = false;
	if (codePointLength(s) > 64) {
		s = codePointSlice(s, 64);
		truncated = true;
	}
	const content = escapeString(s);
	return truncated ? `"${content}..."` : `"${content}"`;
}

function canonicalFloat(f: number): string {
	if (Object.is(f, -0)) {
		return "-0.0";
	}
	let s = String(f);
	if (!/[.eE]/.test(s)) {
		s += ".0";
	}
	return s;
}

// --- did-you-mean (appendix-rendering.md Part C) -----------------------------

function renderSuggestion(s: Extract<Slot, { t: "suggestion" }>): string {
	const cands: Array<[string, number]> = [];
	for (const c of s.candidates) {
		const dist = levenshtein(s.unknown, c);
		if (dist <= 2) {
			cands.push([c, dist]);
		}
	}
	cands.sort((a, b) =>
		a[1] !== b[1] ? a[1] - b[1] : a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0,
	);
	const top = cands.slice(0, 3);
	if (top.length === 0) {
		return "";
	}
	const names = top.map((c) => renderCandidate(c[0]));
	if (names.length === 1) {
		return ` Did you mean ${names[0]}?`;
	}
	if (names.length === 2) {
		return ` Did you mean ${names[0]} or ${names[1]}?`;
	}
	return ` Did you mean ${names[0]}, ${names[1]}, or ${names[2]}?`;
}

function renderCandidate(name: string): string {
	return isIdentShaped(name) ? name : renderQuotedString(name);
}

// Case-sensitive Levenshtein distance over code points.
function levenshtein(aIn: string, bIn: string): number {
	const ra = [...aIn];
	const rb = [...bIn];
	if (ra.length === 0) {
		return rb.length;
	}
	if (rb.length === 0) {
		return ra.length;
	}
	let prev = Array.from({ length: rb.length + 1 }, (_, i) => i);
	let curr = new Array<number>(rb.length + 1).fill(0);
	for (let i = 1; i <= ra.length; i++) {
		curr[0] = i;
		for (let j = 1; j <= rb.length; j++) {
			const cost = ra[i - 1] === rb[j - 1] ? 0 : 1;
			curr[j] = Math.min(
				(prev[j] as number) + 1,
				(curr[j - 1] as number) + 1,
				(prev[j - 1] as number) + cost,
			);
		}
		[prev, curr] = [curr, prev];
	}
	return prev[rb.length] as number;
}
