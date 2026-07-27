/**
 * Lossless TOML helpers built on toml-eslint-parser, mirroring the proven
 * technique from strictcli's TypeScript config splicer
 * (strictcli/typescript/src/toml.ts): parse to an AST whose every value node
 * carries a precise [start, end] source range, then perform all edits as pure
 * string surgery ("splicing") on those ranges. Untouched bytes are copied
 * verbatim, so anything not explicitly edited stays byte-identical.
 *
 * This module is the reusable core the spike's tests exercise. It is
 * deliberately compact but covers everything the strictspec write path needs:
 * value-node enumeration with exact lexeme recovery, integer/float lexeme-class
 * distinction, and the four targeted edit primitives (scalar replace, key
 * rename, append key, delete key line).
 */

import { parseTOML } from "toml-eslint-parser";

/** Parse TOML text at a pinned spec version, returning the AST program. */
export function parse(text, tomlVersion = "1.0") {
	return parseTOML(text, { tomlVersion });
}

/** Exact original lexeme for any AST node: the raw source between its range. */
export function lexeme(text, node) {
	return text.slice(node.range[0], node.range[1]);
}

/** Segments of a dotted key node as plain strings (bare or quoted spelling-agnostic). */
function keySegments(key) {
	return key.keys.map((k) => (k.type === "TOMLBare" ? k.name : String(k.value)));
}

/**
 * Recursively collect every scalar TOMLValue node in the document, paired with
 * its resolved dotted path. Descends into arrays and inline tables. Array
 * elements get an index segment; this is enumeration for assertions, not a
 * value model.
 */
export function collectValues(program) {
	const out = [];
	const top = program.body[0];
	if (top === undefined) {
		return out;
	}

	const visit = (node, path) => {
		if (node.type === "TOMLValue") {
			out.push({ path, node });
			return;
		}
		if (node.type === "TOMLArray") {
			node.elements.forEach((el, i) => visit(el, [...path, `[${i}]`]));
			return;
		}
		if (node.type === "TOMLInlineTable") {
			for (const kv of node.body) {
				visit(kv.value, [...path, ...keySegments(kv.key)]);
			}
		}
	};

	for (const node of top.body) {
		if (node.type === "TOMLKeyValue") {
			visit(node.value, keySegments(node.key));
		} else {
			// Table or array-of-tables: prefix by the (string) resolved key.
			const prefix = node.resolvedKey.map(String);
			for (const kv of node.body) {
				visit(kv.value, [...prefix, ...keySegments(kv.key)]);
			}
		}
	}
	return out;
}

function pathsEqual(a, b) {
	return a.length === b.length && a.every((s, i) => s === b[i]);
}

/**
 * Locate the KeyValue node for a resolved dotted path across the root block and
 * all [table] / [[array-of-tables]] headers. Returns { kv, container } where
 * container is the owning table node (or undefined for the root block).
 * First match wins (array-of-tables: first entry).
 */
export function locateKeyValue(program, parts) {
	const top = program.body[0];
	if (top === undefined) {
		return undefined;
	}
	for (const node of top.body) {
		if (node.type === "TOMLKeyValue") {
			if (pathsEqual(keySegments(node.key), parts)) {
				return { kv: node, container: undefined };
			}
			continue;
		}
		const prefix = node.resolvedKey.map(String);
		if (parts.length <= prefix.length) {
			continue;
		}
		if (!prefix.every((s, i) => s === parts[i])) {
			continue;
		}
		const rel = parts.slice(prefix.length);
		for (const kv of node.body) {
			if (pathsEqual(keySegments(kv.key), rel)) {
				return { kv, container: node };
			}
		}
	}
	return undefined;
}

/** Offset of the start of the line containing `index`. */
function lineStart(text, index) {
	const nl = text.lastIndexOf("\n", index - 1);
	return nl === -1 ? 0 : nl + 1;
}

/** Offset just past the end of the line containing `index` (past its newline). */
function lineEnd(text, index) {
	const nl = text.indexOf("\n", index);
	return nl === -1 ? text.length : nl + 1;
}

function eolOf(text) {
	return text.includes("\r\n") ? "\r\n" : "\n";
}

// --- The four targeted edit primitives (pure range splices) ---

/** Replace one scalar value's lexeme in place with `newLexeme`. */
export function replaceValue(text, program, parts, newLexeme) {
	const found = locateKeyValue(program, parts);
	if (found === undefined) {
		throw new Error(`replaceValue: key not found: ${parts.join(".")}`);
	}
	const [s, e] = found.kv.value.range;
	return text.slice(0, s) + newLexeme + text.slice(e);
}

/** Rename a key (the whole dotted key node) to `newKeyText`, value untouched. */
export function renameKey(text, program, parts, newKeyText) {
	const found = locateKeyValue(program, parts);
	if (found === undefined) {
		throw new Error(`renameKey: key not found: ${parts.join(".")}`);
	}
	const [s, e] = found.kv.key.range;
	return text.slice(0, s) + newKeyText + text.slice(e);
}

/**
 * Append `key = value` at the end of an existing table's key block (or the root
 * block when `tableParts` is empty), inserting a full line after the last
 * key-value in that block.
 */
export function appendKey(text, program, tableParts, keyText, valueLexeme) {
	const eol = eolOf(text);
	const line = `${keyText} = ${valueLexeme}${eol}`;
	const top = program.body[0];
	let lastKv;
	if (tableParts.length === 0) {
		for (const node of top.body) {
			if (node.type === "TOMLKeyValue") {
				lastKv = node;
			}
		}
	} else {
		for (const node of top.body) {
			if (node.type === "TOMLKeyValue") {
				continue;
			}
			if (pathsEqual(node.resolvedKey.map(String), tableParts)) {
				if (node.body.length > 0) {
					lastKv = node.body[node.body.length - 1];
				}
			}
		}
	}
	if (lastKv === undefined) {
		throw new Error(`appendKey: no anchor key in table ${tableParts.join(".")}`);
	}
	const at = lineEnd(text, lastKv.range[1]);
	const head = text.slice(0, at);
	const terminator = head.endsWith("\n") ? "" : eol;
	return head + terminator + line + text.slice(at);
}

/** Delete a key and its value by removing the whole line it lives on. */
export function deleteKeyLine(text, program, parts) {
	const found = locateKeyValue(program, parts);
	if (found === undefined) {
		throw new Error(`deleteKeyLine: key not found: ${parts.join(".")}`);
	}
	const start = lineStart(text, found.kv.range[0]);
	const end = lineEnd(text, found.kv.value.range[1]);
	return text.slice(0, start) + text.slice(end);
}
