package dpath

import (
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/write"
)

func parseNode(t *testing.T, format doc.Format, src string) doc.Node {
	t.Helper()
	d, err := write.New(format, []byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return d.Root()
}

// TestParseValid covers the supported op-target grammar: root, dotted keys,
// indices, and quoted (map/record) keys, including nesting.
func TestParseValid(t *testing.T) {
	cases := []struct {
		in    string
		steps []Step
	}{
		{"$", nil},
		{"$.a", []Step{Key{Name: "a"}}},
		{"$.a.b", []Step{Key{Name: "a"}, Key{Name: "b"}}},
		{"$.xs[0]", []Step{Key{Name: "xs"}, Index{N: 0}}},
		{"$.xs[10].k", []Step{Key{Name: "xs"}, Index{N: 10}, Key{Name: "k"}}},
		{`$["a.b"]`, []Step{Key{Name: "a.b"}}},
		{`$.m["weird key"].v`, []Step{Key{Name: "m"}, Key{Name: "weird key"}, Key{Name: "v"}}},
		{`$["esc\"aped"]`, []Step{Key{Name: `esc"aped`}}},
	}
	for _, c := range cases {
		p, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): unexpected error %v", c.in, err)
		}
		if len(p.Steps) != len(c.steps) {
			t.Fatalf("Parse(%q): got %d steps, want %d (%+v)", c.in, len(p.Steps), len(c.steps), p.Steps)
		}
		for i, want := range c.steps {
			switch w := want.(type) {
			case Key:
				got, ok := p.Steps[i].(Key)
				if !ok || got.Name != w.Name {
					t.Fatalf("Parse(%q) step %d = %+v, want Key{%q}", c.in, i, p.Steps[i], w.Name)
				}
			case Index:
				got, ok := p.Steps[i].(Index)
				if !ok || got.N != w.N {
					t.Fatalf("Parse(%q) step %d = %+v, want Index{%d}", c.in, i, p.Steps[i], w.N)
				}
			}
		}
		if p.Raw != c.in {
			t.Fatalf("Parse(%q): Raw = %q", c.in, p.Raw)
		}
	}
}

// TestParseErrors covers every rejection branch of the parser.
func TestParseErrors(t *testing.T) {
	bad := []string{
		"",         // empty
		"a.b",      // no leading $
		"$.",       // empty key step
		"$.a.",     // empty key step (trailing dot)
		"$[0",      // unterminated index
		"$[x]",     // bad index
		`$["oops]`, // unterminated quoted key
		`$["k"x]`,  // missing ] after quoted key
		"$,a",      // unexpected byte
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Fatalf("Parse(%q): expected error, got nil", s)
		}
	}
}

// TestNavigate resolves records, arrays, and reports misses.
func TestNavigate(t *testing.T) {
	root := parseNode(t, doc.FormatJSON, `{"a": {"b": [10, 20, 30]}, "c": "z"}`)

	p, _ := Parse("$.a.b[1]")
	n, ok := Navigate(root, p)
	if !ok || n.Kind() != doc.Integer || n.Lexeme() != "20" {
		t.Fatalf("Navigate $.a.b[1] = %v, ok=%v", n, ok)
	}

	// Missing key.
	pm, _ := Parse("$.a.z")
	if _, ok := Navigate(root, pm); ok {
		t.Fatal("Navigate to a missing key must not resolve")
	}
	// Out-of-range index.
	poor, _ := Parse("$.a.b[9]")
	if _, ok := Navigate(root, poor); ok {
		t.Fatal("Navigate to an out-of-range index must not resolve")
	}
	// Kind mismatch: indexing a record.
	pk, _ := Parse("$.a[0]")
	if _, ok := Navigate(root, pk); ok {
		t.Fatal("Navigate indexing a record must not resolve")
	}
	// Root path resolves to root.
	pr, _ := Parse("$")
	if n, ok := Navigate(root, pr); !ok || n != root {
		t.Fatalf("Navigate $ must resolve to root")
	}
}

// TestParent returns the container plus the final step.
func TestParent(t *testing.T) {
	root := parseNode(t, doc.FormatJSON, `{"a": {"b": 1}, "xs": [7, 8]}`)

	p, _ := Parse("$.a.b")
	parent, last, ok := Parent(root, p)
	if !ok || parent.Kind() != doc.Record {
		t.Fatalf("Parent $.a.b parent = %v, ok=%v", parent, ok)
	}
	if key, isKey := last.(Key); !isKey || key.Name != "b" {
		t.Fatalf("Parent $.a.b last = %+v, want Key{b}", last)
	}

	// Array-element parent.
	pi, _ := Parse("$.xs[1]")
	parentI, lastI, ok := Parent(root, pi)
	if !ok || parentI.Kind() != doc.Array {
		t.Fatalf("Parent $.xs[1] parent = %v, ok=%v", parentI, ok)
	}
	if idx, isIdx := lastI.(Index); !isIdx || idx.N != 1 {
		t.Fatalf("Parent $.xs[1] last = %+v, want Index{1}", lastI)
	}

	// Root path has no parent.
	pr, _ := Parse("$")
	if _, _, ok := Parent(root, pr); ok {
		t.Fatal("Parent of the root path must not resolve")
	}

	// Unresolvable parent chain.
	pu, _ := Parse("$.missing.b")
	if _, _, ok := Parent(root, pu); ok {
		t.Fatal("Parent with an unresolvable chain must not resolve")
	}
}
