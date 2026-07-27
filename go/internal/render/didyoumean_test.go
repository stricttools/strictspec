package render

import (
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
)

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"colr", "color", 1},
		{"gren", "green", 1},
		{"gren", "red", 2},
		{"Foo", "foo", 1}, // case-SENSITIVE
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSuggestionThresholdBoundary(t *testing.T) {
	// "abcd" vs a candidate at distance exactly 2 qualifies; distance 3 does not.
	s := diag.SlotSuggestion{
		Unknown: "abcd",
		Candidates: []string{
			"abXY",  // distance 2 (two substitutions) -> IN
			"aXYZW", // distance 4 -> OUT
			"XYZWV", // distance 5 -> OUT
		},
	}
	got := renderSuggestion(s)
	want := " Did you mean abXY?"
	if got != want {
		t.Errorf("threshold boundary: got %q, want %q", got, want)
	}
}

func TestSuggestionEmptyBelowThreshold(t *testing.T) {
	s := diag.SlotSuggestion{Unknown: "abcd", Candidates: []string{"wxyz", "qrst"}}
	if got := renderSuggestion(s); got != "" {
		t.Errorf("no candidate within threshold: got %q, want empty", got)
	}
	// No candidates at all.
	if got := renderSuggestion(diag.SlotSuggestion{Unknown: "abcd"}); got != "" {
		t.Errorf("no candidates: got %q, want empty", got)
	}
}

func TestSuggestionTieBreakAlphabetical(t *testing.T) {
	// All three candidates are at edit distance 1 from "cat"; the tie is broken
	// alphabetically (code-point order): "bat", "cab", "cot" -> but we insert a
	// fourth that also ties to exercise the max-3 cap plus alphabetical order.
	s := diag.SlotSuggestion{
		Unknown:    "cat",
		Candidates: []string{"cot", "bat", "car", "cab"}, // all distance 1
	}
	got := renderSuggestion(s)
	// Alphabetical: bat, cab, car, cot -> first three: bat, cab, car.
	want := " Did you mean bat, cab, or car?"
	if got != want {
		t.Errorf("tie-break/max-3: got %q, want %q", got, want)
	}
}

func TestSuggestionClauseForms(t *testing.T) {
	// One candidate.
	one := diag.SlotSuggestion{Unknown: "colr", Candidates: []string{"color"}}
	if got := renderSuggestion(one); got != " Did you mean color?" {
		t.Errorf("one: got %q", got)
	}
	// Two candidates (both distance 1): "or" join, no Oxford comma.
	two := diag.SlotSuggestion{Unknown: "bar", Candidates: []string{"baz", "car"}}
	if got := renderSuggestion(two); got != " Did you mean baz or car?" {
		t.Errorf("two: got %q", got)
	}
	// Three candidates: Oxford comma before "or".
	three := diag.SlotSuggestion{Unknown: "bat", Candidates: []string{"bad", "bam", "ban"}}
	if got := renderSuggestion(three); got != " Did you mean bad, bam, or ban?" {
		t.Errorf("three: got %q", got)
	}
}

func TestSuggestionNonIdentCandidateQuoted(t *testing.T) {
	// A candidate that is not identifier-shaped renders as a double-quoted string
	// (A.1); ident-shaped candidates render bare.
	s := diag.SlotSuggestion{Unknown: "a b", Candidates: []string{"a c"}} // "a c" has a space
	got := renderSuggestion(s)
	want := ` Did you mean "a c"?`
	if got != want {
		t.Errorf("non-ident candidate: got %q, want %q", got, want)
	}
}
