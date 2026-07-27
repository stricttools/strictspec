package schema

import (
	"path/filepath"
	"testing"

	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

func loadRoot(t *testing.T, rel string) (*Schema, []diagCode) {
	t.Helper()
	path := filepath.Join(repoRoot(t), rel)
	s, diags, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", rel, err)
	}
	out := make([]diagCode, len(diags))
	for i, d := range diags {
		out[i] = diagCode{d.Code, d.Path.Render()}
	}
	return s, out
}

type diagCode struct{ code, path string }

func TestMetaSchemaAuthoringDiagnostics(t *testing.T) {
	cases := []struct {
		rel  string
		code string
		path string
	}{
		{"conformance/fixtures/_inputs/meta/opaque-no-stance.toml",
			"STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE", "$.types.ToolSchema.fields.input_schema"},
		{"conformance/fixtures/_inputs/meta/unchecked-no-reason.toml",
			"STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON", "$.types.ToolSchema.fields.input_schema"},
		{"conformance/fixtures/_inputs/meta/types-with-constraint.toml",
			"STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT", "$"},
		{"conformance/fixtures/_inputs/meta/types-transitive.toml",
			"STRICTSPEC_IMPORT_TRANSITIVE", "$"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			_, diags := loadRoot(t, c.rel)
			if len(diags) != 1 {
				t.Fatalf("expected 1 authoring diagnostic, got %v", diags)
			}
			if diags[0].code != c.code {
				t.Errorf("code = %s, want %s", diags[0].code, c.code)
			}
			if diags[0].path != c.path {
				t.Errorf("path = %s, want %s", diags[0].path, c.path)
			}
		})
	}
}

func TestValidSchemaReadsClean(t *testing.T) {
	for _, rel := range []string{
		"conformance/fixtures/_schemas/wavescript-score.schema.toml",
		"conformance/fixtures/_schemas/predraw-scene.toml",
		"conformance/fixtures/_schemas/types-geometry.toml",
		"conformance/fixtures/_schemas/pgdesign-table.toml",
	} {
		_, diags := loadRoot(t, rel)
		if len(diags) != 0 {
			t.Errorf("%s: expected clean read, got %v", rel, diags)
		}
	}
}

func TestManifestScalars(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "conformance/fixtures/_schemas")
	scalars := LoadManifestScalars(dir)
	for _, name := range []string{"identifier", "pgtype", "sql-expression"} {
		if _, ok := scalars[name]; !ok {
			t.Errorf("custom scalar %s not registered from manifest", name)
		}
	}
	if scalars["identifier"].LexemeRule == "" {
		t.Error("identifier lexeme_rule not parsed")
	}
}

var _ = tomldoc.Parse
