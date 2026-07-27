package strictspec

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// Diagnostic is one validation error: a stable STRICTSPEC_* code, the rendered
// path it is attached to, and the pinned message text (rendered from the
// spec-pinned template — the runtime carries no hand-written message strings).
// There is no severity field: every diagnostic is an error.
type Diagnostic struct {
	Code    string
	Path    string
	Message string
}

// Result is a validation outcome: whether the document validated, and the
// ordered diagnostics (empty iff Valid). Diagnostics are in emission order; the
// runtime never reorders them.
type Result struct {
	Valid       bool
	Diagnostics []Diagnostic
}

// Program is a compiled strictspec schema. Generated code builds one at package
// init from its embedded schema (CompileEmbedded) and calls Validate. It is the
// runtime handle to the shared emitter IR; validation runs the identical
// executor as the reference interpreter.
type Program struct {
	prog *ir.Program
}

// CompileEmbedded compiles a schema carried IN MEMORY by generated code: files
// maps each referenced file name (the schema, any imported type-definition
// files, and the scalar manifest) to its exact bytes, and mainFile names the
// schema entry point. Import resolution and custom-scalar binding happen against
// the FileSet, never disk — so a generated validator is self-contained and does
// no IO to compile. A schema-authoring problem in the embedded schema (which
// `strictspec gen` would already have rejected) surfaces as an error.
func CompileEmbedded(files map[string]string, mainFile string) (*Program, error) {
	fs := schema.FileSet(files)
	s, sdiags, err := schema.ParseFrom(fs, mainFile)
	if err != nil {
		return nil, err
	}
	sdiags = append(sdiags, schema.ResolveImportsFrom(s, fs)...)
	if len(sdiags) > 0 {
		return nil, embeddedSchemaError(sdiags)
	}
	scalars := schema.LoadManifestScalarsFrom(fs)
	return &Program{prog: ir.Compile(s, scalars)}, nil
}

// Validate is the RAW-BYTES entry point (Generated API contract, first entry
// point): lossless parse of input in the given syntax ("json" | "toml" |
// "jsonl"), then validate. Loading and validation are inseparable — there is no
// parse-without-validate mode. For JSONL every line is validated independently
// and diagnostics accumulate across lines with per-line anchors.
func (p *Program) Validate(input []byte, syntax string) Result {
	return p.ValidateWithEvidence(input, syntax, nil)
}

// ValidateWithEvidence is Validate plus cross-document resolver evidence for the
// phase-2 constraint vocabulary (count-limit / sum-limit and the other
// cross-document forms). An empty evidence map runs structural checks only.
func (p *Program) ValidateWithEvidence(input []byte, syntax string, evidence map[string][]map[string]any) Result {
	var diags []diag.Diagnostic
	switch syntax {
	case "jsonl":
		diags = p.validateJSONL(input, evidence)
	case "toml":
		d, perr := tomldoc.Parse(input)
		if perr != nil {
			diags = []diag.Diagnostic{parseDiag(perr)}
		} else {
			diags = ir.Execute(p.prog, d.Root, ir.ExecOptions{Format: doc.FormatTOML, Evidence: evidence})
		}
	default: // json
		d, perr := jsondoc.Parse(input)
		if perr != nil {
			diags = []diag.Diagnostic{parseDiag(perr)}
		} else {
			diags = ir.Execute(p.prog, d.Root, ir.ExecOptions{Format: doc.FormatJSON, Evidence: evidence})
		}
	}
	return render_(diags)
}

// ValidateValue is the TAGGED-VALUE entry point (Generated API contract, second
// entry point): validate an already-parsed tagged document value (from LoadValue
// or a generated typed constructor). Raw untagged maps are never accepted —
// ambiguity never enters the model.
func (p *Program) ValidateValue(v Value) Result {
	return p.ValidateValueWithEvidence(v, nil)
}

// ValidateValueWithEvidence is ValidateValue plus cross-document evidence.
func (p *Program) ValidateValueWithEvidence(v Value, evidence map[string][]map[string]any) Result {
	diags := ir.Execute(p.prog, v.node, ir.ExecOptions{Format: v.format, Evidence: evidence})
	return render_(diags)
}

func (p *Program) validateJSONL(src []byte, evidence map[string][]map[string]any) []diag.Diagnostic {
	docs, perr := jsondoc.ParseLines(src)
	if perr != nil {
		return []diag.Diagnostic{parseDiag(perr)}
	}
	starts := lineStarts(src)
	var out []diag.Diagnostic
	for i, d := range docs {
		ls := 0
		if i < len(starts) {
			ls = starts[i]
		}
		out = append(out, ir.Execute(p.prog, d.Root, ir.ExecOptions{
			Format: doc.FormatJSONL, Evidence: evidence,
			JSONL: true, Line: i + 1, LineStart: ls,
		})...)
	}
	return out
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// render_ turns the internal ordered diagnostics into the public Result,
// rendering each message from the spec-pinned template catalogue.
func render_(diags []diag.Diagnostic) Result {
	res := Result{Valid: len(diags) == 0, Diagnostics: make([]Diagnostic, 0, len(diags))}
	for _, d := range diags {
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Code:    d.Code,
			Path:    d.Path.Render(),
			Message: render.Render(d),
		})
	}
	return res
}

func parseDiag(pe *doc.ParseError) diag.Diagnostic {
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	slots := map[string]diag.Slot{"detail": diag.SlotString{S: pe.Message}}
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		// Only the JSONL parse template carries a {line} slot; the JSON and TOML
		// templates do not, so binding `line` there makes render.Render panic on
		// an unknown slot (render's slot-coverage invariant).
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
		slots["line"] = diag.SlotInt{N: int64(pe.Position.Line)}
	}
	return diag.Diagnostic{
		Code:  code,
		Path:  diag.NewPath(),
		Slots: slots,
	}
}
