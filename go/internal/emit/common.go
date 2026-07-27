package emit

import (
	"sort"
	"strings"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// namedTypeOrder returns every named type deterministically: the schema's
// declaration order first, then any remaining (imported) types sorted
// alphabetically. Shared by every emitter so the three targets walk the schema
// in one identical order.
func namedTypeOrder(s *schema.Schema) []string {
	seen := map[string]bool{}
	order := make([]string, 0, len(s.Types))
	for _, name := range s.TypeOrder {
		if _, ok := s.Types[name]; ok && !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range s.Types {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(order, rest...)
}

// isRecordType reports whether the named type is a record.
func isRecordType(s *schema.Schema, name string) bool {
	t, ok := s.Types[name]
	return ok && t != nil && t.Kind == schema.KindRecord
}

// isIntEnum reports whether an enum type's arms are all integer literals (so it
// binds to an integer carrier rather than a string one). A sourced enum always
// binds string.
func isIntEnum(t *schema.Type) bool {
	if t.Sourced {
		return false
	}
	for _, ev := range t.EnumValues {
		if ev.Kind != doc.Integer {
			return false
		}
	}
	return len(t.EnumValues) > 0
}

// escapeStringLiteral renders s as the body of a double-quoted string literal in
// the C-family escaping shared by Python and JS/TS (\\, \", \n, \r, \t, and
// \xHH for other control bytes; all other bytes pass through verbatim). Both
// languages accept \xHH, so one escaper serves both embedders. The result is
// deterministic (byte-for-byte stable across regenerations).
func escapeStringLiteral(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				b.WriteString(`\x`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

// identSafe converts a schema identifier into a lower-camel/snake-preserving
// identifier valid in Python and TS: every rune that is not a letter, digit, or
// underscore becomes an underscore, and a leading digit is prefixed with `x`. It
// preserves the original casing (unlike exportName, which CamelCases).
func identSafe(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "field"
	}
	if c := out[0]; c >= '0' && c <= '9' {
		return "x" + out
	}
	return out
}

// isJSIdent reports whether name is a bare JS/TS identifier (so an object/interface
// key can be emitted unquoted). Conservative: letters, digits, `_`, `$`, no
// leading digit.
func isJSIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		ok := r == '_' || r == '$' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
