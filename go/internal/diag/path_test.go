package diag

import "testing"

func TestPathRender(t *testing.T) {
	tests := []struct {
		name string
		path Path
		want string
	}{
		{"root", NewPath(), "$"},
		{"one key", NewPath(Key{"a"}), "$.a"},
		{"two keys", NewPath(Key{"a"}, Key{"b"}), "$.a.b"},
		{"index", NewPath(Key{"items"}, Index{0}), "$.items[0]"},
		{"index large", NewPath(Key{"items"}, Index{42}), "$.items[42]"},
		{"hyphen and underscore idents", NewPath(Key{"a-b"}, Key{"c_d"}), "$.a-b.c_d"},
		// Non-identifier keys switch to the quoted map-key form.
		{"map key with space", NewPath(Key{"config"}, Key{"weird key"}), `$.config["weird key"]`},
		{"map key leading digit", NewPath(Key{"m"}, Key{"1x"}), `$.m["1x"]`},
		{"map key empty", NewPath(Key{"m"}, Key{""}), `$.m[""]`},
		// A.2 escaping inside map keys.
		{"map key with quote", NewPath(Key{`a"b`}), `$["a\"b"]`},
		{"map key with backslash", NewPath(Key{`a\b`}), `$["a\\b"]`},
		{"map key with newline", NewPath(Key{"a\nb"}), `$["a\nb"]`},
		{"map key with tab", NewPath(Key{"a\tb"}), `$["a\tb"]`},
		{"map key with control char", NewPath(Key{"a\x01b"}), `$["a\u0001b"]`},
		// Arm steps.
		{"arm step", NewPath(Key{"shape"}, Arm{"gradient"}, Key{"stops"}, Index{0}), "$.shape(gradient).stops[0]"},
		// JSONL anchors.
		{"anchor at root", NewPath().WithAnchor(42, 0), "$@L42:0"},
		{"anchor after key", NewPath(Key{"budget"}).WithAnchor(42, 17), "$.budget@L42:17"},
		{"anchor after index", NewPath(Key{"rows"}, Index{3}).WithAnchor(3, 12), "$.rows[3]@L3:12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.path.Render(); got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsIdentShaped(t *testing.T) {
	shaped := []string{"a", "abc", "_x", "a1", "a-b", "c_d", "A", "Content-Type", "x-y-z"}
	for _, s := range shaped {
		if !IsIdentShaped(s) {
			t.Errorf("IsIdentShaped(%q) = false, want true", s)
		}
	}
	notShaped := []string{"", "1x", "-a", "a b", "a.b", "a/b", `a"b`, "trailing ", "é"}
	for _, s := range notShaped {
		if IsIdentShaped(s) {
			t.Errorf("IsIdentShaped(%q) = true, want false", s)
		}
	}
}

func TestEscapeString(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"plain", "plain"},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		{"a\nb", `a\nb`},
		{"a\rb", `a\rb`},
		{"a\tb", `a\tb`},
		{"a\x00b", `a\u0000b`},
		{"a\x1fb", `a\u001fb`},
		{"unicodé", "unicodé"}, // non-ASCII emitted verbatim as UTF-8
		{"emoji \U0001F600", "emoji \U0001F600"},
	}
	for _, tt := range tests {
		if got := EscapeString(tt.in); got != tt.want {
			t.Errorf("EscapeString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDiagnosticsAccumulationOrder(t *testing.T) {
	var d Diagnostics
	if d.Len() != 0 {
		t.Fatalf("zero value Len = %d, want 0", d.Len())
	}
	d.EmitCode("STRICTSPEC_A", NewPath(Key{"x"}), nil)
	d.Emit(Diagnostic{Code: "STRICTSPEC_B", Path: NewPath(Key{"y"})})
	d.EmitCode("STRICTSPEC_C", NewPath(Key{"z"}), nil)
	got := d.All()
	want := []string{"STRICTSPEC_A", "STRICTSPEC_B", "STRICTSPEC_C"}
	if len(got) != len(want) {
		t.Fatalf("All() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Code != want[i] {
			t.Errorf("All()[%d].Code = %q, want %q (emission order must be preserved)", i, got[i].Code, want[i])
		}
	}
}
