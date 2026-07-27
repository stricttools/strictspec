import assert from "node:assert/strict";
import { test } from "node:test";
import { version } from "../src/index.js";

test("version constant", () => {
	assert.equal(version, "0.0.0");
});
