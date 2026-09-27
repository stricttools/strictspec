// Freshness check for the generated built-in schema module: regenerate
// src/builtins.generated.ts from go/strictspec/builtin/ (the single source of
// every built-in schema) and assert the on-disk file is byte-identical.

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

// Compiled tests live at dist-test/*.test.js; the ts/ root is one level up.
const tsRoot = fileURLToPath(new URL("../", import.meta.url));

test("built-in schemas are fresh", () => {
	const res = spawnSync(
		process.execPath,
		["scripts/genbuiltins.mjs", "--check"],
		{ cwd: tsRoot, encoding: "utf-8" },
	);
	assert.equal(
		res.status,
		0,
		`src/builtins.generated.ts is stale relative to go/strictspec/builtin/; regenerate with node ts/scripts/genbuiltins.mjs\n${res.stderr}`,
	);
});
