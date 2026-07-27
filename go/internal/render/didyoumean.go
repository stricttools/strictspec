package render

import (
	"sort"

	"github.com/smm-h/strictspec/go/internal/diag"
)

// renderSuggestion computes the did-you-mean clause per appendix-rendering.md
// Part C: Levenshtein distance, case-SENSITIVE, threshold 2 (inclusive), at most
// 3 suggestions, primary sort ascending distance with alphabetical (code-point)
// tie-break. Returns "" when no candidate is within threshold; otherwise the
// leading-space clause " Did you mean ...?".
func renderSuggestion(s diag.SlotSuggestion) string {
	type cand struct {
		name string
		dist int
	}
	var cands []cand
	for _, c := range s.Candidates {
		d := levenshtein(s.Unknown, c)
		if d <= 2 {
			cands = append(cands, cand{name: c, dist: d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name // code-point order, ascending
	})
	if len(cands) > 3 {
		cands = cands[:3]
	}
	if len(cands) == 0 {
		return ""
	}

	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = renderCandidate(c.name)
	}
	switch len(names) {
	case 1:
		return " Did you mean " + names[0] + "?"
	case 2:
		return " Did you mean " + names[0] + " or " + names[1] + "?"
	default: // exactly 3 (capped above)
		return " Did you mean " + names[0] + ", " + names[1] + ", or " + names[2] + "?"
	}
}

// renderCandidate renders a suggestion candidate per A.1: bare when
// identifier-shaped, otherwise as a double-quoted string.
func renderCandidate(name string) string {
	if diag.IsIdentShaped(name) {
		return name
	}
	return renderQuotedString(name)
}

// levenshtein computes the case-sensitive edit distance between a and b over
// runes: insertions, deletions, and substitutions each cost 1; no transposition
// (Part C, metric 1).
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := 0; j <= len(rb); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
