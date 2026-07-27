package ir

import (
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// evalCondition evaluates a gate condition (closed six-kind set) against a record.
// Value-testing predicates read only the WRITTEN value; an absent field makes
// every value predicate FALSE (there is no default — decision 30).
func (v *exec) evalCondition(rec doc.Node, c *schema.Condition) bool {
	if c == nil {
		return false
	}
	fn, present := entryOf(rec, c.Field)
	switch c.Predicate {
	case "present":
		return present
	case "absent":
		return !present
	case "equals":
		return present && v.sameScalar(c.Value, fn)
	case "not-equals":
		return present && !v.sameScalar(c.Value, fn)
	case "in":
		if !present {
			return false
		}
		for _, val := range c.Values {
			if v.sameScalar(val, fn) {
				return true
			}
		}
		return false
	case "not-in":
		if !present {
			return false
		}
		for _, val := range c.Values {
			if v.sameScalar(val, fn) {
				return false
			}
		}
		return true
	}
	return false
}

// renderCondition composes the `{condition}` slot per appendix-rendering Part D.
func renderCondition(c *schema.Condition) string {
	if c == nil {
		return ""
	}
	switch c.Predicate {
	case "present":
		return c.Field + " present"
	case "absent":
		return c.Field + " absent"
	case "equals":
		return c.Field + " == " + renderLiteral(c.Value)
	case "not-equals":
		return c.Field + " != " + renderLiteral(c.Value)
	case "in":
		return c.Field + " in [" + joinLiterals(c.Values) + "]"
	case "not-in":
		return c.Field + " not in [" + joinLiterals(c.Values) + "]"
	}
	return ""
}

func joinLiterals(vals []schema.SVal) string {
	parts := make([]string, len(vals))
	for i, sv := range vals {
		parts[i] = renderLiteral(sv)
	}
	return strings.Join(parts, ", ")
}

// renderLiteral renders a schema literal per A.1 (strings double-quoted and
// A.2-escaped; integers/floats from their lexeme; booleans lowercase).
func renderLiteral(sv schema.SVal) string {
	switch sv.Kind {
	case doc.String:
		return `"` + diag.EscapeString(sv.Str) + `"`
	case doc.Bool:
		if sv.Bool {
			return "true"
		}
		return "false"
	default:
		return sv.Lexeme
	}
}
