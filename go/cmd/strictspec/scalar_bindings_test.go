package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const identSchema = `name = "Named"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Named"

[types.Named]
type = "record"

[types.Named.fields.format_version]
type = "integer"
required = true

[types.Named.fields.id]
type = "ident"
required = true
`

func scalarManifest(bindings string) string {
	return `format_version = 1

[[scalars]]
name = "ident"
base = "string"
lexeme_rule = "^[a-z]+$"
bindings = ` + bindings + `
rendering = { inherits = "string" }

[[schemas]]
path = "named.schema.toml"
  [[schemas.targets]]
  lang = "go"
  output = "named_gen.go"
`
}

// TestScalarBindingsRefuseRemovedLanguages asserts that a custom scalar binding
// a language strictspec no longer has (python or ts) is refused by gen, check,
// and validate, with a message naming go as the only binding language, and that
// the fix the message names (dropping the binding) clears the refusal.
func TestScalarBindingsRefuseRemovedLanguages(t *testing.T) {
	for _, lang := range []string{"python", "ts"} {
		t.Run(lang, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "named.schema.toml"), identSchema)
			writeFile(t, filepath.Join(dir, "doc.json"), `{"format_version": 1, "id": "abc"}`)
			manifest := filepath.Join(dir, "strictspec.toml")
			writeFile(t, manifest, scalarManifest(`{ go = "string", `+lang+` = "string" }`))

			want := `scalars[0] (ident) declares a ` + lang + ` binding: the ` + lang + ` binding was removed; the only binding language is "go"`
			runs := [][]string{
				{"gen", "--manifest", manifest},
				{"check", "--manifest", manifest},
				{"validate", filepath.Join(dir, "named.schema.toml"), filepath.Join(dir, "doc.json"), "--structural-only"},
			}
			for _, argv := range runs {
				r := newApp().Test(argv)
				if r.ExitCode == 0 {
					t.Fatalf("%s with a %s binding must be refused", argv[0], lang)
				}
				if !strings.Contains(r.Stderr, want) {
					t.Errorf("%s: expected %q, got:\n%s", argv[0], want, r.Stderr)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "named_gen.go")); err == nil {
				t.Fatal("a refused gen must write nothing")
			}

			writeFile(t, manifest, scalarManifest(`{ go = "string" }`))
			for _, argv := range runs {
				if r := newApp().Test(argv); r.ExitCode != 0 {
					t.Fatalf("%s after dropping the %s binding: exit %d: %s", argv[0], lang, r.ExitCode, r.Stderr)
				}
			}
		})
	}
}
