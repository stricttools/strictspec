// Package strdecode decodes the RETAINED SOURCE LEXEME of a string scalar into
// its code-point value. The document model retains the exact source bytes of a
// scalar (quotes and escapes as written); validation that reads a string's VALUE
// (regex, length, enum membership, literal comparison, reference resolution, and
// message rendering) needs the decoded content, not the raw lexeme.
//
// Two entry points cover the two source syntaxes strictspec accepts as string
// lexemes: JSON (double-quoted with the JSON escape set) and TOML (basic,
// literal, and their multiline forms). JSONL strings use the JSON decoder.
package strdecode

import (
	"strings"
	"unicode/utf8"
)

// JSON decodes a JSON string lexeme (including the surrounding double quotes and
// any escape sequences) into its code-point value. A malformed lexeme (which the
// lossless parser would already have rejected) decodes best-effort.
func JSON(lexeme string) string {
	if len(lexeme) < 2 || lexeme[0] != '"' {
		return lexeme
	}
	return decodeEscaped(lexeme[1:len(lexeme)-1], false)
}

// TOML decodes a TOML string lexeme into its code-point value. It handles the
// four TOML string forms: basic (double-quoted), literal (single-quoted),
// multiline basic (triple double-quoted), and multiline literal (triple
// single-quoted).
func TOML(lexeme string) string {
	switch {
	case strings.HasPrefix(lexeme, `"""`):
		body := strings.TrimSuffix(strings.TrimPrefix(lexeme, `"""`), `"""`)
		body = trimLeadingNewline(body)
		return decodeEscaped(body, true)
	case strings.HasPrefix(lexeme, `'''`):
		body := strings.TrimSuffix(strings.TrimPrefix(lexeme, `'''`), `'''`)
		return trimLeadingNewline(body) // literal: no escapes
	case strings.HasPrefix(lexeme, `"`):
		return decodeEscaped(strings.TrimSuffix(strings.TrimPrefix(lexeme, `"`), `"`), false)
	case strings.HasPrefix(lexeme, `'`):
		return strings.TrimSuffix(strings.TrimPrefix(lexeme, `'`), `'`) // literal: no escapes
	default:
		return lexeme
	}
}

// trimLeadingNewline drops a single leading newline (LF or CRLF) — the TOML rule
// that a newline immediately after the opening multiline delimiter is trimmed.
func trimLeadingNewline(s string) string {
	if strings.HasPrefix(s, "\r\n") {
		return s[2:]
	}
	if strings.HasPrefix(s, "\n") {
		return s[1:]
	}
	return s
}

// decodeEscaped processes backslash escapes over the body of a basic string.
// When multiline is true, a backslash at end-of-line trims the following
// whitespace (the TOML line-ending backslash).
func decodeEscaped(s string, multiline bool) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(s) {
			b.WriteByte(c)
			break
		}
		e := s[i+1]
		switch e {
		case '"':
			b.WriteByte('"')
			i += 2
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '/':
			b.WriteByte('/')
			i += 2
		case 'b':
			b.WriteByte('\b')
			i += 2
		case 'f':
			b.WriteByte('\f')
			i += 2
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'u':
			if r, ok := hexN(s, i+2, 4); ok {
				b.WriteRune(r)
				i += 6
			} else {
				b.WriteByte(c)
				i++
			}
		case 'U':
			if r, ok := hexN(s, i+2, 8); ok {
				b.WriteRune(r)
				i += 10
			} else {
				b.WriteByte(c)
				i++
			}
		default:
			if multiline && (e == '\n' || e == '\r' || e == ' ' || e == '\t') {
				// Line-ending backslash: trim following whitespace incl. the newline.
				j := i + 1
				for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
					j++
				}
				i = j
			} else {
				b.WriteByte(c)
				i++
			}
		}
	}
	return b.String()
}

func hexN(s string, start, n int) (rune, bool) {
	if start+n > len(s) {
		return 0, false
	}
	var r rune
	for k := 0; k < n; k++ {
		c := s[start+k]
		var v rune
		switch {
		case c >= '0' && c <= '9':
			v = rune(c - '0')
		case c >= 'a' && c <= 'f':
			v = rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = rune(c-'A') + 10
		default:
			return 0, false
		}
		r = r<<4 | v
	}
	if !utf8.ValidRune(r) {
		return utf8.RuneError, true
	}
	return r, true
}
