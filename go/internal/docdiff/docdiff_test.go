package docdiff

import (
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
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
