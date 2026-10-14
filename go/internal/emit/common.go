package emit

import (
	"sort"

	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/schema"
)

// namedTypeOrder returns every named type deterministically: the schema's
// declaration order first, then any remaining (imported) types sorted
// alphabetically.
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
