// Decode the RETAINED SOURCE LEXEME of a string scalar into its code points.
//
// A faithful port of go/internal/strdecode (and python _strdecode). The document
// model retains the exact source bytes of a scalar (quotes and escapes as
// written); validation that reads a string's VALUE (regex, length, enum
// membership, literal comparison, reference resolution, rendering) needs the
// decoded content, not the raw lexeme.
//
// Two entry points cover the two source syntaxes: JSON (double-quoted with the
// JSON escape set) and TOML (basic, literal, and their multiline forms). JSONL
// strings use the JSON decoder.

const REPLACEMENT = "�";

// Decode a JSON string lexeme (quotes + escapes) into its code points.
export function decodeJSON(lexeme: string): string {
	if (lexeme.length < 2 || lexeme[0] !== '"') {
		return lexeme;
	}
	return decodeEscaped(lexeme.slice(1, -1), false);
}

// Decode a TOML string lexeme into its code points, handling all four TOML
// string forms.
export function decodeTOML(lexeme: string): string {
	if (lexeme.startsWith('"""')) {
		let body = stripSuffix(stripPrefix(lexeme, '"""'), '"""');
		body = trimLeadingNewline(body);
		return decodeEscaped(body, true);
	}
	if (lexeme.startsWith("'''")) {
		const body = stripSuffix(stripPrefix(lexeme, "'''"), "'''");
		return trimLeadingNewline(body); // literal: no escapes
	}
	if (lexeme.startsWith('"')) {
		return decodeEscaped(stripSuffix(stripPrefix(lexeme, '"'), '"'), false);
	}
	if (lexeme.startsWith("'")) {
		return stripSuffix(stripPrefix(lexeme, "'"), "'"); // literal: no escapes
	}
	return lexeme;
}

function stripPrefix(s: string, prefix: string): string {
	return s.startsWith(prefix) ? s.slice(prefix.length) : s;
}

function stripSuffix(s: string, suffix: string): string {
	return s.endsWith(suffix) ? s.slice(0, s.length - suffix.length) : s;
}

function trimLeadingNewline(s: string): string {
	if (s.startsWith("\r\n")) {
		return s.slice(2);
	}
	if (s.startsWith("\n")) {
		return s.slice(1);
	}
	return s;
}

function decodeEscaped(s: string, multiline: boolean): string {
	if (!s.includes("\\")) {
		return s;
	}
	const out: string[] = [];
	let i = 0;
	const n = s.length;
	while (i < n) {
		const c = s[i] as string;
		if (c !== "\\") {
			out.push(c);
			i += 1;
			continue;
		}
		if (i + 1 >= n) {
			out.push(c);
			break;
		}
		const e = s[i + 1] as string;
		switch (e) {
			case '"':
				out.push('"');
				i += 2;
				break;
			case "\\":
				out.push("\\");
				i += 2;
				break;
			case "/":
				out.push("/");
				i += 2;
				break;
			case "b":
				out.push("\b");
				i += 2;
				break;
			case "f":
				out.push("\f");
				i += 2;
				break;
			case "n":
				out.push("\n");
				i += 2;
				break;
			case "r":
				out.push("\r");
				i += 2;
				break;
			case "t":
				out.push("\t");
				i += 2;
				break;
			case "u": {
				const r = hexN(s, i + 2, 4);
				if (r !== null) {
					out.push(r);
					i += 6;
				} else {
					out.push(c);
					i += 1;
				}
				break;
			}
			case "U": {
				const r = hexN(s, i + 2, 8);
				if (r !== null) {
					out.push(r);
					i += 10;
				} else {
					out.push(c);
					i += 1;
				}
				break;
			}
			default:
				if (
					multiline &&
					(e === "\n" || e === "\r" || e === " " || e === "\t")
				) {
					// Line-ending backslash: trim following whitespace incl. newline.
					let j = i + 1;
					while (j < n) {
						const cj = s[j] as string;
						if (cj === " " || cj === "\t" || cj === "\n" || cj === "\r") {
							j += 1;
						} else {
							break;
						}
					}
					i = j;
				} else {
					out.push(c);
					i += 1;
				}
				break;
		}
	}
	return out.join("");
}

function hexN(s: string, start: number, count: number): string | null {
	if (start + count > s.length) {
		return null;
	}
	let r = 0;
	for (let k = 0; k < count; k++) {
		const c = s[start + k] as string;
		let v: number;
		if (c >= "0" && c <= "9") {
			v = c.charCodeAt(0) - 0x30;
		} else if (c >= "a" && c <= "f") {
			v = c.charCodeAt(0) - 0x61 + 10;
		} else if (c >= "A" && c <= "F") {
			v = c.charCodeAt(0) - 0x41 + 10;
		} else {
			return null;
		}
		r = (r << 4) | v;
	}
	if (r > 0x10ffff || (r >= 0xd800 && r <= 0xdfff)) {
		return REPLACEMENT;
	}
	return String.fromCodePoint(r);
}
