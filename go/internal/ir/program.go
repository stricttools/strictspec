// Package ir is the strictspec SHARED EMITTER IR and its executor
// (spec/appendix-emitter-ir.md). It is the single intermediate representation
// from which every target's validator is driven: the reference interpreter
// (internal/interp) and the generated Go validators (via the public runtime,
// go/strictspec) both compile a schema into an ir.Program and run the SAME
// ir.Execute over it. That shared execution is the mechanism behind the
// four-target verdict+code+path+message identity guarantee — the identity
// argument is real (one executor), not aspirational (four hand-written checks
// that merely agree).
//
// # The IR node set (appendix-emitter-ir.md §2), mapped to this executor
//
// The appendix pins a small, closed node set in two families. This executor
// realizes each node as an operation over the compiled Program; the mapping is:
//
//	SCHEMA-SHAPE NODES              executor operation (this package)
//	  record-open / record-close     walkRecord (closed-scope + unknown-key + required)
//	  key-presence                   walkRecord field pass (required/optional/alias)
//	  type-dispatch                  walkInner (dispatch on schema.Kind / node kind)
//	  union-dispatch                 walkDiscriminated / walkNodeKind
//	  scalar-check                   walkScalar + validate{String,Integer,Float,Number,Bool,Datetime,CustomScalar}
//	  constraint-eval                runConstraints (the closed vocabulary; §2.1)
//	  depth-guard                    walk depth cap (MaxValidationDepth)
//	  alias-resolution               walkRecord alias pass
//	  version-gate                   gate (runs FIRST, terminal on failure)
//	  import-resolution              Compile (imports merged into Program.Types)
//	  enum-arm-table                 walkEnum over baked arms
//
//	DIAGNOSTIC-EMISSION NODES       executor operation
//	  emit(code, slot-bindings)      exec.emit
//	  path-push / path-pop           the diag.Path threaded through walk (append*)
//	  one-pass-accumulate            diag.Diagnostics (emission order; no reorder)
//
// Ordering is a property of the IR, not of any target: the executor fixes the
// traversal and emission order once, so every target that runs it accumulates
// diagnostics in the identical order.
//
// What is NOT in the IR (appendix §4): no expressions (constraint forms are a
// closed named vocabulary, conditions are the closed six-kind set), no consumer
// hooks, no target-specific nodes, no severity. Those exclusions keep the
// four-target identity tractable.
package ir

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// MaxValidationDepth is the pinned recursion-depth cap (spec/DESIGN.md, Construct
// set: a pinned max validation depth with its own canonical diagnostic, fired
// before stack exhaustion). Every nesting level costs multiple frames per target;
// 128 is far below any runtime's stack limit yet far above realistic nesting.
const MaxValidationDepth = 128

// Program is a compiled, self-contained IR program: a resolved schema (imports
// merged, enum arms baked, refinements attached) plus its bound custom scalars.
// It is the "compiled-IR structure" both the interpreter and generated code
// drive off. It is produced by Compile and consumed by Execute.
type Program struct {
	// Schema is the resolved schema model. Its Types map is self-contained:
	// import-resolution has already merged every referenced named type in, so
	// Execute never touches the filesystem.
	Schema *schema.Schema
	// Scalars is the bound custom-scalar registry (name -> registration).
	Scalars map[string]*schema.Scalar
}

// SchemaName returns the compiled schema's name (used by the version gate's
// remediation payload and by the pairing/embedding machinery).
func (p *Program) SchemaName() string { return p.Schema.Name }

// FormatVersion returns the schema's declared document format_version.
func (p *Program) FormatVersion() int64 { return p.Schema.FormatVersion }

// Compile lowers a resolved schema plus its custom-scalar registry into an
// executable Program. The schema's imports MUST already be resolved into
// s.Types (the caller runs schema.ResolveImports, or the runtime's in-memory
// equivalent, first) — Compile performs no IO. This is the single lowering used
// by every target: the interpreter compiles the on-disk schema; the generator
// compiles the same schema at gen time and the runtime compiles the embedded
// schema at load time, so all targets execute the identical Program.
func Compile(s *schema.Schema, scalars map[string]*schema.Scalar) *Program {
	if scalars == nil {
		scalars = map[string]*schema.Scalar{}
	}
	return &Program{Schema: s, Scalars: scalars}
}

// ExecOptions carries the per-run ambient inputs a single Execute call needs:
// the document's surface format, the cross-document resolver evidence (empty for
// structural-only runs), and the JSONL per-line anchor context.
type ExecOptions struct {
	Format   doc.Format
	Evidence map[string][]map[string]any

	// JSONL per-line anchor context.
	JSONL     bool
	Line      int
	LineStart int
}

// Execute validates one document (root node) against the compiled Program and
// returns the ordered diagnostics the constitution pins: the version gate first
// (terminal on failure), then one-pass structural accumulation in the pinned
// traversal order, then the phase-2 constraint vocabulary over records whose
// phase 1 passed. For JSONL it is called once per line with the line's anchor
// context in opts.
func Execute(p *Program, root doc.Node, opts ExecOptions) []diag.Diagnostic {
	v := &exec{
		p:         p,
		s:         p.Schema,
		scalars:   p.Scalars,
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
	rt, ok := v.s.Types[v.s.Root]
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
