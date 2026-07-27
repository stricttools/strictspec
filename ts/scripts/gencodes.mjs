#!/usr/bin/env node
// Generate the TypeScript strictspec error-code catalogue module.
//
// Parses spec/appendix-error-codes.md (the single normative source for the
// error-code catalogue) and emits src/codes.generated.ts. This mirrors the Go
// generator (go/tools/gencodes) and the Python generator
// (python/scripts/gencodes.py): the appendix is the only writer of the
// catalogue, and a freshness test regenerates and byte-compares (drift =
// failure).
//
// Usage:
//   node scripts/gencodes.mjs [--check]
//
// With no flags it auto-locates the repo root (the ancestor containing
// spec/appendix-error-codes.md) and derives both paths. --check regenerates in
// memory and exits non-zero if the on-disk file differs (the freshness gate).

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const SPEC_REL = "spec/appendix-error-codes.md";
const OUT_REL = "ts/src/codes.generated.ts";

const CODE_ROW_RE = /^\| `STRICTSPEC_/;
const AREA_ROW_RE = /^\| `([A-Z]+)` \|/;
const AREA_HEADING_RE = /^## 3\. /;
const NEXT_HEADING_RE = /^## /;
const PLACEHOLDER_RE = /\{(\w+)\}/g;
const SLOT_ENTRY_RE = /^([A-Za-z_]\w*):\s*(.+)$/;
const BACKTICK_CODE_RE = /^`(STRICTSPEC_[A-Z0-9_]+)`$/;

function findRepoRoot(start) {
	let d = start;
	for (;;) {
		if (existsSync(join(d, SPEC_REL))) {
			return d;
		}
		const parent = dirname(d);
		if (parent === d) {
			throw new Error(`gencodes: could not locate ${SPEC_REL} in any ancestor`);
		}
		d = parent;
	}
}

function splitCells(row) {
	const cells = [];
	let cur = "";
	let i = 0;
	const n = row.length;
	while (i < n) {
		const ch = row[i];
		if (ch === "\\" && i + 1 < n && row[i + 1] === "|") {
			cur += "|";
			i += 2;
			continue;
		}
		if (ch === "|") {
			cells.push(cur);
			cur = "";
			i += 1;
			continue;
		}
		cur += ch;
		i += 1;
	}
	cells.push(cur);
	let out = cells;
	if (out.length >= 2) {
		out = out.slice(1, -1);
	}
	return out.map((c) => c.trim());
}

function parseAreas(lines) {
	const areas = [];
	let inSection = false;
	for (const line of lines) {
		if (AREA_HEADING_RE.test(line)) {
			inSection = true;
			continue;
		}
		if (inSection && NEXT_HEADING_RE.test(line)) {
			break;
		}
		if (!inSection) {
			continue;
		}
		const m = AREA_ROW_RE.exec(line);
		if (m) {
			areas.push(m[1]);
		}
	}
	if (areas.length === 0) {
		throw new Error(
			"gencodes: could not parse the closed area set from section 3",
		);
	}
	return areas.sort();
}

function parseCodeCell(cell) {
	const m = BACKTICK_CODE_RE.exec(cell);
	if (m === null) {
		throw new Error(`gencodes: malformed code cell ${JSON.stringify(cell)}`);
	}
	return m[1];
}

function parseTemplateCell(cell) {
	if (cell.length < 2 || cell[0] !== "`" || cell[cell.length - 1] !== "`") {
		throw new Error(
			`gencodes: template cell not backtick-delimited: ${JSON.stringify(cell)}`,
		);
	}
	return cell.slice(1, -1);
}

function areaOf(code, areaSet) {
	const parts = code.split("_");
	if (parts.length < 3 || parts[0] !== "STRICTSPEC") {
		throw new Error(
			`gencodes: code ${JSON.stringify(code)} is not STRICTSPEC_<AREA>_<NAME>`,
		);
	}
	const area = parts[1];
	if (!areaSet.has(area)) {
		throw new Error(
			`gencodes: code ${JSON.stringify(code)} has area ${JSON.stringify(area)} outside the closed set`,
		);
	}
	return area;
}

const SCALAR_SLOT_TYPES = {
	string: "String",
	int: "Int",
	code: "Code",
	identifier: "Identifier",
	version: "Version",
	path: "Path",
	value: "Value",
};

function scalarSlotType(token) {
	const t = SCALAR_SLOT_TYPES[token];
	if (t === undefined) {
		throw new Error(`gencodes: unknown slot type ${JSON.stringify(token)}`);
	}
	return t;
}

function parseSlotType(name, typeTextIn) {
	const typeText = typeTextIn
		.replaceAll("\\<", "<")
		.replaceAll("\\>", ">")
		.trim();
	if (typeText.startsWith("list<") && typeText.endsWith(">")) {
		const elem = typeText.slice("list<".length, -1);
		return { name, typ: "List", elemType: scalarSlotType(elem) };
	}
	return { name, typ: scalarSlotType(typeText) };
}

function parseSlotsCell(cell) {
	const out = {};
	const c = cell.trim();
	if (c === "" || c === "—" || c === "-") {
		return out;
	}
	for (const partIn of c.split(",")) {
		const part = partIn.trim();
		if (part === "") {
			continue;
		}
		const m = SLOT_ENTRY_RE.exec(part);
		if (m === null) {
			throw new Error(
				`gencodes: malformed slot declaration ${JSON.stringify(part)}`,
			);
		}
		out[m[1]] = parseSlotType(m[1], m[2].trim());
	}
	return out;
}

function placeholderOrder(template) {
	const order = [];
	const seen = new Set();
	for (const m of template.matchAll(PLACEHOLDER_RE)) {
		const name = m[1];
		if (seen.has(name)) {
			continue;
		}
		seen.add(name);
		order.push(name);
	}
	return { order, seen };
}

function resolveSlots(code, template, declared) {
	const { order, seen } = placeholderOrder(template);
	for (const name of Object.keys(declared)) {
		if (!seen.has(name)) {
			throw new Error(
				`gencodes: code ${code} declares slot ${JSON.stringify(name)} the template does not reference`,
			);
		}
	}
	const slots = [];
	for (const name of order) {
		if (name in declared) {
			slots.push(declared[name]);
			continue;
		}
		if (name === "path") {
			slots.push({ name: "path", typ: "Path" });
		} else if (name === "suggestion") {
			slots.push({ name: "suggestion", typ: "String" });
		} else {
			throw new Error(
				`gencodes: code ${code}: placeholder {${name}} has no declared slot type`,
			);
		}
	}
	return slots;
}

function parseSpec(src) {
	const lines = src.split("\n");
	const areas = parseAreas(lines);
	const areaSet = new Set(areas);

	const entries = [];
	const seen = new Set();
	let lineno = 0;
	for (const line of lines) {
		lineno += 1;
		if (!CODE_ROW_RE.test(line)) {
			continue;
		}
		const cells = splitCells(line);
		if (cells.length < 3) {
			throw new Error(
				`gencodes: line ${lineno}: expected >=3 cells, got ${cells.length}`,
			);
		}
		const code = parseCodeCell(cells[0]);
		if (seen.has(code)) {
			throw new Error(`gencodes: line ${lineno}: duplicate code ${code}`);
		}
		seen.add(code);
		const template = parseTemplateCell(cells[1]);
		const area = areaOf(code, areaSet);
		const declared = parseSlotsCell(cells[2]);
		const slots = resolveSlots(code, template, declared);
		entries.push({ code, area, template, slots });
	}
	if (entries.length === 0) {
		throw new Error("gencodes: no code rows found");
	}
	entries.sort((a, b) => (a.code < b.code ? -1 : a.code > b.code ? 1 : 0));
	return { entries, areas };
}

function q(s) {
	return JSON.stringify(s);
}

function render(entries, areas) {
	const out = [];
	out.push(`/* eslint-disable */
