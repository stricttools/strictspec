package interp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/internal/diag"
	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/jsondoc"
	"github.com/stricttools/strictspec/go/internal/render"
	"github.com/stricttools/strictspec/go/internal/schema"
	"github.com/stricttools/strictspec/go/internal/tomldoc"
)

func fixturesDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "conformance", "fixtures"))
}

type want struct {
	code string
	path string
}

// runFixture loads a fixture schema + input like the harness adapter does and
// returns the observed diagnostics.
func runFixture(t *testing.T, schemaRel, inputRel, syntax string, evidence map[string][]map[string]any) []diag.Diagnostic {
	t.Helper()
	root := fixturesDir(t)
	s, sdiags, err := schema.LoadFile(filepath.Join(root, schemaRel))
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) != 0 {
		t.Fatalf("schema %s has authoring diagnostics: %v", schemaRel, codes(sdiags))
	}
	scalars := schema.LoadManifestScalars(s.Dir)
	src, err := readFile(filepath.Join(root, inputRel))
	if err != nil {
		t.Fatal(err)
	}
	switch syntax {
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			t.Fatalf("parse toml: %v", perr)
		}
		return Validate(s, d.Root, Options{Scalars: scalars, Format: doc.FormatTOML, Evidence: evidence})
	case "jsonl":
		docs, perr := jsondoc.ParseLines(src)
		if perr != nil {
			t.Fatalf("parse jsonl: %v", perr)
		}
		var out []diag.Diagnostic
		starts := []int{0}
		for i, b := range src {
			if b == '\n' {
				starts = append(starts, i+1)
			}
		}
		for i, d := range docs {
			ls := 0
			if i < len(starts) {
				ls = starts[i]
			}
			out = append(out, Validate(s, d.Root, Options{
				Scalars: scalars, Format: doc.FormatJSONL, Evidence: evidence,
				JSONL: true, Line: i + 1, LineStart: ls,
			})...)
		}
		return out
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			t.Fatalf("parse json: %v", perr)
		}
		return Validate(s, d.Root, Options{Scalars: scalars, Format: doc.FormatJSON, Evidence: evidence})
	}
}

func assertDiags(t *testing.T, got []diag.Diagnostic, wants []want) {
	t.Helper()
	if len(got) != len(wants) {
		t.Fatalf("got %d diagnostics %v, want %d %v", len(got), describe(got), len(wants), wants)
	}
	for i, w := range wants {
		if got[i].Code != w.code {
			t.Errorf("diag[%d] code = %s, want %s", i, got[i].Code, w.code)
		}
		if got[i].Path.Render() != w.path {
			t.Errorf("diag[%d] path = %s, want %s", i, got[i].Path.Render(), w.path)
		}
		// Rendering must not panic (message identity is asserted end-to-end by the
		// harness; here we assert code+path and that the message renders).
		_ = render.Render(got[i])
	}
}

func TestGateAbsent(t *testing.T) {
	got := runFixture(t, "_schemas/claudestream-agent.toml", "_inputs/claudestream/missing-gate.agent.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_GATE_ABSENT", "$"}})
}

func TestGateWrongType(t *testing.T) {
	got := runFixture(t, "_schemas/wavescript-score.schema.toml", "_inputs/wavescript/mut-gate-wrong-type.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_GATE_WRONG_TYPE", "$"}})
}

func TestUnknownKey(t *testing.T) {
	got := runFixture(t, "_schemas/wavescript-score.schema.toml", "_inputs/wavescript/mut-unknown-key.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_KEY_UNKNOWN", `$.embedded_bank["svf_pad"](subtractive).params`}})
	// Suggestion renders empty ("wobble" is >2 edits from every param name).
	if msg := render.Render(got[0]); strings.Contains(msg, "Did you mean") {
		t.Errorf("unexpected suggestion in %q", msg)
	}
}

func TestWrongTypeInteger(t *testing.T) {
	got := runFixture(t, "_schemas/wavescript-score.schema.toml", "_inputs/wavescript/mut-wrong-type-seed.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_TYPE_NOT_INTEGER", "$.seed"}})
}

