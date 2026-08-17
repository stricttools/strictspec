package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The declaration-regime behaviors the CLI gained on strictcli 0.33: the
// member-spelled `mode` selector on validate (which replaced the retired mutex
// group plus its hand guard), argument presence declared rather than guarded,
// framework-owned --dry-run over the effects handle, and the mutating-default
// ban's consequence that absence is read as absence rather than as "".

// --- validate: the member-spelled mode selector -----------------------------

// Nothing elected is the framework's refusal, not a handler's. The old code
// reached the handler with both bools false and printed its own sentence.
func TestValidateModeSelectorRequiresAnElection(t *testing.T) {
	_, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{"validate", schema, fleet})
	if r.ExitCode == 0 {
		t.Fatal("validate with no mode elected must fail")
	}
	if !strings.Contains(r.Stderr, "one of --structural-only, --with-domain-checks is required") {
		t.Fatalf("expected the framework's unsatisfied-selector refusal, stderr:\n%s", r.Stderr)
	}
}

// Two elected is the framework's mutual-exclusion refusal.
func TestValidateModeSelectorRefusesTwoElections(t *testing.T) {
	_, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{"validate", schema, fleet,
		"--structural-only", "--with-domain-checks"})
	if r.ExitCode == 0 {
		t.Fatal("validate with both modes elected must fail")
	}
	if !strings.Contains(r.Stderr, "--structural-only and --with-domain-checks are mutually exclusive") {
		t.Fatalf("expected the framework's mutual-exclusion refusal, stderr:\n%s", r.Stderr)
	}
}

// A declined member elects nothing: --no-structural-only says "not this one".
func TestValidateModeSelectorDeclineElectsNothing(t *testing.T) {
	_, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{"validate", schema, fleet, "--no-structural-only"})
	if r.ExitCode == 0 {
		t.Fatal("--no-structural-only declines an option; it does not choose one")
	}
	if !strings.Contains(r.Stderr, "is required") {
		t.Fatalf("expected the unsatisfied-selector refusal, stderr:\n%s", r.Stderr)
	}
}

// The evidence flags live INSIDE the with-domain-checks scope. Under
// --structural-only they used to be accepted and silently ignored; now the
// parser refuses and names both sides.
func TestValidateEvidenceFlagsAreScopedToDomainChecks(t *testing.T) {
	dir, schema, fleet := writeFleetFixture(t)
	r := newApp().Test([]string{"validate", schema, fleet,
		"--structural-only", "--collection", "services/*.toml", "--collection-root", dir})
	if r.ExitCode == 0 {
		t.Fatal("--collection under --structural-only must be a parse error, not a silently ignored flag")
	}
	if !strings.Contains(r.Stderr, "--collection") || !strings.Contains(r.Stderr, "--structural-only") {
		t.Fatalf("the scope refusal must name both sides, stderr:\n%s", r.Stderr)
	}
}

// --- argument presence ------------------------------------------------------

// The variadic `documents` arg declares its presence, so an empty document list
// is refused by the declaration instead of by a hand guard in the handler.
func TestValidateRequiresAtLeastOneDocument(t *testing.T) {
	_, schema, _ := writeFleetFixture(t)
	r := newApp().Test([]string{"validate", schema, "--structural-only"})
	if r.ExitCode == 0 {
		t.Fatal("validate with no documents must fail")
	}
	if !strings.Contains(r.Stderr, "documents") {
		t.Fatalf("the refusal must name the missing argument, stderr:\n%s", r.Stderr)
	}
}

func TestMigrateRequiresAtLeastOneDocument(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.toml")
	writeFile(t, schemaPath, budgetV2Schema)
	r := newApp().Test([]string{"migrate", schemaPath, "--to", "2", "--migrations", dir})
	if r.ExitCode == 0 {
		t.Fatal("migrate with no documents must fail")
	}
	if !strings.Contains(r.Stderr, "documents") {
		t.Fatalf("the refusal must name the missing argument, stderr:\n%s", r.Stderr)
	}
}

// --- framework-owned --dry-run ----------------------------------------------

