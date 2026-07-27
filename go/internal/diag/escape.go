package diag

import "strings"

// identRe describes an identifier-shaped token per the path grammar
// (appendix-rendering.md Part B, `ident`) and the A.5 identifier-shaped rule:
// [A-Za-z_][A-Za-z0-9_-]* .
//
// IsIdentShaped reports whether s is identifier-shaped. Identifier-shaped keys
// render bare (`.name`); non-identifier-shaped keys switch to the quoted map-key
// form (`["name"]`), and identifier-shaped container keys / did-you-mean
// candidates render bare while others are double-quoted.
func IsIdentShaped(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || isASCIILetter(r)) {
				return false
			}
			continue
		}
		if !(r == '_' || r == '-' || isASCIILetter(r) || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

// EscapeString applies the A.2 string-escaping table (appendix-rendering.md).
// It does NOT add surrounding quotes and never truncates: it is the shared
// escape primitive used by path map-key rendering (never truncated) and by the
// diagnostic string renderer (which truncates first, then escapes). Exactly the
// A.2 escapes are produced and nothing else; all other code points — including
// non-ASCII — are emitted verbatim as UTF-8.
func EscapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r <= 0x1f {
				// Other control chars U+0000–U+001F: \u00XX, lowercase hex, four digits.
				const hexdigits = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hexdigits[(r>>4)&0xf])
				b.WriteByte(hexdigits[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
