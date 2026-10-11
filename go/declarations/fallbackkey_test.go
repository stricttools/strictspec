package declarations_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

const corsSchema = `name = "test-cors"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Cors"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Cors]
type = "record"
[types.Cors.fields.rule]
type = "array"
required = true
[types.Cors.fields.rule.item]
type = "Rule"
[types.Cors.fields.unmanaged]
type = "array"
required = true
[types.Cors.fields.unmanaged.item]
type = "UnmanagedEntry"

[types.Rule]
type = "record"
[types.Rule.fields.id]
type = "string"
non_empty = true
[types.Rule.fields.origins]
type = "array"
required = true
[types.Rule.fields.origins.item]
type = "string"
[types.Rule.fields.methods]
type = "array"
required = true
[types.Rule.fields.methods.item]
type = "string"
`

// corsType holds rules keyed by their id, and a rule without one by its
// origins.
type corsType struct{ fallback []string }

func (corsType) Name() string { return "test-cors" }
func (corsType) Schema() (map[string]string, string) {
	return map[string]string{"test-cors.schema.toml": corsSchema}, "test-cors.schema.toml"
}
func (c corsType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{
		Collections: []declarations.Collection{{Name: "rule", Key: []string{"id"}, FallbackKey: c.fallback, Fields: []string{"origins", "methods"}}},
		Stage:       30,
	}
}
func (corsType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-cors", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{"rule": {}}}
	rules, _ := field(doc, "rule")
	for _, it := range rules.Items() {
		item := declarations.Item{Fields: map[string]declarations.Value{}}
		for _, kv := range it.Entries() {
			item.Fields[kv.Key] = valueOf(kv.Value)
		}
		r.Children["rule"] = append(r.Children["rule"], item)
	}
	return r, nil
}
func (corsType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (corsType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

func corsTool() declarations.Tool {
	tool := testTool()
	tool.ResourceTypes = []declarations.ResourceType{corsType{fallback: []string{"origins"}}}
	return tool
}

func corsRule(id string, origins ...string) declarations.Item {
	o := []declarations.Value{}
	for _, s := range origins {
		o = append(o, s)
	}
	it := declarations.Item{Fields: M{"origins": o, "methods": []declarations.Value{"GET"}}}
	if id != "" {
		it.Fields["id"] = id
	}
	return it
}

// A child holding none of its collection's key fields is identified by the
// fallback key: two rules without an id are two children, matched by their
// origins, reported by them, and rendered by import without an id.
func TestAChildWithoutItsKeyIsIdentifiedByTheFallbackKey(t *testing.T) {
	tool := corsTool()
	p := newProject(t, "example")
	declare(t, p, "test-cors", "assets", "unmanaged = []\n[[rule]]\nid = \"site\"\norigins = [\"https://example.org\"]\nmethods = [\"GET\"]\n[[rule]]\norigins = [\"https://a.example\"]\nmethods = [\"GET\"]\n")
	s, err := declarations.Load(tool, p)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(declarations.Resource{Type: "test-cors", Name: "assets", Children: map[string][]declarations.Item{"rule": {
		corsRule("site", "https://example.org"), corsRule("", "https://a.example"), corsRule("", "https://b.example"),
	}}})
	claims := declarations.Claims{Root: t.TempDir(), Claims: []declarations.Claim{{ResourceType: "test-cors", Name: "assets", Project: "example"}}}
	r, err := declarations.Compare(s, l, claims)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Differences) != 1 || r.Differences[0].Status != declarations.StatusUnexpected || !reflect.DeepEqual(r.Differences[0].Key, M{"origins": []declarations.Value{"https://b.example"}}) {
		t.Fatalf("differences %+v", r.Differences)
	}
	if live, _ := r.Differences[0].Live.(map[string]declarations.Value); !reflect.DeepEqual(live, M{"methods": []declarations.Value{"GET"}}) {
		t.Fatalf("live %+v", r.Differences[0].Live)
	}
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatal(err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, []string{"delete test-cors/assets rule [https://b.example]"}) {
		t.Fatalf("changes %q", got)
	}

	set, err := declarations.LoadForImport(tool, newProject(t, "example"), "test-cors", "assets")
	if err != nil {
		t.Fatal(err)
	}
	b, err := declarations.RenderImport(set, declarations.Resource{Type: "test-cors", Name: "assets", Children: map[string][]declarations.Item{"rule": {corsRule("", "https://b.example"), corsRule("site", "https://example.org")}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "id = ") != 1 || !strings.Contains(string(b), "[[rule]]\norigins = [\"https://b.example\"]\n") {
		t.Fatalf("import:\n%s", b)
	}
}

// Two rules without an id sharing their origins share a key, and are
// refused as one child declared twice.
func TestTwoChildrenSharingTheirFallbackKeyAreRefused(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-cors", "assets", "unmanaged = []\n[[rule]]\norigins = [\"https://a.example\"]\nmethods = [\"GET\"]\n[[rule]]\norigins = [\"https://a.example\"]\nmethods = [\"PUT\"]\n")
	_, err := declarations.Load(corsTool(), p)
	mustContain(t, err, "declares the rule", "twice")
}

// A fallback key names compared fields only.
func TestAFallbackKeyOutsideTheFieldsIsATypeBug(t *testing.T) {
	tool := testTool()
	tool.ResourceTypes = []declarations.ResourceType{corsType{fallback: []string{"hosts"}}}
	_, err := declarations.FindProject(tool, t.TempDir())
	mustContain(t, err, "FallbackKey", "hosts")
}
