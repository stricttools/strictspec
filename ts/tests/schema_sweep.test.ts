// Meta-schema reader sweep over examples/** (port of the Go/Python sweep):
// every role-bearing schema/type-definition file reads with ZERO authoring
// diagnostics, except the deliberately-invalid companions whose expected
// rejection is asserted.

import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { basename, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { type Node, ParseError } from "../dist/doc.js";
import { readSchema, strEntry } from "../dist/schema.js";
import { parse as parseToml } from "../dist/tomldoc.js";

// Compiled tests live at dist-test/*.test.js; repo root is two levels up.
const repoRoot = fileURLToPath(new URL("../../", import.meta.url));
const EXAMPLES = join(repoRoot, "examples");

const KNOWN_INVALID: Record<string, string> = {
	"INVALID-types-transitive.toml": "STRICTSPEC_IMPORT_TRANSITIVE",
	"INVALID-types-with-constraint.toml":
		"STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
	"opaque-no-stance.reject.toml": "STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE",
	"unchecked-no-reason.reject.toml": "STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON",
};

function walkToml(dir: string): string[] {
	const out: string[] = [];
	for (const name of readdirSync(dir)) {
		const p = join(dir, name);
		const st = statSync(p);
		if (st.isDirectory()) {
			out.push(...walkToml(p));
		} else if (name.endsWith(".toml")) {
			out.push(p);
		}
	}
	return out;
}

test("examples sweep", {
	skip: !existsSync(EXAMPLES) ? "examples/ not found" : false,
}, () => {
	let scanned = 0;
	for (const path of walkToml(EXAMPLES)) {
		let root: Node;
		try {
			root = parseToml(readFileSync(path)).root;
		} catch (e) {
			if (e instanceof ParseError) {
				continue;
			}
			throw e;
		}
		const [role] = strEntry(root, "role");
		if (role !== "schema" && role !== "type-definitions") {
			continue;
		}
		scanned += 1;
		const [s, diags] = readSchema(root, path);
		const codes = diags.map((d) => d.code);
		const base = basename(path);
		if (base in KNOWN_INVALID) {
			assert.ok(
				codes.includes(KNOWN_INVALID[base] as string),
				`${base}: expected ${KNOWN_INVALID[base]}, got ${codes}`,
			);
		} else {
			assert.deepEqual(
				codes,
				[],
				`${base} (${s.name}): expected zero authoring diagnostics, got ${codes}`,
			);
		}
	}
	assert.ok(scanned >= 30, `sweep scanned only ${scanned} schema files`);
});
