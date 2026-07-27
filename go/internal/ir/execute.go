package ir

import (
	"fmt"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// exec holds one document-validation pass's state. It is the concrete IR
// executor: each method realizes one or more of the appendix IR nodes (see the
// package doc mapping).
type exec struct {
	p        *Program
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

// p2task is a deferred phase-2 constraint run for one record (the
// constraint-eval node, deferred until phase 1 completes).
type p2task struct {
	typ  *schema.Type
	rec  doc.Node
	path diag.Path
}

// gate realizes the version-gate node: it applies the document format_version
// gate FIRST and is TERMINAL on failure (returns false; no structural or domain
// diagnostics follow).
func (v *exec) gate(root doc.Node) bool {
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

// emit realizes the emit node: it appends a diagnostic in emission order,
// applying the JSONL @L anchor (line + within-line byte offset of anchorNode)
// when validating a JSONL line.
func (v *exec) emit(code string, path diag.Path, anchorNode doc.Node, slots map[string]diag.Slot) {
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
