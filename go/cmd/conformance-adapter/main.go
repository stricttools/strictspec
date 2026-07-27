// Command conformance-adapter is the Go-side invocation contract for the
// strictspec conformance harness's `interpreter` target. It reads a JSON request
// on stdin describing one fixture (schema path + input document + optional
// cross-document evidence), runs the reference interpreter (or, when the schema
// is the built-in meta-schema, the meta-schema reader over the input schema), and
// writes the observed outcome as JSON on stdout in the runner's expected shape:
//
//	{"valid": bool, "diagnostics": [{"code","path","message"}, ...]}
//
// The harness's Python `interpreter` target builds this binary once and invokes
// it per fixture (see conformance/harness/targets.py).
package main

import (
	"encoding/json"
	"os"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/interp"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

type request struct {
	Schema      string                      `json:"schema"`
	InputPath   string                      `json:"input_path"`
	InputInline string                      `json:"input_inline"`
	InputSyntax string                      `json:"input_syntax"`
	Evidence    map[string][]map[string]any `json:"evidence"`
}

type observed struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type response struct {
	Valid       bool       `json:"valid"`
	Diagnostics []observed `json:"diagnostics"`
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fail("bad request: " + err.Error())
	}
	diags, err := run(req)
	if err != nil {
		fail(err.Error())
	}
	writeResponse(diags)
}

func run(req request) ([]diag.Diagnostic, error) {
	s, sdiags, err := schema.LoadFile(req.Schema)
	if err != nil {
		return nil, err
	}

	// Meta-schema mode: the "document" is itself a schema/type-definition file,
	// validated AS a document of the meta-schema — the reader's authoring
	// diagnostics ARE the outcome.
	if s.Name == "strictspec-meta-schema" {
		return metaValidate(req)
	}

	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) > 0 {
		// A schema-authoring problem in a normal fixture: surface it (it will
		// mismatch a valid-schema fixture, exposing the bug).
		return sdiags, nil
	}
	scalars := schema.LoadManifestScalars(s.Dir)

	src, err := readInput(req)
	if err != nil {
		return nil, err
	}

	switch req.InputSyntax {
	case "jsonl":
		return validateJSONL(s, scalars, src, req.Evidence)
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}, nil
		}
		return interp.Validate(s, d.Root, interp.Options{
			Scalars: scalars, Format: doc.FormatTOML, Evidence: req.Evidence,
		}), nil
	default: // json
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}, nil
		}
		return interp.Validate(s, d.Root, interp.Options{
			Scalars: scalars, Format: doc.FormatJSON, Evidence: req.Evidence,
		}), nil
	}
}

// metaValidate reads the input file AS a schema/type-definition file and returns
// the meta-schema reader's authoring diagnostics.
func metaValidate(req request) ([]diag.Diagnostic, error) {
	src, err := readInput(req)
	if err != nil {
		return nil, err
	}
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return []diag.Diagnostic{parseDiag(perr)}, nil
	}
	dir := ""
	if req.InputPath != "" {
		dir = req.InputPath
	}
	_, diags := schema.ReadSchema(d.Root, dir)
	return diags, nil
}

func validateJSONL(s *schema.Schema, scalars map[string]*schema.Scalar, src []byte, ev map[string][]map[string]any) ([]diag.Diagnostic, error) {
	docs, perr := jsondoc.ParseLines(src)
	if perr != nil {
		return []diag.Diagnostic{parseDiag(perr)}, nil
	}
	lineStarts := computeLineStarts(src)
	var out []diag.Diagnostic
	for i, d := range docs {
		ls := 0
		if i < len(lineStarts) {
			ls = lineStarts[i]
		}
		out = append(out, interp.Validate(s, d.Root, interp.Options{
			Scalars: scalars, Format: doc.FormatJSONL, Evidence: ev,
			JSONL: true, Line: i + 1, LineStart: ls,
		})...)
	}
	return out, nil
}

func computeLineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func readInput(req request) ([]byte, error) {
	if req.InputInline != "" {
		return []byte(req.InputInline), nil
	}
	return os.ReadFile(req.InputPath)
}

func parseDiag(pe *doc.ParseError) diag.Diagnostic {
	// Minimal parse-error surfacing (no fixture currently exercises a parse
	// failure through the interpreter target).
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
	}
	return diag.Diagnostic{
		Code: code,
		Path: diag.NewPath(),
		Slots: map[string]diag.Slot{
			"detail": diag.SlotString{S: pe.Message},
			"line":   diag.SlotInt{N: int64(pe.Position.Line)},
		},
	}
}

func writeResponse(diags []diag.Diagnostic) {
	resp := response{Valid: len(diags) == 0, Diagnostics: []observed{}}
	for _, d := range diags {
		resp.Diagnostics = append(resp.Diagnostics, observed{
			Code:    d.Code,
			Path:    d.Path.Render(),
			Message: render.Render(d),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(resp); err != nil {
		fail("encode: " + err.Error())
	}
}

func fail(msg string) {
	json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg}) //nolint:errcheck
	os.Exit(2)
}
