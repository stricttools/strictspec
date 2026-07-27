// Package interp is the strictspec REFERENCE INTERPRETER: given a typed Schema
// (from internal/schema) and a parsed document (internal/doc), it produces the
// ordered diagnostics the constitution pins — the version gate first (and, on
// failure, terminal), then one-pass structural accumulation in the pinned
// traversal order, then the phase-2 constraint vocabulary over records whose
// phase 1 passed. Messages are rendered from the spec-pinned catalogue via
// internal/render; this package only emits structured diag.Diagnostic values.
package interp

import (
	"fmt"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// validator holds one document-validation pass's state.
type validator struct {
	s        *schema.Schema
	scalars  map[string]*schema.Scalar
	root     doc.Node
	format   doc.Format
	evidence map[string][]map[string]any

	// JSONL anchor context (per line).
	jsonl     bool
	line      int
	lineStart int

	diags  diag.Diagnostics
	phase2 []p2task
	clean  map[doc.Node]bool
	depth  int
}

// p2task is a deferred phase-2 constraint run for one record.
type p2task struct {
	typ  *schema.Type
	rec  doc.Node
	path diag.Path
}

// Options carries the ambient inputs a single Validate call needs.
type Options struct {
	Scalars   map[string]*schema.Scalar
	Format    doc.Format
	Evidence  map[string][]map[string]any
	JSONL     bool
	Line      int
	LineStart int
}

// Validate validates one document (root node) against schema s and returns the
// ordered diagnostics. For JSONL it is called once per line with the line's
// anchor context in opts.
func Validate(s *schema.Schema, root doc.Node, opts Options) []diag.Diagnostic {
	v := &validator{
		s:         s,
		scalars:   opts.Scalars,
		root:      root,
		format:    opts.Format,
		evidence:  opts.Evidence,
		jsonl:     opts.JSONL,
		line:      opts.Line,
		lineStart: opts.LineStart,
		clean:     map[doc.Node]bool{},
	}
	// The version gate runs first and is TERMINAL on failure (DESIGN.md gate-terminal
	// amendment): no structural or domain diagnostics for a document that failed the gate.
	if !v.gate(root) {
		return v.diags.All()
	}
	rt, ok := s.Types[s.Root]
	if !ok {
		return v.diags.All()
	}
	// Phase 1: structural, one-pass, pinned traversal order.
	v.walk(rt, root, diag.NewPath())
	// Phase 2: constraint vocabulary over records whose phase 1 passed.
	for _, task := range v.phase2 {
		if v.clean[task.rec] {
			v.runConstraints(task)
		}
	}
	return v.diags.All()
}

// gate applies the document format_version gate. Returns false (terminal) on any
// gate failure.
func (v *validator) gate(root doc.Node) bool {
	invocation := fmt.Sprintf("strictspec migrate --schema %s --to %d <paths>", v.s.Name, v.s.FormatVersion)
	base := map[string]diag.Slot{
		"schema":     diag.SlotIdentifier{Name: v.s.Name},
		"expected":   diag.SlotVersion{V: v.s.FormatVersion},
		"invocation": diag.SlotString{S: invocation},
	}
	fv, ok := entryOf(root, "format_version")
	if !ok {
		v.emit("STRICTSPEC_GATE_ABSENT", diag.NewPath(), root, base)
		return false
	}
	if fv.Kind() != doc.Integer {
		slots := cloneSlots(base)
		slots["got"] = diag.SlotValue{V: v.valueOf(fv)}
		v.emit("STRICTSPEC_GATE_WRONG_TYPE", diag.NewPath(), root, slots)
		return false
	}
	got := svalIntLexeme(fv.Lexeme())
	if got != v.s.FormatVersion {
		slots := cloneSlots(base)
		slots["got"] = diag.SlotVersion{V: got}
		slots["migset"] = diag.SlotIdentifier{Name: v.s.Name}
		v.emit("STRICTSPEC_GATE_UNSUPPORTED", diag.NewPath(), root, slots)
		return false
	}
	return true
}

// emit appends a diagnostic, applying the JSONL @L anchor (line + within-line
// byte offset of anchorNode) when validating a JSONL line.
func (v *validator) emit(code string, path diag.Path, anchorNode doc.Node, slots map[string]diag.Slot) {
	if v.jsonl {
		off := 0
		if anchorNode != nil {
			sp := anchorNode.Span()
			if sp.Start.IsValid() {
				off = sp.Start.ByteOffset - v.lineStart
				if off < 0 {
					off = 0
				}
			}
		}
		path = path.WithAnchor(v.line, off)
	}
	v.diags.EmitCode(code, path, slots)
}

func cloneSlots(m map[string]diag.Slot) map[string]diag.Slot {
	out := make(map[string]diag.Slot, len(m)+2)
	for k, val := range m {
		out[k] = val
	}
	return out
}

func svalIntLexeme(lexeme string) int64 {
	var n int64
	neg := false
	for i, c := range lexeme {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		return -n
	}
	return n
}
