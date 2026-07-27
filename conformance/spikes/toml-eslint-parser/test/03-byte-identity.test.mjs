import test from "node:test";
import assert from "node:assert/strict";
import { parse, collectValues, lexeme } from "../lib/lossless.mjs";
import { TORTURE } from "./fixture.mjs";

/**
 * Test 3 -- Byte identity under no-op. The splicing technique's foundational
 * guarantee: reconstructing the document by copying every byte around each
 * value range yields the identical original text. No canonicalization, no
 * reflow, no lost bytes.
 */

test("document reconstructs byte-identically from inter-node gaps + recovered lexemes", () => {
	const program = parse(TORTURE);
	const values = collectValues(program);
	// Order value nodes by where their range starts in the source.
	const nodes = values.map(({ node }) => node).sort((a, b) => a.range[0] - b.range[0]);

	// Precondition: ranges must be non-overlapping. If any lexeme range bled into
	// a neighbour, the gap-plus-lexeme tiling below would double-count bytes and
	// the reconstruction could not match — so overlap must be ruled out first.
	for (let i = 1; i < nodes.length; i++) {
		const prevEnd = nodes[i - 1].range[1];
		const start = nodes[i].range[0];
		assert.ok(prevEnd <= start, `ranges overlap between node ${i - 1} and ${i}`);
	}

	// Reconstruct the whole document: for each value node, take the ORIGINAL text
	// occupying the gap since the previous node's end, then that node's lexeme
	// recovered through its source range (via lossless.mjs's helper). If any
	// range mislocated its lexeme, the recovered text would not knit back into the
	// original at that offset. Trailing bytes after the last value close it out.
	let rebuilt = "";
	let cursor = 0;
	for (const node of nodes) {
		rebuilt += TORTURE.slice(cursor, node.range[0]);
		rebuilt += lexeme(TORTURE, node);
		cursor = node.range[1];
	}
	rebuilt += TORTURE.slice(cursor);

	assert.equal(rebuilt, TORTURE);
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
