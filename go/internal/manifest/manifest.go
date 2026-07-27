// Package manifest reads the consumer manifest (strictspec.toml): the
// file-driven input to `strictspec gen` and `strictspec check`. The manifest
// declares the consumer's schema files and, per schema, the generation targets
// (language + output path + package/module name). It carries a format_version
// and is itself a document of a toolchain-shipped built-in schema (spec/DESIGN.md
// — Meta-schema); this reader parses the pinned surface into typed structs.
package manifest

import (
	"fmt"
	"os"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// diagErr renders a catalogued diagnostic as a manifest hard error. The manifest
// is a document of a toolchain-shipped built-in schema (spec/DESIGN.md — Manifest;
// appendix-error-codes.md §20: general manifest-schema violations reuse the
// STRICTSPEC_TYPE_* / STRICTSPEC_SCHEMA_* codes), so structural violations surface
// as catalogued diagnostics rather than ad-hoc prose. Malformed entries are never
// skipped — the hard-error philosophy admits no silent `continue`.
func diagErr(manifestPath string, d diag.Diagnostic) error {
	return fmt.Errorf("manifest %s: %s at %s: %s",
		manifestPath, d.Code, d.Path.Render(), render.Render(d))
}

// Manifest is a parsed strictspec.toml.
type Manifest struct {
	FormatVersion int64
	Schemas       []SchemaEntry
}

// SchemaEntry declares one schema file and its generation targets.
type SchemaEntry struct {
	Path    string
	Targets []TargetEntry
}

// TargetEntry declares one generation target for a schema.
type TargetEntry struct {
	Lang    string // "go" | "python" | "ts"
	Output  string // output path, relative to the manifest directory
	Package string // Go package / Python module / TS module name
}

// Load reads and parses a manifest file. A parse error or a structurally
// invalid manifest is a hard error.
func Load(path string) (*Manifest, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, perr)
	}
	root := d.Root
	if root == nil || root.Kind() != doc.Record {
		return nil, fmt.Errorf("manifest %s: root is not a table", path)
	}
	m := &Manifest{}
	if fv, ok := entryOf(root, "format_version"); ok && fv.Kind() == doc.Integer {
		m.FormatVersion = intOf(fv)
	} else {
		return nil, diagErr(path, diag.Diagnostic{
			Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
			Path:  diag.NewPath(),
			Slots: map[string]diag.Slot{"key": diag.SlotString{S: "format_version"}},
		})
	}

	// Boundary-checkpoint constructs (stores / channels) drive GENERATED ingest
	// write-doors and channel wrappers (spec/DESIGN.md — version-boundary
	// invariant); they are NOT declarative-only. This toolchain build does not
	// emit that code yet, so declaring one and silently getting nothing is
	// degradation. Their presence is a hard error naming the construct — never
	// ignored.
	for _, key := range []string{"stores", "channels"} {
		if _, ok := entryOf(root, key); ok {
			return nil, fmt.Errorf(
				"manifest %s: declares %q, but boundary-checkpoint generation "+
					"(ingest write-doors / channel wrappers) is not available in this "+
					"toolchain build; remove the %s declaration or use a build that emits it",
				path, key, key)
		}
	}

	schemas, ok := entryOf(root, "schemas")
	if !ok || schemas.Kind() != doc.Array {
		return nil, diagErr(path, diag.Diagnostic{
			Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
			Path:  diag.NewPath(),
			Slots: map[string]diag.Slot{"key": diag.SlotString{S: "schemas"}},
		})
	}
	for i, s := range schemas.Items() {
		spath := diag.NewPath(diag.Key{Name: "schemas"}, diag.Index{N: i})
		if s.Kind() != doc.Record {
			return nil, diagErr(path, diag.Diagnostic{
				Code:  "STRICTSPEC_TYPE_NOT_RECORD",
				Path:  spath,
				Slots: map[string]diag.Slot{"got": diag.SlotString{S: s.Kind().String()}},
			})
		}
		se := SchemaEntry{Path: strOf(s, "path")}
		if se.Path == "" {
			return nil, diagErr(path, diag.Diagnostic{
				Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
				Path:  spath,
				Slots: map[string]diag.Slot{"key": diag.SlotString{S: "path"}},
			})
		}
		if ts, ok := entryOf(s, "targets"); ok && ts.Kind() == doc.Array {
			for j, t := range ts.Items() {
				tpath := diag.NewPath(
					diag.Key{Name: "schemas"}, diag.Index{N: i},
					diag.Key{Name: "targets"}, diag.Index{N: j},
				)
				if t.Kind() != doc.Record {
					return nil, diagErr(path, diag.Diagnostic{
						Code:  "STRICTSPEC_TYPE_NOT_RECORD",
						Path:  tpath,
						Slots: map[string]diag.Slot{"got": diag.SlotString{S: t.Kind().String()}},
					})
				}
				te := TargetEntry{
					Lang:    strOf(t, "lang"),
					Output:  strOf(t, "output"),
					Package: strOf(t, "package"),
				}
				if te.Lang == "" {
					return nil, diagErr(path, diag.Diagnostic{
						Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
						Path:  tpath,
						Slots: map[string]diag.Slot{"key": diag.SlotString{S: "lang"}},
					})
				}
				if te.Output == "" {
					return nil, diagErr(path, diag.Diagnostic{
						Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
						Path:  tpath,
						Slots: map[string]diag.Slot{"key": diag.SlotString{S: "output"}},
					})
				}
				se.Targets = append(se.Targets, te)
			}
		}
		m.Schemas = append(m.Schemas, se)
	}
	if len(m.Schemas) == 0 {
		return nil, diagErr(path, diag.Diagnostic{
			Code:  "STRICTSPEC_TYPE_MISSING_REQUIRED",
			Path:  diag.NewPath(),
			Slots: map[string]diag.Slot{"key": diag.SlotString{S: "schemas"}},
		})
	}
	return m, nil
}

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

func strOf(rec doc.Node, key string) string {
	n, ok := entryOf(rec, key)
	if !ok || n.Kind() != doc.String {
		return ""
	}
	return strdecode.TOML(n.Lexeme())
}

func intOf(n doc.Node) int64 {
	var v int64
	for _, c := range n.Lexeme() {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int64(c-'0')
	}
	return v
}
