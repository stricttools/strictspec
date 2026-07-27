package migrate

import (
	"fmt"
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

func decodeTOMLString(lexeme string) string { return strdecode.TOML(lexeme) }

// closedPredicates is the §5.2 closed condition set migration predicates are
// restricted to (equality and presence only).
var closedPredicates = map[string]bool{
	"present": true, "absent": true, "equals": true,
	"not-equals": true, "in": true, "not-in": true,
}

// opRequiredKeys names the required keys per op for authoring validation.
var opRequiredKeys = map[string][]string{
	OpAddField:        {"path", "value"},
	OpRemoveField:     {"path"},
	OpRenameField:     {"from", "to"},
	OpMoveField:       {"from", "to"},
	OpSetValue:        {"path", "value"},
	OpSetValueWhere:   {"path", "where", "field", "value"},
	OpRemoveWhere:     {"path", "where"},
	OpAddCollection:   {"path", "value"},
	OpDropCollection:  {"path"},
	OpAppend:          {"path", "value"},
	OpMergeDefaults:   {"path", "defaults"},
	OpWrapInArray:     {"path"},
	OpUnwrapSingleton: {"path"},
}

// opEnumList is the rendered enum list for a bad `op` value diagnostic.
var opEnumList = []string{
	OpAddField, OpRemoveField, OpRenameField, OpMoveField, OpSetValue, OpSetValueWhere,
	OpRemoveWhere, OpAddCollection, OpDropCollection, OpAppend, OpMergeDefaults,
	OpWrapInArray, OpUnwrapSingleton,
}

// ParseMigration parses and AUTHORING-VALIDATES a migration file (TOML surface,
// appendix-surface-syntax.md §9). It returns the typed Migration, the ordered
// authoring diagnostics (empty when the file is well-formed), and a hard error
// only for an unparseable file.
func ParseMigration(src []byte, sourcePath string) (*Migration, []diag.Diagnostic, error) {
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return nil, nil, perr
	}
	root := d.Root
	if root == nil || root.Kind() != doc.Record {
		return nil, nil, fmt.Errorf("migration %s: root is not a table", sourcePath)
	}
	m := &Migration{SourcePath: sourcePath}
	var diags diag.Diagnostics

	mig, ok := entryOf(root, "migration")
	if !ok || mig.Kind() != doc.Record {
		diags.EmitCode("STRICTSPEC_TYPE_MISSING_REQUIRED", diag.NewPath(),
			map[string]diag.Slot{"key": diag.SlotString{S: "migration"}})
		return m, diags.All(), nil
	}
	m.Schema = strOr(mig, "schema")
	m.Set = strOr(mig, "migration_set")
	m.Description = strOr(mig, "description")
	m.From = intOr(mig, "from_format_version")
	m.To = intOr(mig, "to_format_version")

	if opsNode, ok := entryOf(root, "ops"); ok {
		for i, on := range items(opsNode) {
			sp := diag.NewPath(diag.Key{Name: "ops"}, diag.Index{N: i})
			op, ok := parseOp(on, sp, &diags)
			if ok {
				m.Ops = append(m.Ops, op)
			}
		}
	}
	if downNode, ok := entryOf(root, "down_ops"); ok {
		for i, on := range items(downNode) {
			sp := diag.NewPath(diag.Key{Name: "down_ops"}, diag.Index{N: i})
			op, ok := parseOp(on, sp, &diags)
			if ok {
				m.DownOps = append(m.DownOps, op)
			}
		}
	}
	return m, diags.All(), nil
}

