package schema

import (
	"os"
	"path/filepath"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// LoadFile reads and parses a schema/type-definition file from disk into a
// typed Schema plus its authoring diagnostics. Schemas are always TOML.
func LoadFile(path string) (*Schema, []diag.Diagnostic, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return nil, nil, perr
	}
	s, diags := ReadSchema(d.Root, filepath.Dir(path))
	return s, diags, nil
}

// ResolveImports loads the schema's imported type-definition files (relative to
// the schema's directory) and merges the named imported types into s.Types.
// Imported-file authoring diagnostics are surfaced. Only the type NAMES listed
// in each import entry are pulled in.
func ResolveImports(s *Schema) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, imp := range s.Imports {
		path := filepath.Join(s.Dir, imp.File)
		ts, tdiags, err := LoadFile(path)
		if err != nil {
			// Missing type-definition file.
			out = append(out, diag.Diagnostic{
				Code: "STRICTSPEC_IMPORT_MISSING_TYPE_FILE",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"file":   diag.SlotString{S: imp.File},
					"schema": diag.SlotIdentifier{Name: s.Name},
				},
			})
			continue
		}
		out = append(out, tdiags...)
		for _, name := range imp.Types {
			if _, ok := ts.Types[name]; !ok {
				out = append(out, diag.Diagnostic{
					Code: "STRICTSPEC_IMPORT_UNKNOWN_TYPE",
					Path: diag.NewPath(),
					Slots: map[string]diag.Slot{
						"name": diag.SlotIdentifier{Name: name},
						"file": diag.SlotString{S: imp.File},
					},
				})
			}
		}
		// Merge ALL of the file's types into the resolution pool: an imported type
		// (e.g. Shape) may reference sibling types from the same file (e.g.
		// PositiveInt) that the importer did not list explicitly. Making the whole
		// file's types resolvable is a semantics-preserving interpreter choice
		// (name-resolution only; no cross-file constraints are pulled in).
		for name, t := range ts.Types {
			if _, exists := s.Types[name]; !exists {
				s.Types[name] = t
			}
		}
	}
	return out
}

// LoadManifestScalars scans dir for TOML files declaring `[[scalars]]` (the
// consumer manifest's custom-scalar registrations, appendix-custom-scalars.md)
// and returns the merged registry keyed by scalar name. A custom scalar travels
// with the schema via the sibling manifest; this discovers it by convention.
func LoadManifestScalars(dir string) map[string]*Scalar {
	out := map[string]*Scalar{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			continue
		}
		sc, ok := entryOf(d.Root, "scalars")
		if !ok {
			continue
		}
		for _, s := range items(sc) {
			cs := &Scalar{
				Name:       strOr(s, "name"),
				Base:       strOr(s, "base"),
				LexemeRule: strOr(s, "lexeme_rule"),
			}
			if length, ok := entryOf(s, "length"); ok {
				if mn, ok := entryOf(length, "min"); ok {
					cs.LenMin = intPtr(mn)
				}
				if mx, ok := entryOf(length, "max"); ok {
					cs.LenMax = intPtr(mx)
				}
				if ne, ok := entryOf(length, "non_empty"); ok {
					cs.NonEmpty = ne.Lexeme() == "true"
				}
			}
			if cs.Name != "" {
				out[cs.Name] = cs
			}
		}
	}
	return out
}
