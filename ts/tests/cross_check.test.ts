// Cross-target parity: the TS runtime must produce IDENTICAL ordered
// (code, path, message) output to the Go reference interpreter on representative
// example fixtures.
//
// The Go conformance-adapter binary is built once (via `go build`) into a
// gitignored temporary directory (read-only w.r.t. the repo) and invoked per
// fixture with the same JSON request the conformance harness uses. Any divergence
// fails the test; Go is the reference (a TS divergence is a TS bug to fix).

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
	existsSync,
	mkdtempSync,
	readdirSync,
	readFileSync,
	rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { compileFromSource, type Syntax } from "../dist/index.js";
import type * as schema from "../dist/schema.js";

const repoRoot = fileURLToPath(new URL("../../", import.meta.url));
const GO = join(repoRoot, "go");
const EXAMPLES = join(repoRoot, "examples");

// 6 representative (schema, input, syntax) fixtures spanning: valid, JSON type/
// regex/union errors, datetime kind, enum, intra-document constraint, and JSONL
// per-line anchors.
const FIXTURES: Array<[string, string, Syntax]> = [
	[
		"shared-types-exercise/schema-canvas.toml",
		"shared-types-exercise/canvas.valid.json",
		"json",
	],
	[
		"shared-types-exercise/schema-canvas.toml",
		"shared-types-exercise/canvas.invalid.json",
		"json",
	],
	[
		"datetime-exercise/schema-datetime.toml",
		"datetime-exercise/invalid-01-kind-and-range.json",
		"json",
	],
	[
		"enum-baking-exercise/schema-drum-pattern.toml",
		"enum-baking-exercise/pattern.invalid.json",
		"json",
	],
	[
		"rlsbl-config/config.schema.toml",
		"rlsbl-config/invalid-01-launcher.json",
		"json",
	],
	[
		"rlsbl-changelog-entry/changelog-entry-commit-mode.schema.toml",
		"rlsbl-changelog-entry/invalid-commit-mode.jsonl",
		"jsonl",
	],
];

interface Shape {
	valid: boolean;
	diagnostics: Array<{ code: string; path: string; message: string }>;
}

// TS analogue of the Go adapter run(): build a FileSet from every *.toml in the
// schema's directory (so imports + manifest scalars resolve in-memory, exactly as
// the browser-safe compile path does), then validate the input.
function tsValidate(
	schemaPath: string,
	inputPath: string,
	syntax: Syntax,
): Shape {
	const dir = dirname(schemaPath);
	const files: schema.FileSet = {};
	for (const name of readdirSync(dir)) {
		if (name.endsWith(".toml")) {
			files[name] = readFileSync(join(dir, name), "utf-8");
		}
	}
	const mainFile = basename(schemaPath);
	const prog = compileFromSource(files, mainFile);
	const rawText = readFileSync(inputPath, "utf-8");
	const res = prog.validate(rawText, syntax);
	return {
		valid: res.valid,
		diagnostics: res.diagnostics.map((d) => ({
			code: d.code,
			path: d.path,
			message: d.message,
		})),
	};
}

function haveGo(): boolean {
	const r = spawnSync("go", ["version"], { encoding: "utf-8" });
	return r.status === 0;
}

function buildAdapter(): string | null {
	const tmp = mkdtempSync(join(tmpdir(), "strictspec-adapter-"));
	const binary = join(tmp, "conformance-adapter");
	const proc = spawnSync(
		"go",
		["build", "-o", binary, "./cmd/conformance-adapter"],
		{
			cwd: GO,
			encoding: "utf-8",
		},
	);
	if (proc.status !== 0) {
		rmSync(tmp, { recursive: true, force: true });
		return null;
	}
	return binary;
}

function runGo(
	binary: string,
	schemaPath: string,
	inputPath: string,
	syntax: Syntax,
): Shape {
	const req = JSON.stringify({
		schema: schemaPath,
		input_path: inputPath,
		input_syntax: syntax,
		evidence: {},
	});
	const proc = spawnSync(binary, [], {
		input: req,
		encoding: "utf-8",
		cwd: repoRoot,
	});
	assert.equal(proc.status, 0, `go adapter failed: ${proc.stderr}`);
	return JSON.parse(proc.stdout) as Shape;
}

test("go/ts parity on representative fixtures", {
	skip: !existsSync(EXAMPLES) ? "examples/ not found" : false,
}, (t) => {
	if (!haveGo()) {
		t.skip("go toolchain not available");
		return;
	}
	const binary = buildAdapter();
	if (binary === null) {
		t.skip("go build failed");
		return;
	}
	try {
		for (const [schemaRel, inputRel, syntax] of FIXTURES) {
			const schemaPath = join(EXAMPLES, schemaRel);
			const inputPath = join(EXAMPLES, inputRel);
			const goOut = runGo(binary, schemaPath, inputPath, syntax);
			const tsOut = tsValidate(schemaPath, inputPath, syntax);
			assert.deepEqual(
				tsOut,
				goOut,
				`parity mismatch for ${inputRel}\nGO: ${JSON.stringify(goOut, null, 2)}\nTS: ${JSON.stringify(tsOut, null, 2)}`,
			);
		}
	} finally {
		rmSync(dirname(binary), { recursive: true, force: true });
	}
});
