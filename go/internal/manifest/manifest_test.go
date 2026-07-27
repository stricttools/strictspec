package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "strictspec.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A fully well-formed manifest must load cleanly (guards against over-rejection).
func TestLoadValid(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
[[schemas]]
path = "a.schema.toml"
[[schemas.targets]]
lang = "go"
output = "gen/a.go"
package = "gena"
`)
	m, err := Load(p)
	if err != nil {
		t.Fatalf("valid manifest should load: %v", err)
	}
	if len(m.Schemas) != 1 || len(m.Schemas[0].Targets) != 1 {
		t.Fatalf("unexpected parse: %+v", m)
	}
}

// A malformed [[schemas]] element (not a table) must be a HARD ERROR, never a
// silent skip. Before the fix the reader `continue`d past the bad element and
// succeeded because a sibling valid entry kept len(schemas) > 0.
func TestMalformedSchemaEntryIsHardError(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
schemas = [
  { path = "a.schema.toml", targets = [ { lang = "go", output = "gen/a.go" } ] },
  "this-is-not-a-table",
]
`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a non-table [[schemas]] element must be a hard error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "STRICTSPEC_TYPE_NOT_RECORD") {
		t.Fatalf("expected catalogued STRICTSPEC_TYPE_NOT_RECORD diagnostic, got: %v", err)
	}
}

// A malformed target element (not a table) must be a HARD ERROR.
func TestMalformedTargetIsHardError(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
schemas = [
  { path = "a.schema.toml", targets = [ { lang = "go", output = "gen/a.go" }, "bad-target" ] },
]
`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a non-table target element must be a hard error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "STRICTSPEC_TYPE_NOT_RECORD") {
		t.Fatalf("expected catalogued STRICTSPEC_TYPE_NOT_RECORD diagnostic, got: %v", err)
	}
}

// A target missing a required key (lang/output) must be a catalogued hard error.
func TestTargetMissingKeyIsHardError(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
[[schemas]]
path = "a.schema.toml"
[[schemas.targets]]
lang = "go"
`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a target missing 'output' must be a hard error")
	}
	if !strings.Contains(err.Error(), "STRICTSPEC_TYPE_MISSING_REQUIRED") {
		t.Fatalf("expected STRICTSPEC_TYPE_MISSING_REQUIRED, got: %v", err)
	}
}

// Stores and channels drive boundary-checkpoint code generation (spec/DESIGN.md,
// version-boundary invariant) that this toolchain build does not yet emit.
// Declaring them and getting no generated code is silent degradation, so their
// presence must be a HARD ERROR — never ignored.
func TestStoresHardError(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
[[schemas]]
path = "a.schema.toml"
[[schemas.targets]]
lang = "go"
output = "gen/a.go"
[[stores]]
name = "db"
kind = "postgres"
`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a declared store must be a hard error while boundary-checkpoint generation is unavailable")
	}
	if !strings.Contains(err.Error(), "stores") {
		t.Fatalf("error must name the unsupported construct, got: %v", err)
	}
}

func TestChannelsHardError(t *testing.T) {
	p := writeManifest(t, `
format_version = 1
[[schemas]]
path = "a.schema.toml"
[[schemas.targets]]
lang = "go"
output = "gen/a.go"
[[channels]]
name = "events"
`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a declared channel must be a hard error while boundary-checkpoint generation is unavailable")
	}
	if !strings.Contains(err.Error(), "channels") {
		t.Fatalf("error must name the unsupported construct, got: %v", err)
	}
}
