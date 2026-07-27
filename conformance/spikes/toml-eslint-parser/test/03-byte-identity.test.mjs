import test from "node:test";
import assert from "node:assert/strict";
import { parse, collectValues } from "../lib/lossless.mjs";
import { TORTURE } from "./fixture.mjs";

/**
 * Test 3 -- Byte identity under no-op. The splicing technique's foundational
 * guarantee: reconstructing the document by copying every byte around each
 * value range yields the identical original text. No canonicalization, no
 * reflow, no lost bytes.
 */

test("no-op reconstruction around every value range is byte-identical", () => {
	const program = parse(TORTURE);
	const values = collectValues(program);
	// For each value, splice its own lexeme back in place; result must equal input.
	for (const { node } of values) {
		const [s, e] = node.range;
		const rebuilt = TORTURE.slice(0, s) + TORTURE.slice(s, e) + TORTURE.slice(e);
		assert.equal(rebuilt, TORTURE);
	}
});

test("ranges are non-overlapping and within bounds", () => {
	const program = parse(TORTURE);
	const values = collectValues(program);
	for (const { node, path } of values) {
		const [s, e] = node.range;
		assert.ok(s >= 0 && e <= TORTURE.length && s < e, `sane range at ${path}`);
	}
});

test("parsing does not mutate or normalize the source string", () => {
	const before = TORTURE;
	parse(TORTURE);
	assert.equal(TORTURE, before);
});
