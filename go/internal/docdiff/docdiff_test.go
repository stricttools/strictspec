package docdiff

import (
	"strings"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
	"github.com/smm-h/strictspec/go/internal/write"
)

func loadSchema(t *testing.T, src string) *schema.Schema {
	t.Helper()
	d, perr := tomldoc.Parse([]byte(src))
	if perr != nil {
		t.Fatalf("schema parse: %v", perr)
	}
	s, diags := schema.ReadSchema(d.Root, "")
	if len(diags) > 0 {
		t.Fatalf("schema authoring diags: %v", diags)
	}
	return s
}

func node(t *testing.T, jsonSrc string) doc.Node {
	t.Helper()
	d, err := write.New(doc.FormatJSON, []byte(jsonSrc))
	if err != nil {
		t.Fatalf("doc parse: %v", err)
	}
	return d.Root()
}

const scalarSchema = `
name = "Doc"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Root"

[types.Root]
type = "record"
[types.Root.fields.x]
type = "number"
`

// TestLexemeClassChange: 1 (integer) vs 1.0 (float) at the same path is a
// `changed` delta even though the numeric magnitude is equal (C.2 rules).
func TestLexemeClassChange(t *testing.T) {
	s := loadSchema(t, scalarSchema)
	a := node(t, `{"format_version": 1, "x": 1}`)
	b := node(t, `{"format_version": 1, "x": 1.0}`)
	res := Diff(s, doc.FormatJSON, a, b)
	if len(res.Deltas) != 1 {
		t.Fatalf("expected 1 delta, got %+v", res.Deltas)
	}
	d := res.Deltas[0]
	if d.Op != "changed" || d.Path != "$.x" || *d.OldValue != "1" || *d.NewValue != "1.0" {
		t.Fatalf("wrong delta: %+v (old=%v new=%v)", d, deref(d.OldValue), deref(d.NewValue))
	}
}

// TestAddedRemoved: field added / removed at record scope.
func TestAddedRemoved(t *testing.T) {
	s := loadSchema(t, scalarSchema)
	a := node(t, `{"format_version": 1, "x": 1}`)
	b := node(t, `{"format_version": 1}`)
	res := Diff(s, doc.FormatJSON, a, b)
	if len(res.Deltas) != 1 || res.Deltas[0].Op != "removed" || res.Deltas[0].Path != "$.x" {
		t.Fatalf("expected removed $.x, got %+v", res.Deltas)
	}
	res2 := Diff(s, doc.FormatJSON, b, a)
	if len(res2.Deltas) != 1 || res2.Deltas[0].Op != "added" || res2.Deltas[0].Path != "$.x" {
		t.Fatalf("expected added $.x, got %+v", res2.Deltas)
	}
}

const arraySchema = `
name = "Doc"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Root"

[types.Root]
type = "record"
[types.Root.fields.items]
type = "array"
  [types.Root.fields.items.item]
  type = "Elem"

[types.Elem]
type = "record"
[types.Elem.fields.id]
type = "string"

[[types.Root.fields.items.constraints]]
form = "unique-by"
field = "id"
`

// TestMovedDetection: a matched element (by unique-by key `id`) at a different
// index yields `moved`, not removed+added.
func TestMovedDetection(t *testing.T) {
	s := loadSchema(t, arraySchema)
	a := node(t, `{"format_version": 1, "items": [{"id": "a"}, {"id": "b"}]}`)
	b := node(t, `{"format_version": 1, "items": [{"id": "b"}, {"id": "a"}]}`)
	res := Diff(s, doc.FormatJSON, a, b)
	var moves int
	for _, d := range res.Deltas {
		if d.Op == "moved" {
			moves++
		}
		if d.Op == "added" || d.Op == "removed" {
			t.Fatalf("unexpected %s in a pure reorder: %+v", d.Op, res.Deltas)
		}
	}
	if moves == 0 {
		t.Fatalf("expected moved deltas, got %+v", res.Deltas)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func compileJSONLProg(t *testing.T, src string) *ir.Program {
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

const jsonlSchema = `
name = "LineRec"
meta_version = 1
format_version = 1
document_syntax = "jsonl"
role = "schema"
root = "Root"
[types.Root]
type = "record"
[types.Root.fields.id]
type = "string"
required = true
[types.Root.fields.n]
type = "integer"
required = true
`

// TestComputeJSONLLineScoped: a JSONL doc-diff is line-scoped — each line is an
// independent document, and every delta carries the @Lline:byte suffix. Two
// three-line streams differing on lines 2 and 3 yield exactly two changed deltas,
// anchored to lines 2 and 3 respectively.
func TestComputeJSONLLineScoped(t *testing.T) {
	prog := compileJSONLProg(t, jsonlSchema)
	s := loadSchema(t, jsonlSchema)
	old := []byte(`{"format_version": 1, "id": "a", "n": 1}` + "\n" +
		`{"format_version": 1, "id": "b", "n": 2}` + "\n" +
		`{"format_version": 1, "id": "c", "n": 3}` + "\n")
	nw := []byte(`{"format_version": 1, "id": "a", "n": 1}` + "\n" +
		`{"format_version": 1, "id": "b", "n": 20}` + "\n" +
		`{"format_version": 1, "id": "cc", "n": 3}` + "\n")
	res, diags := ComputeJSONL(prog, s, "a.jsonl", old, "b.jsonl", nw)
	if len(diags) > 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if len(res.Deltas) != 2 {
		t.Fatalf("expected 2 line-scoped deltas, got %+v", res.Deltas)
	}
	d2, d3 := res.Deltas[0], res.Deltas[1]
	if d2.Op != "changed" || !strings.HasPrefix(d2.Path, "$.n@L2:") {
		t.Fatalf("line-2 delta wrong: %+v", d2)
	}
	if d3.Op != "changed" || !strings.HasPrefix(d3.Path, "$.id@L3:") {
		t.Fatalf("line-3 delta wrong: %+v", d3)
	}
}

// TestComputeJSONLInvalidOperand: a line that does not validate against the schema
// makes the whole diff refuse with STRICTSPEC_DOCDIFF_INVALID_OPERAND.
func TestComputeJSONLInvalidOperand(t *testing.T) {
	prog := compileJSONLProg(t, jsonlSchema)
	s := loadSchema(t, jsonlSchema)
	old := []byte(`{"format_version": 1, "id": "a", "n": 1}` + "\n")
	nw := []byte(`{"format_version": 1, "id": "a"}` + "\n") // missing required n
	_, diags := ComputeJSONL(prog, s, "a.jsonl", old, "b.jsonl", nw)
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_DOCDIFF_INVALID_OPERAND" {
		t.Fatalf("expected INVALID_OPERAND, got %v", diags)
	}
}
