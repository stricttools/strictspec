package diag

import "strconv"

// Path is a diagnostic path: an ordered sequence of steps rooted at the document
// root, optionally anchored to a JSONL stream position. It renders per the path
// grammar (appendix-rendering.md Part B) and is part of the conformance identity
// guarantee (byte-identical across all four targets).
//
// The first step of a well-formed path is Root; Render simply concatenates the
// rendering of each step in order, then appends the JSONL anchor if present.
type Path struct {
	Steps  []Step
	Anchor *JSONLAnchor
}

// Step is one element of a path. The closed set is Root, Key, Index, Arm.
type Step interface{ isStep() }

// Root renders the document root marker "$". A path always begins with it.
type Root struct{}

// Key is a record field or typed-map key. It renders bare as ".name" when the
// name is identifier-shaped, and switches to the quoted map-key form
// ["<escaped>"] otherwise (the index-then-key switching rule, Part B).
type Key struct{ Name string }

// Index is a zero-based array element, rendered "[n]".
type Index struct{ N int }

// Arm disambiguates which discriminated-union arm produced a nested diagnostic,
// rendered "(name)". The arm name is always the schema-declared arm identifier.
type Arm struct{ Name string }

// JSONLAnchor addresses a value within a JSONL stream: "@L<line>:<offset>".
// Line is one-based (human-facing); Offset is a zero-based byte offset within
// the line.
type JSONLAnchor struct {
	Line   int
	Offset int
}

func (Root) isStep()  {}
func (Key) isStep()   {}
func (Index) isStep() {}
func (Arm) isStep()   {}

// Render produces the path string per the Part B grammar.
func (p Path) Render() string {
	var out []byte
	for _, s := range p.Steps {
		switch st := s.(type) {
		case Root:
			out = append(out, '$')
		case Key:
			if IsIdentShaped(st.Name) {
				out = append(out, '.')
				out = append(out, st.Name...)
			} else {
				out = append(out, '[', '"')
				out = append(out, EscapeString(st.Name)...)
				out = append(out, '"', ']')
			}
		case Index:
			out = append(out, '[')
			out = strconv.AppendInt(out, int64(st.N), 10)
			out = append(out, ']')
		case Arm:
			out = append(out, '(')
			out = append(out, st.Name...)
			out = append(out, ')')
		default:
			panic("diag: unknown path step type")
		}
	}
	if p.Anchor != nil {
		out = append(out, '@', 'L')
		out = strconv.AppendInt(out, int64(p.Anchor.Line), 10)
		out = append(out, ':')
		out = strconv.AppendInt(out, int64(p.Anchor.Offset), 10)
	}
	return string(out)
}

// NewPath builds a Path rooted at "$" with the given steps (Root is prepended
// automatically). It is the convenience constructor emitters use.
func NewPath(steps ...Step) Path {
	all := make([]Step, 0, len(steps)+1)
	all = append(all, Root{})
	all = append(all, steps...)
	return Path{Steps: all}
}

// WithAnchor returns a copy of p anchored to the given JSONL line and offset.
func (p Path) WithAnchor(line, offset int) Path {
	p.Anchor = &JSONLAnchor{Line: line, Offset: offset}
	return p
}
