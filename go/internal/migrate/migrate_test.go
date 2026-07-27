package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
)

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
