package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A count-limit cross-document constraint (documents-in(services/*.toml) <= 1)
// must fire when --collection hosts a two-document evidence set that exceeds the
// limit. This exercises the in-process collection-evidence path end to end.
const fleetCountLimitSchema = `
name = "fleet"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Fleet"

[types.Fleet]
type = "record"
[types.Fleet.fields.name]
type = "string"
required = true
non_empty = true

[[types.Fleet.constraints]]
form = "count-limit"
selection = "documents-in(services/*.toml)"
compare = "<="
limit = 1
`

func writeFleetFixture(t *testing.T) (dir, schema, fleet string) {
	t.Helper()
	dir = t.TempDir()
	schema = filepath.Join(dir, "fleet.schema.toml")
	writeFile(t, schema, fleetCountLimitSchema)
	fleet = filepath.Join(dir, "fleet.toml")
	writeFile(t, fleet, "format_version = 1\nname = \"prod\"\n")
	svc := filepath.Join(dir, "services")
	if err := os.MkdirAll(svc, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(svc, "s1.toml"), "format_version = 1\nname = \"s1\"\n")
	writeFile(t, filepath.Join(svc, "s2.toml"), "format_version = 1\nname = \"s2\"\n")
	return dir, schema, fleet
}

func TestValidateCollectionCountLimitViolation(t *testing.T) {
	dir, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{
		"validate", schema, fleet,
		"--with-domain-checks",
		"--collection", "services/*.toml",
		"--collection-root", dir,
	})
	if r.ExitCode == 0 {
		t.Fatalf("count-limit (<=1) with two collection documents must fail; stdout:\n%s", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "STRICTSPEC_CROSS_COUNT_LIMIT") {
		t.Fatalf("expected STRICTSPEC_CROSS_COUNT_LIMIT, stderr:\n%s", r.Stderr)
	}
}

// The same schema WITHOUT --collection must be a hard error: a collection-shaped
// resolver that cannot be satisfied is never a silent skip.
func TestValidateDomainChecksWithoutCollectionIsHardError(t *testing.T) {
	_, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{
		"validate", schema, fleet,
		"--with-domain-checks",
	})
	if r.ExitCode == 0 {
		t.Fatal("--with-domain-checks with a collection-shaped resolver and no --collection must be a hard error")
	}
	if !strings.Contains(r.Stderr, "documents-in(services/*.toml)") ||
		!strings.Contains(r.Stderr, "--collection") {
		t.Fatalf("hard error must name the resolver and point to --collection, stderr:\n%s", r.Stderr)
	}
}