func TestNumberUnrepresentable(t *testing.T) {
	got := runFixture(t, "_schemas/predraw-scene.toml", "_inputs/predraw/scene.invalid-unrepresentable-number.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_NUM_UNREPRESENTABLE", "$.width"}})
}

func TestDatetimeKind(t *testing.T) {
	got := runFixture(t, "_schemas/datetime.schema.toml", "_inputs/datetime/invalid-01-kind-and-range.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_TYPE_DATETIME_KIND", "$.recorded_at"}})
}

func TestEnumSourced(t *testing.T) {
	got := runFixture(t, "_schemas/enum-drum-pattern.toml", "_inputs/enum/pattern.invalid.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_TYPE_NOT_ENUM_MEMBER", "$.hits[1].sound"}})
	got = runFixture(t, "_schemas/enum-drum-pattern.toml", "_inputs/enum/pattern.valid.json", "json", nil)
	assertDiags(t, got, nil)
}

func TestCustomScalars(t *testing.T) {
	got := runFixture(t, "_schemas/pgdesign-table.toml", "_inputs/pgdesign/account.invalid.toml", "toml", nil)
	assertDiags(t, got, []want{
		{"STRICTSPEC_SCALAR_LEXEME", "$.name"},
		{"STRICTSPEC_TYPE_MISSING_REQUIRED", "$"},
		{"STRICTSPEC_SCALAR_LEXEME", `$.columns["id"].type`},
		{"STRICTSPEC_SCALAR_LENGTH", `$.columns["note"].check`},
	})
}

func TestUnionsAndConstraints(t *testing.T) {
	got := runFixture(t, "_schemas/shared-canvas.toml", "_inputs/shared/canvas.invalid.json", "json", nil)
	assertDiags(t, got, []want{
		{"STRICTSPEC_VALUE_STRING_REGEX", "$.background"},
		{"STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE", "$.shapes[0](circle).radius"},
		{"STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN", "$.shapes[1]"},
	})
}

func TestOrderedPairInConstraintPass(t *testing.T) {
	got := runFixture(t, "_schemas/pixelweaver-character-preview.toml", "_inputs/pixelweaver/character-preview.invalid-union-and-order.json", "json", nil)
	assertDiags(t, got, []want{
		{"STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN", "$.oscillators.functions[0]"},
		{"STRICTSPEC_INTRA_ORDERED_PAIR", "$.oscillators.functions[1](blink)"},
	})
}

func TestIntraConstraintsOrxtra(t *testing.T) {
	got := runFixture(t, "_schemas/orxtra-workflow.toml", "_inputs/orxtra/broken-pipeline.invalid.toml", "toml", nil)
	assertDiags(t, got, []want{
		{"STRICTSPEC_INTRA_EXACTLY_ONE_OF", "$.tasks[0]"},
		{"STRICTSPEC_INTRA_CONDITIONAL_REQUIRED", "$.tasks[1]"},
		{"STRICTSPEC_INTRA_CONDITIONAL_REQUIRED", "$.tasks[1]"},
		{"STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", "$.tasks[2].depends_on[0]"},
	})
}

func TestGatedFormsWavescript(t *testing.T) {
	got := runFixture(t, "_schemas/wavescript-score.schema.toml", "_inputs/wavescript/invalid-01-gate-violations.json", "json", nil)
	assertDiags(t, got, []want{
		{"STRICTSPEC_INTRA_FORBIDDEN_WHEN", `$.embedded_bank["bad_filter"](subtractive).params.q`},
		{"STRICTSPEC_INTRA_FORBIDDEN_WHEN", `$.embedded_bank["bad_filter"](subtractive).params.harm2_level`},
		{"STRICTSPEC_INTRA_CONDITIONAL_REQUIRED", `$.embedded_bank["noise_unison"](subtractive).params`},
		{"STRICTSPEC_INTRA_CONDITIONAL_VALUE", `$.embedded_bank["noise_unison"](subtractive).params.unison`},
	})
}

