package migrate

import (
	"strings"
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/write"
)

// litNode builds a TOML-parsed literal value node (as a migration file supplies).
func litNode(t *testing.T, toml string) doc.Node {
	t.Helper()
	d, err := write.New(doc.FormatTOML, []byte("v = "+toml+"\n"))
	if err != nil {
		t.Fatalf("lit parse %q: %v", toml, err)
	}
	for _, e := range d.Root().Entries() {
		if e.Key == "v" {
			return e.Value
		}
	}
	t.Fatalf("lit not found")
	return nil
}

func applyOne(t *testing.T, format doc.Format, src string, op Op) (string, []doc.Node, []diag.Diagnostic) {
	t.Helper()
	wd, err := write.New(format, []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := applyOp(wd, op)
	return string(wd.Bytes()), nil, diags
}

func TestOpAddField(t *testing.T) {
	out, _, diags := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpAddField, Path: "$.b", Value: litNode(t, "2"), HasValue: true})
	if len(diags) > 0 {
		t.Fatalf("diags: %v", diags)
	}
	if out != `{"a": 1, "b": 2}` {
		t.Fatalf("add_field out = %q", out)
	}
}

func TestOpAddFieldCollision(t *testing.T) {
	_, _, diags := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpAddField, Path: "$.a", Value: litNode(t, "2"), HasValue: true})
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_COLLISION" {
		t.Fatalf("expected COLLISION, got %v", diags)
	}
}

func TestOpRemoveField(t *testing.T) {
	out, _, _ := applyOne(t, doc.FormatJSON, `{"a": 1, "b": 2}`,
		Op{Kind: OpRemoveField, Path: "$.a"})
	if out != `{"b": 2}` {
		t.Fatalf("remove_field out = %q", out)
	}
	out2, _, _ := applyOne(t, doc.FormatJSON, `{"a": 1, "b": 2}`,
		Op{Kind: OpRemoveField, Path: "$.b"})
	if out2 != `{"a": 1}` {
		t.Fatalf("remove_field(last) out = %q", out2)
	}
}

func TestOpRemoveFieldMissing(t *testing.T) {
	_, _, diags := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpRemoveField, Path: "$.z"})
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_TARGET_MISSING" {
		t.Fatalf("expected TARGET_MISSING, got %v", diags)
	}
}

func TestOpRenameCollision(t *testing.T) {
	_, _, diags := applyOne(t, doc.FormatJSON, `{"a": 1, "b": 2}`,
		Op{Kind: OpRenameField, From: "$.a", To: "$.b"})
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_COLLISION" {
		t.Fatalf("expected COLLISION, got %v", diags)
	}
}

func TestOpMoveField(t *testing.T) {
	out, _, diags := applyOne(t, doc.FormatJSON, `{"src": {"x": 1}, "dst": {}}`,
		Op{Kind: OpMoveField, From: "$.src.x", To: "$.dst.x"})
	if len(diags) > 0 {
		t.Fatalf("diags: %v", diags)
	}
	if !strings.Contains(out, `"dst": {"x": 1}`) || strings.Contains(out, `"src": {"x"`) {
		t.Fatalf("move_field out = %q", out)
	}
}

func TestOpSetValue(t *testing.T) {
	out, _, _ := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpSetValue, Path: "$.a", Value: litNode(t, "42"), HasValue: true})
	if out != `{"a": 42}` {
		t.Fatalf("set_value out = %q", out)
	}
}

func TestOpAddCollectionAndAppend(t *testing.T) {
	out, _, _ := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpAddCollection, Path: "$.xs", Value: litNode(t, "[1, 2]"), HasValue: true})
	if out != `{"a": 1, "xs": [1, 2]}` {
		t.Fatalf("add_collection out = %q", out)
	}
	out2, _, _ := applyOne(t, doc.FormatJSON, `{"xs": [1, 2]}`,
		Op{Kind: OpAppend, Path: "$.xs", Value: litNode(t, "3"), HasValue: true})
	if out2 != `{"xs": [1, 2, 3]}` {
		t.Fatalf("append out = %q", out2)
	}
	out3, _, _ := applyOne(t, doc.FormatJSON, `{"xs": []}`,
		Op{Kind: OpAppend, Path: "$.xs", Value: litNode(t, "3"), HasValue: true})
	if out3 != `{"xs": [3]}` {
		t.Fatalf("append(empty) out = %q", out3)
	}
}

func TestOpDropCollectionTypeMismatch(t *testing.T) {
	_, _, diags := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpDropCollection, Path: "$.a"})
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_TYPE_MISMATCH" {
		t.Fatalf("expected TYPE_MISMATCH, got %v", diags)
	}
}

func TestOpMergeDefaults(t *testing.T) {
	// b is absent -> injected; a present -> untouched.
	defs := litNode(t, "{ a = 99, b = 2 }") // inline table
	out, _, _ := applyOne(t, doc.FormatJSON, `{"a": 1}`,
		Op{Kind: OpMergeDefaults, Path: "$", Defaults: defs.Entries()})
	if out != `{"a": 1, "b": 2}` {
		t.Fatalf("merge_defaults out = %q", out)
	}
}

func TestOpWrapUnwrap(t *testing.T) {
	out, _, _ := applyOne(t, doc.FormatJSON, `{"a": 5.0}`,
		Op{Kind: OpWrapInArray, Path: "$.a"})
	if out != `{"a": [5.0]}` {
		t.Fatalf("wrap out = %q", out)
	}
	out2, _, _ := applyOne(t, doc.FormatJSON, `{"a": [5.0]}`,
		Op{Kind: OpUnwrapSingleton, Path: "$.a"})
	if out2 != `{"a": 5.0}` {
		t.Fatalf("unwrap out = %q", out2)
	}
	_, _, diags := applyOne(t, doc.FormatJSON, `{"a": [1, 2]}`,
		Op{Kind: OpUnwrapSingleton, Path: "$.a"})
	if len(diags) != 1 || diags[0].Code != "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON" {
		t.Fatalf("expected UNWRAP_NOT_SINGLETON, got %v", diags)
	}
}

func TestOpRemoveWhere(t *testing.T) {
	// Remove elements where status == "dead".
	src := `{"xs": [{"status": "live"}, {"status": "dead"}, {"status": "dead"}]}`
	out, _, _ := applyOne(t, doc.FormatJSON, src,
		Op{Kind: OpRemoveWhere, Path: "$.xs", Where: &Cond{Field: "status", Predicate: "equals", Value: litNode(t, `"dead"`)}})
	if out != `{"xs": [{"status": "live"}]}` {
		t.Fatalf("remove_where out = %q", out)
	}
}

func TestOpSetValueWhere(t *testing.T) {
	src := `{"xs": [{"k": "a", "v": 1}, {"k": "b", "v": 1}]}`
	out, _, _ := applyOne(t, doc.FormatJSON, src,
		Op{Kind: OpSetValueWhere, Path: "$.xs", Field: "v", Value: litNode(t, "9"),
			Where: &Cond{Field: "k", Predicate: "equals", Value: litNode(t, `"b"`)}})
	if out != `{"xs": [{"k": "a", "v": 1}, {"k": "b", "v": 9}]}` {
		t.Fatalf("set_value_where out = %q", out)
	}
}

