package declarations

import "testing"

// The live part of the diff identifier is sorted by resource type, then
// name: test-zone before test-zone-x, though "test-zone-x/a" sorts before
// "test-zone/a" as one string.
func TestDiffIDSortsLiveReadsByResourceTypeThenName(t *testing.T) {
	c := &comparison{set: Set{files: map[string]string{}}, rc: &recorder{reads: map[string]readResult{
		"test-zone-x/a": {resourceType: "test-zone-x", name: "a"},
		"test-zone/b":   {resourceType: "test-zone", name: "b"},
		"test-zone/a":   {resourceType: "test-zone", name: "a"},
	}}}
	entry := func(resourceType, name string) any {
		return map[string]any{"resource_type": resourceType, "name": name, "exists": false}
	}
	want := hashHex([]byte(canonicalJSON(map[string]any{
		"declarations": map[string]any{},
		"live":         []any{entry("test-zone", "a"), entry("test-zone", "b"), entry("test-zone-x", "a")},
		"differences":  []any{},
	})))
	if got := c.diffID(Report{}); got != want {
		t.Fatalf("diff_id %s; want %s, the live reads sorted by resource type then name", got, want)
	}
}
