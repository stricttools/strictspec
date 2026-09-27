// The options built-ins and rules (stricttools/docs/appendix-options.md).
//
// The shape and rules cases live in go/strictspec/testdata/options/ and are
// shared with the Go and Python runtimes' tests, so every runtime asserts the
// identical bound values, diagnostics, refusals, and classifications.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
	type Diagnostic,
	type OptionsEntry,
	optionsEntriesProgram,
	optionsRegistryProgram,
	readOptionsEntries,
	readOptionsRegistry,
	readUpstream,
	upstreamProgram,
} from "../dist/index.js";
import {
	checkOptionsRegistry,
	type OptionsRanking,
	type OptionsRefusal,
	parseOptionsRanking,
	validateOptionsNamespace,
} from "../dist/options.js";

const casesDir = fileURLToPath(
	new URL("../../go/strictspec/testdata/options/", import.meta.url),
);

// biome-ignore lint/suspicious/noExplicitAny: the cases are untyped JSON
type Json = any;

const shapeCases: Json[] = JSON.parse(
	readFileSync(`${casesDir}shape-cases.json`, "utf-8"),
);
const rulesCases: Json = JSON.parse(
	readFileSync(`${casesDir}rules-cases.json`, "utf-8"),
);

function diags(ds: readonly Diagnostic[]): Json[] {
	return ds.map((d) => ({ code: d.code, path: d.path, message: d.message }));
}

function runShape(c: Json): Json {
	const got: Json = {};
	let ds: Diagnostic[];
	if (c.schema === "entries") {
		const [es, d] = readOptionsEntries(c.file, c.input);
		ds = d;
		if (es.length > 0) {
			got.entries = es.map((e) => ({ ...e }));
		}
	} else if (c.schema === "registry") {
		const [reg, d] = readOptionsRegistry(c.input);
		ds = d;
		if (reg !== null && reg.options.length > 0) {
			got.options = reg.options.map((o) => ({ ...o }));
		}
	} else {
		const [u, d] = readUpstream(c.input);
		ds = d;
		if (u !== null) {
			got.upstream = { ...u };
		}
	}
	if (ds.length > 0) {
		got.diagnostics = diags(ds);
	}
	return got;
}

for (const c of shapeCases) {
	test(`shape: ${c.name}`, () => {
		const got = runShape(c);
		// An expected diagnostic without a message compares code and path only
		// (parse-error detail comes from each runtime's own parser).
		(c.expect.diagnostics ?? []).forEach((want: Json, i: number) => {
			if (!("message" in want) && got.diagnostics?.[i] !== undefined) {
				delete got.diagnostics[i].message;
			}
		});
		assert.deepEqual(got, c.expect);
	});
}

for (const [name, program] of [
	["options-entries", optionsEntriesProgram],
	["options-registry", optionsRegistryProgram],
	["upstream", upstreamProgram],
] as const) {
	test(`built-in ${name} accepts only format_version 1`, () => {
		const res = program().validate("format_version = 7\n", "toml");
		assert.deepEqual(
			res.diagnostics.map((d) => d.code),
			["STRICTSPEC_GATE_UNSUPPORTED"],
		);
		assert.ok(res.diagnostics[0]?.message.includes(`schema ${name} `));
		assert.ok(res.diagnostics[0]?.message.includes("accepts exactly 1 "));
	});
}

// --- the rules beyond shape ---------------------------------------------------

function refusals(rs: readonly OptionsRefusal[]): Json[] {
	return rs.map((r) => ({ ...r }));
}

function levels(rk: OptionsRanking | null): string[][] | undefined {
	if (rk === null) {
		return undefined;
	}
	const out: string[][] = [];
	for (const v of rk.values) {
		const lv = rk.level.get(v) as number;
		while (out.length <= lv) {
			out.push([]);
		}
		(out[lv] as string[]).push(v);
	}
	return out;
}

for (const c of rulesCases.ranking) {
	test(`ranking: ${c.name}`, () => {
		const [rk, rs] = parseOptionsRanking(c.input);
		assert.deepEqual(refusals(rs), c.refusals ?? []);
		assert.deepEqual(levels(rk), c.levels);
	});
}

function registry(src: string) {
	const [reg, ds] = readOptionsRegistry(src);
	assert.deepEqual(ds, []);
	assert.ok(reg !== null);
	return reg;
}

for (const c of rulesCases.registry) {
	test(`registry: ${c.name}`, () => {
		const [checked, rs] = checkOptionsRegistry(registry(c.registry));
		assert.deepEqual(refusals(rs), c.refusals);
		assert.equal(checked === null, c.refusals.length > 0);
	});
}

const ns = rulesCases.namespace;
for (const c of ns.cases) {
	test(`namespace: ${c.name}`, () => {
		const [reg, rr] = checkOptionsRegistry(registry(ns.registry));
		assert.deepEqual(rr, []);
		assert.ok(reg !== null);
		const entries: OptionsEntry[] = [];
		for (const name of Object.keys(c.files).sort()) {
			const [es, ds] = readOptionsEntries(name, c.files[name]);
			assert.deepEqual(ds, []);
			entries.push(...es);
		}
		const [accepted, rs] = validateOptionsNamespace(ns.tool, reg, entries);
		assert.deepEqual(refusals(rs), c.refusals);
		assert.deepEqual(
			accepted.map((a) => ({
				file: a.entry.file,
				index: a.entry.index,
				class: a.class,
			})),
			c.classified,
		);
	});
}
