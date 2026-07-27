package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const budgetV2Schema = `
name = "AgentDefinition"
meta_version = 1
format_version = 2
document_syntax = "json"
role = "schema"
root = "Root"
[types.Root]
type = "record"
[types.Root.fields.name]
type = "string"
required = true
[types.Root.fields.prompt_template]
type = "string"
required = true
[types.Root.fields.version]
type = "string"
required = true
[types.Root.fields.budget]
type = "Budget"
required = true
[types.Budget]
type = "record"
[types.Budget.fields.cost_thresholds]
type = "array"
required = true
  [types.Budget.fields.cost_thresholds.item]
  type = "float"
`

const budgetV1Schema = `
name = "AgentDefinition"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Root"
[types.Root]
type = "record"
[types.Root.fields.name]
type = "string"
required = true
[types.Root.fields.prompt_template]
type = "string"
required = true
[types.Root.fields.version]
type = "string"
required = true
[types.Root.fields.budget]
type = "Budget"
required = true
[types.Budget]
type = "record"
[types.Budget.fields.max_cost_usd]
type = "float"
required = true
`

const budgetMigrationFile = `
[migration]
schema = "AgentDefinition"
from_format_version = 1
to_format_version = 2
migration_set = "agent-budget"
[[ops]]
op = "rename_field"
from = "$.budget.max_cost_usd"
to = "$.budget.cost_thresholds"
down = "total"
[[ops]]
op = "wrap_in_array"
path = "$.budget.cost_thresholds"
down = "partial"
[[down_ops]]
op = "unwrap_singleton"
path = "$.budget.cost_thresholds"
[[down_ops]]
op = "rename_field"
from = "$.budget.cost_thresholds"
to = "$.budget.max_cost_usd"
`

const docV1JSON = `{
  "format_version": 1,
  "name": "reviewer",
  "prompt_template": "review",
  "version": "1.0.0",
  "budget": { "max_cost_usd": 5.0 }
}`

const docV2ExpectedJSON = `{
  "format_version": 2,
  "name": "reviewer",
  "prompt_template": "review",
  "version": "1.0.0",
  "budget": { "cost_thresholds": [5.0] }
}`

// TestMigrateCLIByteExact: `strictspec migrate` writes the flagship v2 document
// byte-exact.
func TestMigrateCLIByteExact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "schema.toml"), budgetV2Schema)
	writeFile(t, filepath.Join(dir, "budget.migration.toml"), budgetMigrationFile)
	docPath := filepath.Join(dir, "doc.json")
	writeFile(t, docPath, docV1JSON)

	r := newApp().Test([]string{"migrate", filepath.Join(dir, "schema.toml"), docPath,
		"--to", "2", "--migrations", dir})
	if r.ExitCode != 0 {
		t.Fatalf("migrate exit = %d: %s", r.ExitCode, r.Stderr)
	}
	got, _ := os.ReadFile(docPath)
	if string(got) != docV2ExpectedJSON {
		t.Fatalf("migrated bytes:\n--- got ---\n%s\n--- want ---\n%s", got, docV2ExpectedJSON)
	}
}

// TestMigrateOnCurrentRefusal: migrating a doc already at the current version is
// refused (STRICTSPEC_MIGRATE_ON_CURRENT).
func TestMigrateOnCurrentRefusal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "schema.toml"), budgetV2Schema)
	writeFile(t, filepath.Join(dir, "budget.migration.toml"), budgetMigrationFile)
	docPath := filepath.Join(dir, "doc.json")
	writeFile(t, docPath, docV2ExpectedJSON)

	r := newApp().Test([]string{"migrate", filepath.Join(dir, "schema.toml"), docPath,
		"--to", "2", "--migrations", dir})
	if r.ExitCode == 0 {
		t.Fatal("migrate must refuse a doc already at the current version")
	}
	if !strings.Contains(r.Stderr, "STRICTSPEC_MIGRATE_ON_CURRENT") {
		t.Fatalf("expected MIGRATE_ON_CURRENT, stderr: %s", r.Stderr)
	}
}

// TestDiffCLICertificate: `strictspec diff` emits a corpus-supported certificate
// and exits green over a corpus with no violations.
func TestDiffCLICertificate(t *testing.T) {
	dir := t.TempDir()
	oldS := filepath.Join(dir, "old.toml")
	newS := filepath.Join(dir, "new.toml")
	mig := filepath.Join(dir, "budget.migration.toml")
	writeFile(t, oldS, budgetV1Schema)
	writeFile(t, newS, budgetV2Schema)
	writeFile(t, mig, budgetMigrationFile)
	corpusDir := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(corpusDir, "v1.json"), docV1JSON)
	writeFile(t, filepath.Join(corpusDir, "v2.json"), docV2ExpectedJSON)

	res := newApp().Test([]string{"diff", oldS, newS,
		"--corpus", "*.json", "--corpus-root", corpusDir, "--migration", mig})
	if res.ExitCode != 0 {
		t.Fatalf("diff exit = %d: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "corpus-supported") || !strings.Contains(res.Stdout, "content_hash") {
		t.Fatalf("certificate output unexpected:\n%s", res.Stdout)
	}
}

// TestDocDiffCLI: `strictspec doc-diff` emits a per-path delta with the wrapped
// value change.
func TestDocDiffCLI(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.toml")
	writeFile(t, schemaPath, budgetV2Schema)
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	writeFile(t, a, docV2ExpectedJSON)
	writeFile(t, b, `{
  "format_version": 2,
  "name": "reviewer",
  "prompt_template": "review",
  "version": "1.0.0",
  "budget": { "cost_thresholds": [9.0] }
}`)
	res := newApp().Test([]string{"doc-diff", schemaPath, a, b})
	if res.ExitCode != 0 {
		t.Fatalf("doc-diff exit = %d: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, `"changed"`) || !strings.Contains(res.Stdout, "cost_thresholds[0]") {
		t.Fatalf("doc-diff delta unexpected:\n%s", res.Stdout)
	}
}
