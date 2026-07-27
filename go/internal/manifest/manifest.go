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

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

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
		return nil, fmt.Errorf("manifest %s: missing or non-integer format_version", path)
	}
	schemas, ok := entryOf(root, "schemas")
	if !ok || schemas.Kind() != doc.Array {
		return nil, fmt.Errorf("manifest %s: missing [[schemas]] array", path)
	}
	for _, s := range schemas.Items() {
		if s.Kind() != doc.Record {
			continue
		}
		se := SchemaEntry{Path: strOf(s, "path")}
		if se.Path == "" {
			return nil, fmt.Errorf("manifest %s: a [[schemas]] entry has no path", path)
		}
		if ts, ok := entryOf(s, "targets"); ok && ts.Kind() == doc.Array {
			for _, t := range ts.Items() {
				if t.Kind() != doc.Record {
					continue
				}
				te := TargetEntry{
					Lang:    strOf(t, "lang"),
					Output:  strOf(t, "output"),
					Package: strOf(t, "package"),
				}
				if te.Lang == "" || te.Output == "" {
					return nil, fmt.Errorf("manifest %s: target for %s missing lang or output", path, se.Path)
				}
				se.Targets = append(se.Targets, te)
			}
		}
		m.Schemas = append(m.Schemas, se)
	}
	if len(m.Schemas) == 0 {
		return nil, fmt.Errorf("manifest %s: declares no schemas", path)
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
