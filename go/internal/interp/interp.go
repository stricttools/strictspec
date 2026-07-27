// Package interp is the strictspec REFERENCE INTERPRETER: the fourth conformance
// target. It is now a THIN WRAPPER over the shared emitter IR (internal/ir): it
// compiles a typed Schema plus its custom scalars into an ir.Program and runs
// ir.Execute over a parsed document. The validation logic lives once, in the IR
// executor, so the interpreter and the generated Go validators (via the public
// runtime) run the identical checks in the identical order — the four-target
// identity argument is realized by shared execution, not by parallel
// hand-written implementations.
package interp

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// maxValidationDepth mirrors the IR's pinned recursion-depth cap (kept as an
// unexported alias so interp-level tests can reference it).
const maxValidationDepth = ir.MaxValidationDepth

// Options carries the ambient inputs a single Validate call needs. It is the
// interpreter-facing spelling of ir.ExecOptions plus the custom-scalar registry
// (which the IR folds into the compiled Program).
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
// anchor context in opts. It compiles s into an ir.Program and delegates to
// ir.Execute — the same executor the generated Go target runs.
func Validate(s *schema.Schema, root doc.Node, opts Options) []diag.Diagnostic {
	prog := ir.Compile(s, opts.Scalars)
	return ir.Execute(prog, root, ir.ExecOptions{
		Format:    opts.Format,
		Evidence:  opts.Evidence,
		JSONL:     opts.JSONL,
		Line:      opts.Line,
		LineStart: opts.LineStart,
	})
}
