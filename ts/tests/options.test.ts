// The options built-ins and rules (.strictmetadata/docs/appendix-options.md).
//
// The shape and rules cases live in go/strictspec/testdata/options/ and are
// shared with the Go and Python runtimes' tests, so every runtime asserts the
// identical bound values, diagnostics, and classifications.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
	type CheckedOptionsRegistry,
	type Diagnostic,
	type OptionsEntry,
	type OptionsRanking,
	type OptionsRegistry,
	optionsEntriesProgram,
	optionsRegistryProgram,
	parseOptionsRanking,
	readOptionsEntries,
	readOptionsRegistry,
	readUpstream,
	upstreamProgram,
	validateOptionsNamespace,
	validateOptionsRegistry,
} from "../dist/index.js";

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
	// options-registry is at 2: version 2 added requires.
	const version = name === "options-registry" ? 2 : 1;
	test(`built-in ${name} accepts only format_version ${version}`, () => {
		const res = program().validate("format_version = 7\n", "toml");
		assert.deepEqual(
			res.diagnostics.map((d) => d.code),
			["STRICTSPEC_GATE_UNSUPPORTED"],
		);
		assert.ok(res.diagnostics[0]?.message.includes(`schema ${name} `));
		assert.ok(
			res.diagnostics[0]?.message.includes(`accepts exactly ${version} `),
		);
	});
}

// --- the rules beyond shape ---------------------------------------------------

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
		const [rk, ds] = parseOptionsRanking(c.input);
		assert.deepEqual(diags(ds), c.diagnostics);
		assert.deepEqual(levels(rk), c.levels);
		assert.equal(rk === null, ds.length > 0);
	});
}

function registryText(src: string): OptionsRegistry {
	const [reg, ds] = readOptionsRegistry(src);
	assert.deepEqual(ds, []);
	assert.ok(reg !== null);
	return reg;
}

// A registry case's input: a registry document read through the shape reader,
// or options handed to the validator directly.
function registry(inp: Json): OptionsRegistry {
	if (inp.options !== undefined) {
		return { options: inp.options };
	}
	return registryText(inp.registry);
}

for (const c of rulesCases.registry) {
	test(`registry: ${c.name}`, () => {
		const [checked, ds] = validateOptionsRegistry(registry(c));
		assert.deepEqual(diags(ds), c.diagnostics);
		assert.equal(checked === null, c.diagnostics.length > 0);
	});
}

const ns = rulesCases.namespace;

function namespaceRegistry(): CheckedOptionsRegistry {
	const [reg, ds] = validateOptionsRegistry(registryText(ns.registry));
	assert.deepEqual(ds, []);
	assert.ok(reg !== null);
	return reg;
}

// A namespace case's input: subject documents read through the shape reader,
// or entries handed to the validator directly, bypassing the shape reader.
function entries(inp: Json): OptionsEntry[] {
	if (inp.entries !== undefined) {
		return inp.entries.map((e: Json) => ({ scope: null, ...e }));
	}
	const out: OptionsEntry[] = [];
	for (const name of Object.keys(inp.files).sort()) {
		const [es, ds] = readOptionsEntries(name, inp.files[name]);
		assert.deepEqual(ds, []);
		out.push(...es);
	}
	return out;
}

for (const c of ns.cases) {
	test(`namespace: ${c.name}`, () => {
		const [accepted, ds] = validateOptionsNamespace(
			ns.tool,
			namespaceRegistry(),
			entries(c),
		);
		assert.deepEqual(diags(ds), c.diagnostics);
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

function runFixKind(kind: string, inp: Json): readonly Diagnostic[] {
	switch (kind) {
		case "ranking":
			return parseOptionsRanking(inp.input)[1];
		case "registry":
			return validateOptionsRegistry(registry(inp))[1];
		case "namespace":
			return validateOptionsNamespace(
				ns.tool,
				namespaceRegistry(),
				entries(inp),
			)[1];
	}
	throw new Error(`unknown fix kind ${kind}`);
}

// Every fix a message names: the "before" input draws a diagnostic with the
// case's code whose message names the fix, and the "after" input, which
// applies that fix and nothing else, draws no diagnostic at all.
for (const c of rulesCases.fixes) {
	test(`fix instruction: ${c.name}`, () => {
		const before = runFixKind(c.kind, c.before);
		assert.ok(
			before.some((d) => d.code === c.code && d.message.includes(c.fix)),
			JSON.stringify(diags(before)),
		);
		assert.deepEqual(diags(runFixKind(c.kind, c.after)), []);
	});
}
