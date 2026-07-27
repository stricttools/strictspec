package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smm-h/strictspec/go/internal/render"
)

const adapterMiniSchema = `
name = "point"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Point"

[types.Point]
type = "record"
[types.Point.fields.x]
type = "integer"
required = true
`

// writeSchema drops the mini schema in a temp dir and returns its path.
func writeSchema(t *testing.T, syntax string) string {
	t.Helper()
	dir := t.TempDir()
	src := adapterMiniSchema
	if syntax == "toml" {
		src = "\n" + `name = "point"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Point"

[types.Point]
type = "record"
[types.Point.fields.x]
type = "integer"
required = true
`
	}
	p := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAdapterJSONParseErrorRenders is the regression for the adapter's parse-error
// render panic: a malformed JSON input must surface STRICTSPEC_PARSE_JSON_SYNTAX
// and RENDER without panic. The JSON template carries no {line} slot, so binding
// `line` in parseDiag makes render.Render panic. This path was untested.
func TestAdapterJSONParseErrorRenders(t *testing.T) {
	diags, err := run(request{
		Schema:      writeSchema(t, "json"),
		InputInline: `{"format_version": 1, "x": }`,
		InputSyntax: "json",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_PARSE_JSON_SYNTAX" {
		t.Fatalf("expected PARSE_JSON_SYNTAX, got %+v", diags)
	}
	// writeResponse renders each diagnostic; render must not panic on this path.
	if msg := render.Render(diags[0]); msg == "" {
		t.Fatalf("parse diagnostic message must render, got empty")
	}
}

// TestAdapterTOMLParseErrorRenders is the same regression for TOML.
func TestAdapterTOMLParseErrorRenders(t *testing.T) {
	diags, err := run(request{
		Schema:      writeSchema(t, "toml"),
		InputInline: "x = = 3\n",
		InputSyntax: "toml",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_PARSE_TOML_SYNTAX" {
		t.Fatalf("expected PARSE_TOML_SYNTAX, got %+v", diags)
	}
	if msg := render.Render(diags[0]); msg == "" {
		t.Fatalf("parse diagnostic message must render, got empty")
	}
}