// The generated strictspec error-code catalogue.
//
// Code generated by scripts/gencodes.mjs from ${SPEC_REL}. DO NOT EDIT.
//
// Every STRICTSPEC_* code with its area, message template, and declared slots,
// parsed from the single normative source. Hand-transcription is forbidden; a
// freshness test regenerates and byte-compares (drift = failure).

export enum SlotType {
\tString = 0,
\tInt = 1,
\tCode = 2,
\tIdentifier = 3,
\tVersion = 4,
\tPath = 5,
\tValue = 6,
\tList = 7,
}

export interface SlotSpec {
\treadonly name: string;
\treadonly type: SlotType;
\treadonly elemType?: SlotType;
}

export interface Entry {
\treadonly code: string;
\treadonly area: string;
\treadonly template: string;
\treadonly slots: readonly SlotSpec[];
}

export function lookup(code: string): Entry | undefined {
\treturn CATALOGUE.get(code);
}

export function allEntries(): Entry[] {
\treturn [...CATALOGUE.keys()].sort().map((k) => CATALOGUE.get(k) as Entry);
}

// The closed area set (appendix-error-codes.md section 3).
export const AREAS: readonly string[] = [
`);
	for (const a of areas) {
		out.push(`\t${q(a)},\n`);
	}
	out.push("];\n\nexport const CATALOGUE: Map<string, Entry> = new Map([\n");
	for (const e of entries) {
		out.push(`\t[\n\t\t${q(e.code)},\n\t\t{\n`);
		out.push(`\t\t\tcode: ${q(e.code)},\n`);
		out.push(`\t\t\tarea: ${q(e.area)},\n`);
		out.push(`\t\t\ttemplate: ${q(e.template)},\n`);
		if (e.slots.length === 0) {
			out.push("\t\t\tslots: [],\n");
		} else {
			out.push("\t\t\tslots: [\n");
			for (const s of e.slots) {
				if (s.elemType !== undefined) {
					out.push(
						`\t\t\t\t{ name: ${q(s.name)}, type: SlotType.${s.typ}, elemType: SlotType.${s.elemType} },\n`,
					);
				} else {
					out.push(
						`\t\t\t\t{ name: ${q(s.name)}, type: SlotType.${s.typ} },\n`,
					);
				}
			}
			out.push("\t\t\t],\n");
		}
		out.push("\t\t},\n\t],\n");
	}
	out.push("]);\n");
	return out.join("");
}

function generate(repoRoot) {
	const specPath = join(repoRoot, SPEC_REL);
	const src = readFileSync(specPath, "utf-8");
	const { entries, areas } = parseSpec(src);
	return { content: render(entries, areas), count: entries.length };
}

function main() {
	const check = process.argv.includes("--check");
	const root = findRepoRoot(dirname(fileURLToPath(import.meta.url)));
	const outPath = join(root, OUT_REL);
	const { content, count } = generate(root);

	if (check) {
		const existing = existsSync(outPath) ? readFileSync(outPath, "utf-8") : "";
		if (existing !== content) {
			process.stderr.write(
				`gencodes: ${outPath} is STALE; run scripts/gencodes.mjs\n`,
			);
			process.exit(1);
		}
		process.stdout.write(`gencodes: ${outPath} is fresh (${count} codes)\n`);
		return;
	}
	writeFileSync(outPath, content, "utf-8");
	process.stdout.write(`gencodes: wrote ${count} codes to ${outPath}\n`);
}

main();
