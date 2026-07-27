// Package migrate is the strictspec MIGRATION ENGINE (spec/DESIGN.md —
// Versioning, migrations, and the version-boundary invariant). It parses
// migration files in the pinned surface (appendix-surface-syntax.md §9),
// implements the CLOSED 13-op set with the constitution's per-op semantics and
// collision rules, applies a CHAIN (N -> N+1 -> ...) to reach the current
// format_version, and revalidates the output by construction (a migration that
// produces an invalid document is a hard error with the validation diagnostics
// attached).
//
// Admission criterion (stated once, spec/DESIGN.md): ops may move, rename,
// reshape, delete, and inject literal values; NO op computes a new value from an
// existing value; predicates test field equality and presence/absence only.
// Values are never computed from values — every value at N+1 is a verbatim
// carry-over, an injected literal, or absent.
package migrate

import "github.com/smm-h/strictspec/go/internal/doc"

// The closed 13-op set (spec/DESIGN.md; appendix-error-codes.md §17).
const (
	OpAddField        = "add_field"
	OpRemoveField     = "remove_field"
	OpRenameField     = "rename_field"
	OpMoveField       = "move_field"
	OpSetValue        = "set_value"
	OpSetValueWhere   = "set_value_where"
	OpRemoveWhere     = "remove_where"
	OpAddCollection   = "add_collection"
	OpDropCollection  = "drop_collection"
	OpAppend          = "append"
	OpMergeDefaults   = "merge_defaults"
	OpWrapInArray     = "wrap_in_array"
	OpUnwrapSingleton = "unwrap_singleton"
)

// KnownOps is the closed op-name set; anything else is a migration-authoring
// error (STRICTSPEC_MIGRATE_UNKNOWN_SET is the set-level code; an unknown OP name
// is caught at authoring validation).
var KnownOps = map[string]bool{
	OpAddField: true, OpRemoveField: true, OpRenameField: true, OpMoveField: true,
	OpSetValue: true, OpSetValueWhere: true, OpRemoveWhere: true, OpAddCollection: true,
	OpDropCollection: true, OpAppend: true, OpMergeDefaults: true, OpWrapInArray: true,
	OpUnwrapSingleton: true,
}

// Reversibility taxonomy values (spec/DESIGN.md — Reversibility taxonomy).
const (
	DownTotal        = "total"
	DownPartial      = "partial"
	DownIrreversible = "irreversible"
)

// Op is one forward or inverse (down) migration op. Which fields are populated
// depends on Kind (see appendix-surface-syntax.md §9 op-key table).
type Op struct {
	Kind string

	// rename_field / move_field.
	From string
	To   string

	// positional ops (wrap/unwrap/set/add/remove/append/merge/collection).
	Path string

	// add_field / set_value / add_collection / append: the injected literal.
	Value    doc.Node
	HasValue bool

	// set_value_where / remove_where: the predicate; set_value_where also targets
	// Field within each matching element.
	Where *Cond
	Field string

	// merge_defaults: literal key/value pairs injected for ABSENT keys only.
	Defaults []doc.Entry

	// Per-op reversibility taxonomy (declared by the author).
	Down       string
	DownReason string
}

// Cond is a migration predicate (restricted to the §5.2 closed condition set:
// present, absent, equals, not-equals, in, not-in — equality and presence only).
type Cond struct {
	Field     string
	Predicate string
	Value     doc.Node
	Values    []doc.Node
}

// Migration is a parsed migration file: one single-version step (from -> to) with
// its forward ops and author-supplied inverse (down) ops.
type Migration struct {
	Schema      string
	From        int64
	To          int64
	Set         string
	Description string
	Ops         []Op
	DownOps     []Op
	SourcePath  string // for diagnostics
}

// DeclaredTaxonomy combines the per-op down declarations into the migration's
// overall taxonomy: irreversible if any op is irreversible, else partial if any
// op is partial, else total. An empty per-op declaration defaults to total.
func (m *Migration) DeclaredTaxonomy() string {
	worst := DownTotal
	for _, op := range m.Ops {
		switch op.Down {
		case DownIrreversible:
			return DownIrreversible
		case DownPartial:
			worst = DownPartial
		}
	}
	return worst
}
