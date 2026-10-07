package schema

import (
	"sort"

	"github.com/stricttools/strictspec/go/internal/diag"
)

// ArmCategory is the node category (scalar, record, or array) a node-kind
// union arm accepts, the category the runtime selects the arm by: a
// reference is followed to its named type, a builtin or custom scalar is a
// scalar, a record or map is a record, an array or tuple is an array, and
// every other type site is a scalar.
func (s *Schema) ArmCategory(t *Type) string {
	seen := 0
	for t != nil && t.Kind == KindRef {
		if named, ok := s.Types[t.Ref]; ok && seen < 32 {
			t = named
			seen++
			continue
		}
		return "scalar"
	}
	if t == nil {
		return "scalar"
	}
	switch t.Kind {
	case KindRecord, KindMap:
		return "record"
	case KindArray, KindTuple:
		return "array"
	default:
		return "scalar"
	}
}

// checkNodeKindUnions refuses every node-kind union with two arms of one node
// category, which the runtime could not tell apart: the first arm would take
// every input of that category. It runs once the named types, imported ones
// included, are resolvable.
func checkNodeKindUnions(s *Schema) []diag.Diagnostic {
	var out []diag.Diagnostic
	visited := map[*Type]bool{}
	var walk func(t *Type)
	walk = func(t *Type) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true
		if t.Kind == KindNodeKindUnion {
			arms := map[string]int{}
			for _, arm := range t.Arms {
				arms[s.ArmCategory(arm.Type)]++
			}
			var repeated []string
			for category, n := range arms {
				if n > 1 {
					repeated = append(repeated, category)
				}
			}
			sort.Strings(repeated)
			for _, category := range repeated {
				out = append(out, diag.Diagnostic{
					Code:  "STRICTSPEC_SCHEMA_NODE_KIND_UNION_AMBIGUOUS",
					Path:  t.SchemaPath,
					Slots: map[string]diag.Slot{"kind": diag.SlotString{S: category}},
				})
			}
		}
		for _, f := range t.Fields {
			walk(f.Type)
		}
		for _, arm := range t.Arms {
			walk(arm.Type)
		}
		walk(t.Value)
		walk(t.Item)
		walk(t.Inner)
	}
	names := make([]string, 0, len(s.Types))
	for name := range s.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walk(s.Types[name])
	}
	return out
}
