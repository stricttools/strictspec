#!/usr/bin/env node
// Conformance-adapter for the strictspec TypeScript runtime.
//
// The TS-side invocation contract for the cross-target conformance harness's
// `ts` target. It reads a JSON request on stdin describing one fixture (schema
// path + input document + optional cross-document evidence), drives the PUBLIC
// TS runtime (compileFromSource + Program.validate / the meta-schema reader for
// meta fixtures), and writes the observed outcome as JSON on stdout in the
// runner's expected shape:
//
//   {"valid": bool, "diagnostics": [{"code","path","message"}, ...]}
//
// This is the exact request/response contract the Go conformance-adapter speaks
// (go/cmd/conformance-adapter/main.go); byte-identical outcomes across targets
// are what the parity checker asserts. The harness builds ts/dist once per run
// (cached by src hash) and invokes this script per fixture.

import { readdirSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const DIST = join(dirname(fileURLToPath(import.meta.url)), "..", "dist");
const { compileFromSource } = await import(join(DIST, "index.js"));
const schema = await import(join(DIST, "schema.js"));
const tomldoc = await import(join(DIST, "tomldoc.js"));
const doc = await import(join(DIST, "doc.js"));
const diag = await import(join(DIST, "diag.js"));
const render = await import(join(DIST, "render.js"));

function buildFileSet(schemaPath) {
	// Read every .toml in the schema's directory into an in-memory FileSet keyed
	// by basename: imports reference sibling files by bare filename, and the
	// scalar manifest is a sibling .toml merged across the whole set.
	const directory = dirname(schemaPath);
	const files = {};
	for (const name of readdirSync(directory)) {
		if (!name.endsWith(".toml")) continue;
		const full = join(directory, name);
		try {
			files[name] = readFileSync(full, "utf-8");
		} catch {
			// directories and unreadable entries are skipped
		}
	}
	return { files, main: basename(schemaPath) };
}

function readInput(req) {
	if (req.input_inline) return req.input_inline;
	return readFileSync(req.input_path, "utf-8");
}

function renderDiags(diags) {
	return diags.map((d) => ({
		code: d.code,
		path: d.path.render(),
		message: render.render(d),
	}));
}

function parseDiag(pe) {
	let code = "STRICTSPEC_PARSE_JSON_SYNTAX";
	if (pe.format === doc.FORMAT_TOML) code = "STRICTSPEC_PARSE_TOML_SYNTAX";
	else if (pe.format === doc.FORMAT_JSONL)
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX";
	const slots = { detail: diag.slotString(pe.detail) };
	if (code === "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX")
		slots.line = diag.slotInt(pe.position.line);
	return diag.newDiagnostic(code, diag.newPath(), slots);
}

function metaValidate(req) {
	const src = readInput(req);
	let d;
	try {
		d = tomldoc.parse(src);
	} catch (e) {
		if (e instanceof doc.ParseError) return renderDiags([parseDiag(e)]);
		throw e;
	}
	const [, diags] = schema.readSchema(d.root, "");
	return renderDiags(diags);
}

function run(req) {
	const { files, main } = buildFileSet(req.schema);

	// Meta-schema mode: the fixture's "document" is itself a schema file, read AS
	// a document of the built-in meta-schema.
	const [s] = schema.parseFrom(files, main);
	if (s.name === "strictspec-meta-schema") {
		const diagnostics = metaValidate(req);
		return { valid: diagnostics.length === 0, diagnostics };
	}

	const program = compileFromSource(files, main);
	const rawText = readInput(req);
	const evidence = req.evidence ?? null;
	const result = program.validateWithEvidence(rawText, req.input_syntax, evidence);
	return {
		valid: result.valid,
		diagnostics: result.diagnostics.map((d) => ({
			code: d.code,
			path: d.path,
			message: d.message,
		})),
	};
}

async function main() {
	const chunks = [];
	for await (const chunk of process.stdin) chunks.push(chunk);
	let req;
	try {
		req = JSON.parse(Buffer.concat(chunks).toString("utf-8"));
	} catch (e) {
		process.stderr.write(JSON.stringify({ error: `bad request: ${e}` }));
		process.exit(2);
	}
	try {
		const resp = run(req);
		process.stdout.write(JSON.stringify(resp));
	} catch (e) {
		process.stderr.write(JSON.stringify({ error: String(e && e.stack ? e.stack : e) }));
		process.exit(2);
	}
}

await main();
