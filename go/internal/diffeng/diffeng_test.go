package diffeng

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/migrate"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

func compile(t *testing.T, src string) *ir.Program {
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

func writeCorpus(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	// Deterministic order.
	sortStrings(paths)
	return paths
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

const budgetV1 = `
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

const budgetV2 = `
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

func budgetMigration(t *testing.T) *migrate.Migration {
	t.Helper()
	m, diags, err := migrate.ParseMigration([]byte(`
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
`), "budget.migration.toml")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) > 0 {
		t.Fatalf("migration authoring diags: %v", diags)
	}
	return m
}

const (
	docV1       = `{"format_version": 1, "name": "r", "prompt_template": "p", "version": "1.0.0", "budget": {"max_cost_usd": 5.0}}`
	docV2Single = `{"format_version": 2, "name": "r", "prompt_template": "p", "version": "1.0.0", "budget": {"cost_thresholds": [5.0]}}`
	docV2Multi  = `{"format_version": 2, "name": "r", "prompt_template": "p", "version": "1.0.0", "budget": {"cost_thresholds": [1.0, 5.0, 10.0]}}`
)

// TestBudgetGreenAndSelfValidate: the flagship migration over a corpus grades
// every claim corpus-supported, and the emitted certificate self-validates
// against the built-in certificate schema.
func TestBudgetGreenAndSelfValidate(t *testing.T) {
	files := writeCorpus(t, map[string]string{
		"a-v1.json":        docV1,
		"b-v2-single.json": docV2Single,
		"c-v2-multi.json":  docV2Multi,
	})
	cert, violations := Run(Inputs{
		SchemaID:  "AgentDefinition",
		OldProg:   compile(t, budgetV1),
		NewProg:   compile(t, budgetV2),
		OldFV:     1,
		NewFV:     2,
		Migration: budgetMigration(t),
		Glob:      "*.json",
		Files:     files,
		Release:   "0.0.0-test",
	})
	if len(violations) > 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
	for _, c := range cert.Claims {
		if c.Grade != GradeCorpusSupported {
			t.Fatalf("claim %s graded %s, want corpus-supported", c.Kind, c.Grade)
		}
	}
	if err := SelfValidate(cert); err != nil {
		t.Fatalf("certificate self-validation failed: %v", err)
	}
}

// TestTaxonomyMisdeclared: declaring wrap_in_array's down as TOTAL is a hard
// error when a multi-element v2 corpus doc makes the down fail (actual partial).
func TestTaxonomyMisdeclared(t *testing.T) {
	m := budgetMigration(t)
	m.Ops[1].Down = migrate.DownTotal // lie: declare total instead of partial
	files := writeCorpus(t, map[string]string{
		"c-v2-multi.json": docV2Multi,
	})
	_, violations := Run(Inputs{
		SchemaID: "AgentDefinition",
		OldProg:  compile(t, budgetV1),
		NewProg:  compile(t, budgetV2),
		OldFV:    1, NewFV: 2,
		Migration: m,
		Glob:      "*.json",
		Files:     files,
		Release:   "0.0.0-test",
	})
	if !hasCode(violations, "STRICTSPEC_DIFF_TAXONOMY_MISDECLARED") {
		t.Fatalf("expected TAXONOMY_MISDECLARED, got %v", violations)
	}
}

// TestSameVersionNarrowing: two schemas at the SAME format_version where the new
// one narrows (adds a max) — a corpus doc flipping valid->invalid is an un-bumped
// narrowing hard error.
func TestSameVersionNarrowing(t *testing.T) {
	old := `
name = "S"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.x]
type = "integer"
required = true
`
	newS := `
name = "S"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.x]
type = "integer"
required = true
max = 10
`
	files := writeCorpus(t, map[string]string{
		"big.json": `{"format_version": 1, "x": 20}`,
	})
	cert, violations := Run(Inputs{
		SchemaID:    "S",
		OldProg:     compile(t, old),
		NewProg:     compile(t, newS),
		NewFV:       1,
		Glob:        "*.json",
		Files:       files,
		Release:     "0.0.0-test",
		SameVersion: true,
	})
	if !hasCode(violations, "STRICTSPEC_DIFF_NARROWING_UNBUMPED") {
		t.Fatalf("expected NARROWING_UNBUMPED, got %v", violations)
	}
	if cert.OldFormatVersion != SameVersionMarker {
		t.Fatalf("same-version cert old_format_version = %v", cert.OldFormatVersion)
	}
	if len(cert.Claims) != 1 || cert.Claims[0].Kind != KindFlipScan || cert.Claims[0].Grade != GradeViolated {
		t.Fatalf("same-version claim wrong: %+v", cert.Claims)
	}
}

// TestRoundTripCompletenessFailure: a migration op that errors on a valid-at-N
// corpus doc is a completeness violation with a real corpus counterexample.
func TestRoundTripCompletenessFailure(t *testing.T) {
	sv1 := `
name = "S"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.x]
type = "integer"
required = true
`
	sv2 := `
name = "S"
meta_version = 1
format_version = 2
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.x]
type = "integer"
required = true
`
	// Migration whose op cannot apply to a valid-at-N document (unwrap a scalar).
	m := &migrate.Migration{
		Schema: "S", From: 1, To: 2, Set: "bad",
		Ops: []migrate.Op{{Kind: migrate.OpUnwrapSingleton, Path: "$.x", Down: migrate.DownTotal}},
	}
	files := writeCorpus(t, map[string]string{
		"d.json": `{"format_version": 1, "x": 5}`,
	})
	cert, violations := Run(Inputs{
		SchemaID: "S",
		OldProg:  compile(t, sv1),
		NewProg:  compile(t, sv2),
		OldFV:    1, NewFV: 2,
		Migration: m,
		Glob:      "*.json",
		Files:     files,
		Release:   "0.0.0-test",
	})
	if !hasCode(violations, "STRICTSPEC_DIFF_VIOLATED") {
		t.Fatalf("expected DIFF_VIOLATED, got %v", violations)
	}
	var comp *Claim
	for i := range cert.Claims {
		if cert.Claims[i].Kind == KindRoundTripCompleteness {
			comp = &cert.Claims[i]
		}
	}
	if comp == nil || comp.Grade != GradeViolated || len(comp.Counterexamples) == 0 {
		t.Fatalf("completeness claim not violated: %+v", cert.Claims)
	}
}

func hasCode(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestAdjudicationGate: a corpus of only v2-single documents (valid at N+1,
// INVALID at N) leaves the completeness and soundness claims VACUOUSLY
// corpus-supported (zero witnesses). The deploy gate requires each be discharged
// by a matching adjudication entry: without one they are ADJUDICATION_MISSING;
// with matching entries the gate is green; a dangling entry is INVALID.
func TestAdjudicationGate(t *testing.T) {
	files := writeCorpus(t, map[string]string{"b-v2-single.json": docV2Single})
	cert, violations := Run(Inputs{
		SchemaID: "AgentDefinition",
		OldProg:  compile(t, budgetV1), NewProg: compile(t, budgetV2),
		OldFV: 1, NewFV: 2,
		Migration: budgetMigration(t),
		Glob:      "*.json", Files: files, Release: "0.0.0-test",
	})
	if len(violations) > 0 {
		t.Fatalf("unexpected engine violations: %v", violations)
	}

	// RED path: no adjudication -> both unsupported claims are MISSING.
	missing := 0
	for _, d := range Adjudicate(cert, nil) {
		if d.Code == "STRICTSPEC_DIFF_ADJUDICATION_MISSING" {
			missing++
		}
	}
	if missing != 2 {
		t.Fatalf("expected 2 ADJUDICATION_MISSING without adjudication, got %d", missing)
	}

	// GREEN path: adjudication discharging both unsupported claims -> empty gate.
	adj := &Adjudication{
		SchemaID: "AgentDefinition", OldFV: 1, NewFV: 2,
		Entries: []AdjEntry{
			{ClaimKind: KindRoundTripCompleteness,
				Scope:         "the migration never errors on a corpus document valid at N",
				Justification: "greenfield: no at-rest N documents", Author: "t", Date: "2026-07-27"},
			{ClaimKind: KindRoundTripSoundness,
				Scope:         "every corpus document valid at N re-validates at N+1 after M",
				Justification: "greenfield: verified by construction", Author: "t", Date: "2026-07-27"},
		},
	}
	if g := Adjudicate(cert, adj); len(g) != 0 {
		t.Fatalf("expected green gate with full adjudication, got %v", g)
	}

	// Dangling entry (matches no unsupported claim) is INVALID.
	stray := &Adjudication{SourcePath: "adj.toml",
		Entries: []AdjEntry{{ClaimKind: KindDownTaxonomy, Scope: "does not match any claim"}}}
	hasInvalid := false
	for _, d := range Adjudicate(cert, stray) {
		if d.Code == "STRICTSPEC_DIFF_ADJUDICATION_INVALID" {
			hasInvalid = true
		}
	}
	if !hasInvalid {
		t.Fatalf("expected ADJUDICATION_INVALID for a dangling entry")
	}
}

// TestAdjudicationFullySupportedNoGate: a corpus that genuinely supports every
// claim needs no adjudication — the gate is empty even with no adjudication file.
func TestAdjudicationFullySupportedNoGate(t *testing.T) {
	files := writeCorpus(t, map[string]string{
		"a-v1.json":        docV1,
		"b-v2-single.json": docV2Single,
	})
	cert, violations := Run(Inputs{
		SchemaID: "AgentDefinition",
		OldProg:  compile(t, budgetV1), NewProg: compile(t, budgetV2),
		OldFV: 1, NewFV: 2,
		Migration: budgetMigration(t),
		Glob:      "*.json", Files: files, Release: "0.0.0-test",
	})
	if len(violations) > 0 {
		t.Fatalf("unexpected violations: %v", violations)
	}
	if g := Adjudicate(cert, nil); len(g) != 0 {
		t.Fatalf("fully corpus-supported run must need no adjudication, got %v", g)
	}
}

// TestSameVersionSelfValidates: a same-version certificate (old_format_version ==
// the "same-version" marker) self-validates against the same-version built-in
// schema — dogfooding is TOTAL, not partial (both modes self-validate; the
// same-version shape is no longer silently skipped).
func TestSameVersionSelfValidates(t *testing.T) {
	old := `
name = "S"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "R"
[types.R]
type = "record"
[types.R.fields.x]
type = "integer"
required = true
`
	files := writeCorpus(t, map[string]string{
		"ok.json": `{"format_version": 1, "x": 3}`,
	})
	cert, violations := Run(Inputs{
		SchemaID:    "S",
		OldProg:     compile(t, old),
		NewProg:     compile(t, old),
		NewFV:       1,
		Glob:        "*.json",
		Files:       files,
		Release:     "0.0.0-test",
		SameVersion: true,
	})
	if len(violations) > 0 {
		t.Fatalf("unexpected violations: %v", violations)
	}
	if cert.OldFormatVersion != SameVersionMarker {
		t.Fatalf("expected same-version marker, got %v", cert.OldFormatVersion)
	}
	if err := SelfValidate(cert); err != nil {
		t.Fatalf("same-version certificate must self-validate: %v", err)
	}
}
