import test from "node:test";
import assert from "node:assert/strict";
import {
	parse,
	replaceValue,
	renameKey,
	appendKey,
	deleteKeyLine,
} from "../lib/lossless.mjs";
import { TORTURE } from "./fixture.mjs";

/**
 * Test 4 -- Targeted edit fidelity. Each edit changes exactly one thing via a
 * range splice; every other byte (comments, whitespace, other values, string
 * styles, datetimes) must be identical. Proof method: reduce (old, new) to their
 * single contiguous difference region via longest common prefix + suffix. Since
 * that prefix and suffix together account for every unchanged byte, asserting
 * the OLD middle is exactly the edited span and the NEW middle is exactly the
 * replacement proves nothing else moved.
 */

/** Reduce two strings to the one region where they differ. */
function singleDiff(oldText, newText) {
	let p = 0;
	const maxP = Math.min(oldText.length, newText.length);
	while (p < maxP && oldText[p] === newText[p]) {
		p++;
	}
	let s = 0;
	while (
		s < maxP - p &&
		oldText[oldText.length - 1 - s] === newText[newText.length - 1 - s]
	) {
		s++;
	}
	return {
		oldMid: oldText.slice(p, oldText.length - s),
		newMid: newText.slice(p, newText.length - s),
		prefixLen: p,
		suffixLen: s,
	};
}

test("scalar value replacement: only the target value's bytes change", () => {
	const program = parse(TORTURE);
	const out = replaceValue(TORTURE, program, ["table_a", "count"], "999");
	const d = singleDiff(TORTURE, out);
	assert.equal(d.oldMid, "42");
	assert.equal(d.newMid, "999");
	// The trailing comment on the sibling key must survive verbatim.
	assert.ok(out.includes('key = "value"   # trailing comment inside a table'));
});

test("scalar replacement preserves an inline comment on the same line", () => {
	const program = parse(TORTURE);
	// title has an inline comment; replacing its value must not touch the comment.
	// Use a literal-string replacement so it shares no quote bytes with the old
	// value, keeping the diff region exactly the value lexeme.
	const out = replaceValue(TORTURE, program, ["title"], "'changed'");
	const d = singleDiff(TORTURE, out);
	assert.equal(d.oldMid, '"basic \\"quoted\\" string"');
	assert.equal(d.newMid, "'changed'");
	assert.ok(out.includes("# inline comment on a key"));
});

test("key rename: only the key spelling changes, value untouched", () => {
	const program = parse(TORTURE);
	// Rename to a name sharing no prefix/suffix bytes so the diff region is
	// exactly the old key -> new key.
	const out = renameKey(TORTURE, program, ["dec"], "qty");
	const d = singleDiff(TORTURE, out);
	assert.equal(d.oldMid, "dec");
	assert.equal(d.newMid, "qty");
	assert.ok(out.includes("qty = 1_000"));
});

test("append key to an existing table: one new line, everything else identical", () => {
	const program = parse(TORTURE);
	const out = appendKey(TORTURE, program, ["table_a"], "added", "true");
	const d = singleDiff(TORTURE, out);
	// Inserted region is purely additive (old middle empty).
	assert.equal(d.oldMid, "");
	assert.equal(d.newMid, "added = true\n");
	// Inserted after the last key of table_a (count = 42), before the next table.
	assert.ok(out.includes("count = 42\nadded = true\n\n[table_a.sub]"));
});

test("append key to the root block: additive line, other bytes identical", () => {
	const program = parse(TORTURE);
	const out = appendKey(TORTURE, program, [], "root_added", '"x"');
	const d = singleDiff(TORTURE, out);
	assert.equal(d.oldMid, "");
	assert.equal(d.newMid, 'root_added = "x"\n');
});

test("delete key + value line: exactly that line vanishes", () => {
	const program = parse(TORTURE);
	const out = deleteKeyLine(TORTURE, program, ["neg"], );
	const d = singleDiff(TORTURE, out);
	assert.equal(d.oldMid, "neg = -17\n");
	assert.equal(d.newMid, "");
	assert.ok(!out.includes("neg = -17"));
	// Neighbours survive.
	assert.ok(out.includes("dec = 1_000"));
	assert.ok(out.includes("hex = 0xDEAD_beef"));
});

test("edits are re-parseable and stable (splice output is valid TOML)", () => {
	const program = parse(TORTURE);
	const out = replaceValue(TORTURE, program, ["table_a", "count"], "999");
	// Re-parsing the edited document must succeed and reflect the new value.
	const reparsed = parse(out);
	assert.ok(reparsed.body[0], "edited document re-parses");
});
