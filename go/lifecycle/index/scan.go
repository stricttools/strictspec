package index

import (
	"sort"
	"strings"
	"unicode"
)

// Match is one occurrence of a confidential name: the name as the index spells
// it, and the 1-based line and column (in characters) where it starts.
type Match struct {
	Term   string
	Line   int
	Column int
}

// ScanTerms finds every occurrence of every term in text. Matching ignores
// case and is on whole tokens: an occurrence counts only where the characters
// on both sides of it are outside [A-Za-z0-9_-] (or are the start or end of a
// line). Matches come in line, column, and term order.
func ScanTerms(text string, terms []string) []Match {
	type term struct {
		spelled string
		lower   []rune
	}
	var ts []term
	for _, t := range terms {
		if strings.TrimSpace(t) == "" {
			continue
		}
		ts = append(ts, term{spelled: t, lower: lowerRunes(t)})
	}
	var out []Match
	for li, line := range strings.Split(text, "\n") {
		lr := lowerRunes(line)
		for _, t := range ts {
			n := len(t.lower)
			for start := 0; start+n <= len(lr); start++ {
				if !equalRunes(lr[start:start+n], t.lower) {
					continue
				}
				if start > 0 && tokenRune(lr[start-1]) {
					continue
				}
				if start+n < len(lr) && tokenRune(lr[start+n]) {
					continue
				}
				out = append(out, Match{Term: t.spelled, Line: li + 1, Column: start + 1})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		if out[i].Column != out[j].Column {
			return out[i].Column < out[j].Column
		}
		return out[i].Term < out[j].Term
	})
	return out
}

// lowerRunes lowers each character on its own, so the result has one rune per
// rune of s and columns stay aligned with the original text.
func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

func equalRunes(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tokenRune reports whether r continues a token: [A-Za-z0-9_-].
func tokenRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
