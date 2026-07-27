package emit

import (
	"os"
	"path/filepath"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// Loaded is a schema prepared for emission: the resolved schema, its bound
// custom-scalar registry, the in-memory file set to embed (schema + imported
// type-definition files + any scalar-manifest siblings), the main-file key, and
// the authoring diagnostics (non-empty means the schema fails the meta-schema
// and must NOT be emitted).
type Loaded struct {
	Schema   *schema.Schema
	Scalars  map[string]*schema.Scalar
	Files    map[string]string
	MainFile string
	Diags    []diag.Diagnostic
}

// LoadForEmit reads a schema file from disk, resolves its imports, and gathers
// the file set the generated validator will embed. It performs the same
// import-resolution and scalar-discovery as the runtime, so the embedded set
// compiles identically in-memory.
func LoadForEmit(schemaPath string) (*Loaded, error) {
	dir := filepath.Dir(schemaPath)
	mainFile := filepath.Base(schemaPath)

	s, sdiags, err := schema.LoadFile(schemaPath)
	if err != nil {
		return nil, err
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)

	files := map[string]string{}
	mainSrc, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}
	files[mainFile] = string(mainSrc)

	// Embed imported type-definition files (keyed exactly as imports reference).
	for _, imp := range s.Imports {
		if _, done := files[imp.File]; done {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(dir, imp.File))
		if rerr != nil {
			continue // a missing import surfaces as an authoring diagnostic already
		}
		files[imp.File] = string(src)
	}

	// Embed scalar-manifest siblings (files carrying a top-level `scalars` array).
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		if _, done := files[e.Name()]; done {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		if declaresScalars(src) {
			files[e.Name()] = string(src)
		}
	}

	return &Loaded{
		Schema:   s,
		Scalars:  schema.LoadManifestScalars(dir),
		Files:    files,
		MainFile: mainFile,
		Diags:    sdiags,
	}, nil
}

// declaresScalars reports whether a TOML file has a top-level `scalars` array.
func declaresScalars(src []byte) bool {
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return false
	}
	if d.Root == nil || d.Root.Kind() != doc.Record {
		return false
	}
	for _, e := range d.Root.Entries() {
		if e.Key == "scalars" {
			return true
		}
	}
	return false
}
