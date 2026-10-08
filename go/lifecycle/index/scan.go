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
// line). An occurrence lying inside a URL or a dotted hostname (see
// ignoredSpans) does not count; a term that reaches past one, such as a module
// path beginning with a hostname, still does. Matches come in line, column, and term order.
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
		ignored := ignoredSpans([]rune(line))
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
				if within(start, start+n, ignored) {
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

// span is a half-open range of rune indexes in a line.
type span struct{ from, to int }

// within reports whether the runes from..to lie inside one of spans.
func within(from, to int, spans []span) bool {
	for _, s := range spans {
		if from >= s.from && to <= s.to {
			return true
		}
	}
	return false
}

// hostnameSuffixes are the top-level domains a dotted name must end in to be
// read as a hostname. File extensions that are also country domains (.py,
// .md, .sh, .rs) are left out, so a file name such as name.py is not a
// hostname.
var hostnameSuffixes = map[string]bool{
	"com": true, "org": true, "net": true, "io": true, "dev": true, "app": true,
	"ai": true, "co": true, "edu": true, "gov": true, "info": true, "me": true,
	"xyz": true, "cloud": true, "page": true, "site": true, "tech": true,
}

// ignoredSpans are the parts of line no confidential name is matched in:
//
//   - a URL: a scheme ([A-Za-z][A-Za-z0-9+.-]*) followed by "://", up to the
//     next whitespace or one of the characters " ' < > ` ) ] }
//   - a dotted hostname: a run of [A-Za-z0-9_.-] (trailing dots dropped) of at
//     least two non-empty labels separated by dots whose last label is one of
//     hostnameSuffixes, such as docs.example.com.
func ignoredSpans(line []rune) []span {
	var out []span
	for i := 0; i+2 < len(line); i++ {
		if line[i] != ':' || line[i+1] != '/' || line[i+2] != '/' {
			continue
		}
		from := i
		for from > 0 && schemeRune(line[from-1]) {
			from--
		}
		for from < i && !isLetter(line[from]) {
			from++
		}
		if from == i {
			continue
		}
		to := i + 3
		for to < len(line) && !urlEnd(line[to]) {
			to++
		}
		out = append(out, span{from, to})
		i = to - 1
	}
	for i := 0; i < len(line); {
		if !tokenRune(line[i]) && line[i] != '.' {
			i++
			continue
		}
		from := i
		for i < len(line) && (tokenRune(line[i]) || line[i] == '.') {
			i++
		}
		to := i
		for to > from && line[to-1] == '.' {
			to--
		}
		labels := strings.Split(string(line[from:to]), ".")
		if len(labels) < 2 || !hostnameSuffixes[strings.ToLower(labels[len(labels)-1])] {
			continue
		}
		empty := false
		for _, l := range labels {
			empty = empty || l == ""
		}
		if !empty {
			out = append(out, span{from, to})
		}
	}
	return out
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

func schemeRune(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '+' || r == '.' || r == '-'
}

func urlEnd(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'<>`)]}", r)
}