func TestAliasBoth(t *testing.T) {
	got := runFixture(t, "_schemas/predraw-scene.toml", "_inputs/predraw/scene.invalid-alias-both.json", "json", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_ALIAS_BOTH_PRESENT", "$.elements[0](rect)"}})
}

func TestAggregates(t *testing.T) {
	ev := map[string][]map[string]any{
		"documents-in(services/*.toml)": {
			{"name": "big1", "memory_mb": float64(2048)},
			{"name": "big2", "memory_mb": float64(2048)},
			{"name": "big3", "memory_mb": float64(2048)},
		},
	}
	got := runFixture(t, "_schemas/aggregates-fleet.toml", "_inputs/aggregates/fail-sum-fleet.toml", "toml", ev)
	assertDiags(t, got, []want{{"STRICTSPEC_CROSS_SUM_LIMIT", "$"}})
}

func TestJSONLAnchors(t *testing.T) {
	got := runFixture(t, "_schemas/rlsbl-changelog-commit-mode.toml", "_inputs/changelog/invalid-commit-mode.jsonl", "jsonl", nil)
	assertDiags(t, got, []want{{"STRICTSPEC_TYPE_NOT_STRING", "$.commits[0]@L2:31"}})
}

func TestValidDocuments(t *testing.T) {
	cases := []struct{ schema, input, syntax string }{
		{"_schemas/shared-canvas.toml", "_inputs/shared/canvas.valid.json", "json"},
		{"_schemas/shared-palette.toml", "_inputs/shared/palette.valid.json", "json"},
		{"_schemas/predraw-scene.toml", "_inputs/predraw/scene.valid.json", "json"},
		{"_schemas/wavescript-score.schema.toml", "_inputs/wavescript/valid-01-embedded-bank.json", "json"},
		{"_schemas/claudestream-agent.toml", "_inputs/claudestream/reviewer.agent.json", "json"},
	}
	for _, c := range cases {
		got := runFixture(t, c.schema, c.input, c.syntax, nil)
		if len(got) != 0 {
			t.Errorf("%s: expected VALID, got %v", c.input, describe(got))
		}
	}
}

// TestDepthLimit exercises the recursion-depth cap on a synthetic recursive
// schema and a document nested beyond the limit.
func TestDepthLimit(t *testing.T) {
	schemaSrc := `
name = "deep"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Node"
[types.Node]
type = "record"
[types.Node.fields.child]
type = "Node"
required = false
`
	sd, perr := tomldoc.Parse([]byte(schemaSrc))
	if perr != nil {
		t.Fatal(perr)
	}
	s, sdiags := schema.ReadSchema(sd.Root, "")
	if len(sdiags) != 0 {
		t.Fatalf("schema diags: %v", codes(sdiags))
	}
	// Build a JSON document nested deeper than maxValidationDepth:
	// {"format_version":1,"child":{"child":{ ... {} ... }}}
	depth := maxValidationDepth + 10
	var b []byte
	b = append(b, []byte(`{"format_version":1,"child":`)...)
	for i := 0; i < depth; i++ {
		b = append(b, []byte(`{"child":`)...)
	}
	b = append(b, []byte(`{}`)...)
	for i := 0; i < depth; i++ {
		b = append(b, '}')
	}
	b = append(b, '}')
	d, jperr := jsondoc.Parse(b)
	if jperr != nil {
		t.Fatalf("json parse: %v", jperr)
	}
	got := Validate(s, d.Root, Options{Format: doc.FormatJSON})
	found := false
	for _, g := range got {
		if g.Code == "STRICTSPEC_DEPTH_EXCEEDED" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected STRICTSPEC_DEPTH_EXCEEDED, got %v", describe(got))
	}
}

// --- helpers ---

func codes(ds []diag.Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func describe(ds []diag.Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code + "@" + d.Path.Render()
	}
	return out
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
