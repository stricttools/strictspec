// Freshness gate for the generated code catalogue (mirrors the Go/Python
// freshness test pattern): regenerate src/codes.generated.ts from the normative
// spec and assert the on-disk file is byte-identical. Drift = failure.

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

// Compiled tests live at dist-test/*.test.js; the ts/ root is one level up.
const tsRoot = fileURLToPath(new URL("../", import.meta.url));

test("catalogue is fresh", () => {
	const res = spawnSync(process.execPath, ["scripts/gencodes.mjs", "--check"], {
		cwd: tsRoot,
		encoding: "utf-8",
	});
	assert.equal(
		res.status,
		0,
		`src/codes.generated.ts is STALE relative to spec/appendix-error-codes.md; regenerate with scripts/gencodes.mjs\n${res.stderr}`,
	);
});