// migrate previews without touching the document, and still renders the
// would-be bytes the framework's would-do log cannot carry.
func TestMigrateDryRunLeavesTheDocumentUntouched(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "schema.toml"), budgetV2Schema)
	writeFile(t, filepath.Join(dir, "budget.migration.toml"), budgetMigrationFile)
	docPath := filepath.Join(dir, "doc.json")
	writeFile(t, docPath, docV1JSON)

	r := newApp().Test([]string{"migrate", filepath.Join(dir, "schema.toml"), docPath,
		"--to", "2", "--migrations", dir, "--dry-run"})
	if r.ExitCode != 0 {
		t.Fatalf("migrate --dry-run exit = %d: %s", r.ExitCode, r.Stderr)
	}
	got, _ := os.ReadFile(docPath)
	if string(got) != docV1JSON {
		t.Fatalf("--dry-run rewrote the document:\n%s", got)
	}
	if _, err := os.Stat(docPath + ".strictspec-migrate.tmp"); err == nil {
		t.Fatal("--dry-run left a temp file behind")
	}
	// The would-be document bytes are still shown.
	if !strings.Contains(r.Stdout, "cost_thresholds") {
		t.Fatalf("--dry-run must still render the would-be output, stdout:\n%s", r.Stdout)
	}
}

// gen previews without creating, chmodding or clobbering anything.
func TestGenDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	schemasDir := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(schemasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(fixturesSchemas(t), "shared-canvas.toml"),
		filepath.Join(schemasDir, "shared-canvas.toml"))
	copyFile(t, filepath.Join(fixturesSchemas(t), "types-geometry.toml"),
		filepath.Join(schemasDir, "types-geometry.toml"))
	manifest := filepath.Join(dir, "strictspec.toml")
	writeFile(t, manifest, `format_version = 1
[[schemas]]
path = "schemas/shared-canvas.toml"
  [[schemas.targets]]
  lang = "go"
  output = "gen/canvas_gen.go"
  package = "canvas"
`)

	r := newApp().Test([]string{"gen", "--manifest", manifest, "--dry-run"})
	if r.ExitCode != 0 {
		t.Fatalf("gen --dry-run exit = %d: %s", r.ExitCode, r.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "canvas_gen.go")); err == nil {
		t.Fatal("gen --dry-run wrote the generated file")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitattributes")); err == nil {
		t.Fatal("gen --dry-run wrote .gitattributes")
	}
}

func TestInitDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "strictspec.toml")
	r := newApp().Test([]string{"init", "--manifest", manifest, "--dry-run"})
	if r.ExitCode != 0 {
		t.Fatalf("init --dry-run exit = %d: %s", r.ExitCode, r.Stderr)
	}
	if _, err := os.Stat(manifest); err == nil {
		t.Fatal("init --dry-run wrote the manifest")
	}
}

func TestExportDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(fixturesSchemas(t), "shared-canvas.toml")
	out := filepath.Join(dir, "canvas.schema.json")
	r := newApp().Test([]string{"export", schema, "--output", out, "--dry-run"})
	if r.ExitCode != 0 {
		t.Fatalf("export --dry-run exit = %d: %s", r.ExitCode, r.Stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("export --dry-run wrote the output file")
	}
}

// --- absence is absence, not "" ---------------------------------------------

// --output's absence means stdout; an EXPLICIT empty path is an invocation that
// named a path, and is refused rather than silently re-read as "no path".
func TestExportRefusesAnExplicitEmptyOutputPath(t *testing.T) {
	schema := filepath.Join(fixturesSchemas(t), "shared-canvas.toml")
	r := newApp().Test([]string{"export", schema, "--output", ""})
	if r.ExitCode == 0 {
		t.Fatal("export --output '' must be refused, not silently treated as stdout")
	}
	if !strings.Contains(r.Stderr, "--output") {
		t.Fatalf("the refusal must name --output, stderr:\n%s", r.Stderr)
	}
}

// The optional switches on the mutating commands resolve absence to the
// fallback their own help declares -- gen/init to strictspec.toml, migrate's
// --migrations to the current directory.
func TestOptionalFallbacksMatchTheirHelpText(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// init with no --manifest scaffolds ./strictspec.toml.
	if r := newApp().Test([]string{"init"}); r.ExitCode != 0 {
		t.Fatalf("init exit = %d: %s", r.ExitCode, r.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "strictspec.toml")); err != nil {
		t.Fatalf("init with no --manifest must write ./strictspec.toml: %v", err)
	}

	// migrate with no --migrations reads the current directory.
	writeFile(t, filepath.Join(dir, "schema.toml"), budgetV2Schema)
	writeFile(t, filepath.Join(dir, "budget.migration.toml"), budgetMigrationFile)
	writeFile(t, filepath.Join(dir, "doc.json"), docV1JSON)
	r := newApp().Test([]string{"migrate", "schema.toml", "doc.json", "--to", "2"})
	if r.ExitCode != 0 {
		t.Fatalf("migrate with no --migrations exit = %d: %s", r.ExitCode, r.Stderr)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "doc.json"))
	if string(got) != docV2ExpectedJSON {
		t.Fatalf("migrated bytes:\n%s", got)
	}
}