func parseOp(node doc.Node, sp diag.Path, diags *diag.Diagnostics) (Op, bool) {
	if node.Kind() != doc.Record {
		diags.EmitCode("STRICTSPEC_TYPE_NOT_RECORD", sp,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKind(node)}})
		return Op{}, false
	}
	kind := strOr(node, "op")
	if !KnownOps[kind] {
		diags.EmitCode("STRICTSPEC_TYPE_NOT_ENUM_MEMBER", appendKey(sp, "op"),
			map[string]diag.Slot{
				"got":      diag.SlotValue{V: diag.StringVal(kind)},
				"expected": diag.SlotList{Elems: stringVals(opEnumList)},
			})
		return Op{}, false
	}
	op := Op{
		Kind:       kind,
		From:       strOr(node, "from"),
		To:         strOr(node, "to"),
		Path:       strOr(node, "path"),
		Field:      strOr(node, "field"),
		Down:       strOr(node, "down"),
		DownReason: strOr(node, "down_partial_reason"),
	}
	if v, ok := entryOf(node, "value"); ok {
		op.Value = v
		op.HasValue = true
	}
	if defs, ok := entryOf(node, "defaults"); ok && defs.Kind() == doc.Record {
		op.Defaults = defs.Entries()
	}
	if w, ok := entryOf(node, "where"); ok && w.Kind() == doc.Record {
		op.Where = parseCond(w, appendKey(sp, "where"), diags)
	}

	// Required-key authoring check.
	for _, key := range opRequiredKeys[kind] {
		if !hasKey(node, key) {
			diags.EmitCode("STRICTSPEC_TYPE_MISSING_REQUIRED", sp,
				map[string]diag.Slot{"key": diag.SlotString{S: key}})
		}
	}
	return op, true
}

func parseCond(w doc.Node, sp diag.Path, diags *diag.Diagnostics) *Cond {
	c := &Cond{Field: strOr(w, "field"), Predicate: strOr(w, "predicate")}
	if !closedPredicates[c.Predicate] {
		// A predicate outside the closed equality/presence set (e.g. a numeric
		// comparison) is the admission-criterion refusal.
		diags.EmitCode("STRICTSPEC_MIGRATE_PREDICATE_UNSUPPORTED", sp, nil)
	}
	if v, ok := entryOf(w, "value"); ok {
		c.Value = v
	}
	for _, v := range items(child(w, "values")) {
		c.Values = append(c.Values, v)
	}
	return c
}

// --- node helpers -----------------------------------------------------------

func entryOf(rec doc.Node, key string) (doc.Node, bool) {
	if rec == nil || rec.Kind() != doc.Record {
		return nil, false
	}
	for _, e := range rec.Entries() {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func hasKey(rec doc.Node, key string) bool {
	_, ok := entryOf(rec, key)
	return ok
}

func child(rec doc.Node, key string) doc.Node {
	n, _ := entryOf(rec, key)
	return n
}

func items(n doc.Node) []doc.Node {
	if n == nil || n.Kind() != doc.Array {
		return nil
	}
	return n.Items()
}

func strOr(rec doc.Node, key string) string {
	n, ok := entryOf(rec, key)
	if !ok || n.Kind() != doc.String {
		return ""
	}
	return decodeTOMLString(n.Lexeme())
}

func intOr(rec doc.Node, key string) int64 {
	n, ok := entryOf(rec, key)
	if !ok || n.Kind() != doc.Integer {
		return 0
	}
	v, _ := strconv.ParseInt(n.Lexeme(), 10, 64)
	return v
}

func appendKey(p diag.Path, key string) diag.Path {
	steps := make([]diag.Step, len(p.Steps)+1)
	copy(steps, p.Steps)
	steps[len(p.Steps)] = diag.Key{Name: key}
	return diag.Path{Steps: steps, Anchor: p.Anchor}
}

func stringVals(ss []string) []diag.Value {
	out := make([]diag.Value, len(ss))
	for i, s := range ss {
		out[i] = diag.StringVal(s)
	}
	return out
}

func nodeKind(n doc.Node) string {
	switch n.Kind() {
	case doc.Record:
		return "record"
	case doc.Array:
		return "array"
	case doc.String:
		return "string"
	case doc.Integer:
		return "integer"
	case doc.Float:
		return "float"
	case doc.Bool:
		return "boolean"
	default:
		return n.Kind().String()
	}
}
