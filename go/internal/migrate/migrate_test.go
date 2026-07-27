package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

func compileProg(t *testing.T, src string) *ir.Program {
	t.Helper()
	d, perr := tomldoc.Parse([]byte(src))
	if perr != nil {
		t.Fatalf("schema parse: %v", perr)
	}
	s, diags := schema.ReadSchema(d.Root, "")
	if len(diags) > 0 {
		t.Fatalf("schema authoring diags: %v", diags)
	}
	return ir.Compile(s, nil)
}

// TestMigrateRevalidationFailure: a migration whose OUTPUT is well-formed but
// invalid under the target schema is a hard error — MigrateDocument returns
// STRICTSPEC_MIGRATE_REVALIDATION_FAILED FIRST, followed by the underlying
// validation diagnostics (they are carried, not swallowed).
func TestMigrateRevalidationFailure(t *testing.T) {
	const schemaV2 = `
name = "S"
meta_version = 1
format_version = 2
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.a]
type = "string"
required = true
`
	prog := compileProg(t, schemaV2)
	// v1 doc; migration 1->2 sets $.a to an INTEGER — well-formed JSON, but the
	// target schema requires a string, so revalidation must fail.
	in := []byte(`{"format_version": 1, "a": "hello"}`)
	m := &Migration{
		Schema: "S", From: 1, To: 2, Set: "bad",
		Ops: []Op{{Kind: OpSetValue, Path: "$.a", Value: litNodeM(t, "5"), HasValue: true, Down: DownTotal}},
	}
	res := MigrateDocument([]*Migration{m}, prog, doc.FormatJSON, in)
	if len(res.Diags) < 2 {
		t.Fatalf("expected REVALIDATION_FAILED + underlying diagnostics, got %v", res.Diags)
	}
	if res.Diags[0].Code != "STRICTSPEC_MIGRATE_REVALIDATION_FAILED" {
		t.Fatalf("first diagnostic must be REVALIDATION_FAILED, got %v", res.Diags)
	}
	// The underlying validation diagnostic (a type violation on $.a) must be carried.
	carried := false
	for _, d := range res.Diags[1:] {
		if d.Path.Render() == "$.a" {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("underlying validation diagnostics not carried: %v", res.Diags)
	}
}

// litNodeM builds a TOML-parsed literal value node (as a migration file supplies).
func litNodeM(t *testing.T, tomlLit string) doc.Node {
	t.Helper()
	d, perr := tomldoc.Parse([]byte("v = " + tomlLit + "\n"))
	if perr != nil {
		t.Fatalf("lit parse %q: %v", tomlLit, perr)
	}
	for _, e := range d.Root.Entries() {
		if e.Key == "v" {
			return e.Value
		}
	}
	t.Fatalf("lit not found")
	return nil
}

const examplesRel = "../../../examples/migrations"

func readFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(examplesRel, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}

func parseMig(t *testing.T, rel string) *Migration {
	t.Helper()
	src := readFile(t, rel)
	m, diags, err := ParseMigration(src, rel)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	if len(diags) > 0 {
		t.Fatalf("migration %s has authoring diagnostics: %v", rel, diags)
	}
	return m
}

// TestBudgetFlagshipByteExact is the write-side byte-identity assertion: the
// flagship budget migration (rename_field + wrap_in_array) turns
// samples/budget.v1.json into EXACTLY samples/budget.v2.expected.json —
// untouched values (name, prompt_template, version) preserved byte-for-byte,
// the wrapped float 5.0 written [5.0] (float-marked, per A.6).
func TestBudgetFlagshipByteExact(t *testing.T) {
	m := parseMig(t, "budget-v1-to-v2.migration.toml")
	if m.Schema != "AgentDefinition" || m.From != 1 || m.To != 2 {
		t.Fatalf("header parse wrong: %+v", m)
	}
	if len(m.Ops) != 2 || m.Ops[0].Kind != OpRenameField || m.Ops[1].Kind != OpWrapInArray {
		t.Fatalf("ops parse wrong: %+v", m.Ops)
	}
	in := readFile(t, "samples/budget.v1.json")
	want := readFile(t, "samples/budget.v2.expected.json")

	out, diags := ApplyUp(m, doc.FormatJSON, in)
	if len(diags) > 0 {
		t.Fatalf("ApplyUp diagnostics: %v", diags)
	}
	if string(out) != string(want) {
		t.Fatalf("byte mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

// TestBudgetDownPartialFailure: the down migration on a MULTI-element
// cost_thresholds is the pinned STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON hard
// error (this is what makes the declared taxonomy PARTIAL, not total).
func TestBudgetDownPartialFailure(t *testing.T) {
	m := parseMig(t, "budget-v1-to-v2.migration.toml")
	if m.DeclaredTaxonomy() != DownPartial {
		t.Fatalf("declared taxonomy = %s, want partial", m.DeclaredTaxonomy())
	}
	multi := readFile(t, "samples/budget.v2.multi-element.json")
	_, diags := ApplyDown(m, doc.FormatJSON, multi)
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON" {
		t.Fatalf("expected UNWRAP_NOT_SINGLETON, got %v", diags)
	}
	if got := diags[0].Slots["actual"]; got == nil {
		t.Fatalf("missing actual slot")
	}
}

// TestBudgetDownSingletonRoundTrip: down on a well-formed single-element v2
// document round-trips back to the v1 shape.
func TestBudgetDownSingletonRoundTrip(t *testing.T) {
	m := parseMig(t, "budget-v1-to-v2.migration.toml")
	v2 := readFile(t, "samples/budget.v2.expected.json")
	out, diags := ApplyDown(m, doc.FormatJSON, v2)
	if len(diags) > 0 {
		t.Fatalf("down diagnostics: %v", diags)
	}
	// The down output must have max_cost_usd back and format_version 1.
	if !containsAll(string(out), `"max_cost_usd"`, `"format_version": 1`) {
		t.Fatalf("down output wrong:\n%s", out)
	}
}

// TestWorkspaceRenameChain: the two-step pure-rename chain
// internal -> changelog_exempt -> dev_node, byte-exact against the v3 sample.
func TestWorkspaceRenameChain(t *testing.T) {
	m1 := parseMig(t, "dev-node-rename-v1-to-v2.migration.toml")
	m2 := parseMig(t, "dev-node-rename-v2-to-v3.migration.toml")
	in := readFile(t, "samples/workspace-project.v1.json")
	want := readFile(t, "samples/workspace-project.v3.expected.json")

	res := MigrateDocument([]*Migration{m1, m2}, nil, doc.FormatJSON, in)
	if len(res.Diags) > 0 {
		t.Fatalf("chain diagnostics: %v", res.Diags)
	}
	if string(res.Output) != string(want) {
		t.Fatalf("byte mismatch:\n--- got ---\n%s\n--- want ---\n%s", res.Output, want)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
