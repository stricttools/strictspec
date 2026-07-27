// Package dpath parses and navigates the read-side path grammar
// (spec/appendix-rendering.md Part B) as used by migration op targets
// (appendix-surface-syntax.md §9 and §10) and by doc-diff. It is the write-side
// counterpart to the diag package's path RENDERER: diag renders a structured
// path to text; dpath parses path text and navigates a document tree to the
// addressed node (and its parent locator, for edits).
//
// The supported step set covers what op targets and doc-diff addresses use:
// `.ident` record keys, `["quoted"]` map/record keys, and `[i]` array indices,
// rooted at `$`. Arm steps `(name)` and JSONL `@L` suffixes are part of the
// grammar but are never op targets, so they are rejected here with a clear error.
package dpath

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// Step is one navigation step. The closed set is Key (a record field or quoted
// key) and Index (an array element).
type Step interface{ isStep() }

// Key is a record-field / map-key step. Name is the DECODED key.
type Key struct{ Name string }

// Index is a zero-based array-element step.
type Index struct{ N int }

func (Key) isStep()   {}
func (Index) isStep() {}

// Path is a parsed path: an ordered sequence of steps rooted at `$`.
type Path struct {
	Raw   string
	Steps []Step
}

// Parse parses a path string (`$.a.b[0]["k"]`) into a Path. A path outside the
// supported op-target grammar is an error.
func Parse(s string) (Path, error) {
	if s == "" || s[0] != '$' {
		return Path{}, fmt.Errorf("path %q must begin with $", s)
	}
	p := Path{Raw: s}
	i := 1
	for i < len(s) {
		switch s[i] {
		case '.':
			i++
			start := i
			for i < len(s) && s[i] != '.' && s[i] != '[' {
				i++
			}
			name := s[start:i]
			if name == "" {
				return Path{}, fmt.Errorf("path %q: empty key step", s)
			}
			p.Steps = append(p.Steps, Key{Name: name})
		case '[':
			i++
			if i < len(s) && s[i] == '"' {
				// Quoted key: ["..."] with A.2 escaping.
				j := i + 1
				var b strings.Builder
				for j < len(s) && s[j] != '"' {
					if s[j] == '\\' && j+1 < len(s) {
						b.WriteByte(s[j])
						b.WriteByte(s[j+1])
						j += 2
						continue
					}
					b.WriteByte(s[j])
					j++
				}
				if j >= len(s) || s[j] != '"' {
					return Path{}, fmt.Errorf("path %q: unterminated quoted key", s)
				}
				j++ // closing quote
				if j >= len(s) || s[j] != ']' {
					return Path{}, fmt.Errorf("path %q: expected ] after quoted key", s)
				}
				p.Steps = append(p.Steps, Key{Name: strdecode.JSON(`"` + b.String() + `"`)})
				i = j + 1
			} else {
				// Index: [digits].
				start := i
				for i < len(s) && s[i] != ']' {
					i++
				}
				if i >= len(s) {
					return Path{}, fmt.Errorf("path %q: unterminated index", s)
				}
				n, err := strconv.Atoi(s[start:i])
				if err != nil {
					return Path{}, fmt.Errorf("path %q: bad index %q", s, s[start:i])
				}
				p.Steps = append(p.Steps, Index{N: n})
				i++ // ]
			}
		default:
			return Path{}, fmt.Errorf("path %q: unexpected byte %q at %d", s, s[i], i)
		}
	}
	return p, nil
}

// Navigate walks root along p's steps and returns the addressed node. ok is
// false when any step does not resolve (missing key / out-of-range index /
// kind mismatch).
func Navigate(root doc.Node, p Path) (doc.Node, bool) {
	cur := root
	for _, st := range p.Steps {
		next, ok := stepInto(cur, st)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// Parent returns the CONTAINER holding the node addressed by p (i.e. the node at
// p minus its last step) and the last step itself. It is what edits need: the
// container plus the key/index locator. ok is false when the parent chain does
// not resolve. For a root path ($ with no steps) parent is nil.
func Parent(root doc.Node, p Path) (parent doc.Node, last Step, ok bool) {
	if len(p.Steps) == 0 {
		return nil, nil, false
	}
	cur := root
	for _, st := range p.Steps[:len(p.Steps)-1] {
		next, ok := stepInto(cur, st)
		if !ok {
			return nil, nil, false
		}
		cur = next
	}
	return cur, p.Steps[len(p.Steps)-1], true
}

func stepInto(n doc.Node, st Step) (doc.Node, bool) {
	if n == nil {
		return nil, false
	}
	switch s := st.(type) {
	case Key:
		if n.Kind() != doc.Record {
			return nil, false
		}
		for _, e := range n.Entries() {
			if e.Key == s.Name {
				return e.Value, true
			}
		}
		return nil, false
	case Index:
		if n.Kind() != doc.Array {
			return nil, false
		}
		items := n.Items()
		if s.N < 0 || s.N >= len(items) {
			return nil, false
		}
		return items[s.N], true
	default:
		return nil, false
	}
}
