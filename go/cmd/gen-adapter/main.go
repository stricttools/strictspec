// Command gen-adapter is the Go-side invocation contract for the strictspec
// conformance harness's `go` (generated-code) target. It reads a JSON request on
// stdin describing one fixture and writes the observed outcome as JSON on stdout
// in the runner's shape:
//
//	{"valid": bool, "diagnostics": [{"code","path","message"}, ...]}
//
// For a normal schema it GENERATES a Go validator for the fixture's schema
// (cached per schema), COMPILES it against the runtime, and RUNS it on the
// fixture input — proving the emitter's output compiles and validates. For the
// built-in meta-schema (validating a schema/type-definition file AS a document)
// it delegates to the same meta-schema reader the interpreter target uses:
// schema validation is a single-source toolchain function, not per-target
// generated code (consumers validate documents, never schemas), so both targets
// share the reader's golden output and stay in parity.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	strictspecroot "github.com/smm-h/strictspec/go"
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/emit"
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
	RuntimeDir  string                      `json:"runtime_dir"`
	CacheDir    string                      `json:"cache_dir"`
	Version     string                      `json:"version"`
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

	// Meta-schema mode: validate the input AS a schema/type-definition document
	// via the shared reader (same as the interpreter target).
	s, _, err := schema.LoadFile(req.Schema)
	if err != nil {
		fail(err.Error())
	}
	if s.Name == "strictspec-meta-schema" {
		writeResponse(metaValidate(req))
		return
	}

	version := req.Version
	if version == "" {
		// Default to THIS toolchain build's version so the generated code's
		// pairing guard matches the runtime it compiles against (both embed
		// go/VERSION). A hardcoded "0.0.0" here mismatched the moment the
		// package was bumped off 0.0.0.
		version = strictspecroot.Version
	}
	built, err := emit.Build(req.Schema, req.CacheDir, req.RuntimeDir, version)
	if err != nil {
		fail(err.Error())
	}

	src, err := readInput(req)
	if err != nil {
		fail(err.Error())
	}
	runnerReq, _ := json.Marshal(map[string]any{
		"input":    string(src),
		"syntax":   req.InputSyntax,
		"evidence": req.Evidence,
	})
	cmd := exec.Command(built.BinPath)
	cmd.Stdin = strings.NewReader(string(runnerReq))
	out, rerr := cmd.Output()
	if rerr != nil {
		fail("generated validator runner failed: " + rerr.Error())
	}
	// The runner already emits the exact harness shape; pass it through.
	os.Stdout.Write(out)
}

// metaValidate reads the input file AS a schema/type-definition file and returns
// the meta-schema reader's authoring diagnostics (shared with the interpreter
// target).
func metaValidate(req request) []diag.Diagnostic {
	src, err := readInput(req)
	if err != nil {
		fail(err.Error())
	}
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return []diag.Diagnostic{parseDiag(perr)}
	}
	dir := ""
	if req.InputPath != "" {
		dir = req.InputPath
	}
	_, diags := schema.ReadSchema(d.Root, dir)
	return diags
}

func readInput(req request) ([]byte, error) {
	if req.InputInline != "" {
		return []byte(req.InputInline), nil
	}
	return os.ReadFile(req.InputPath)
}

func parseDiag(pe *doc.ParseError) diag.Diagnostic {
	// Only the JSONL parse template carries a {line} slot; the JSON and TOML
	// templates do not, so binding `line` there makes render.Render panic on an
	// unknown slot (render's slot-coverage invariant).
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	slots := map[string]diag.Slot{"detail": diag.SlotString{S: pe.Message}}
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
		slots["line"] = diag.SlotInt{N: int64(pe.Position.Line)}
	}
	return diag.Diagnostic{
		Code:  code,
		Path:  diag.NewPath(),
		Slots: slots,
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
	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
		fail("encode: " + err.Error())
	}
}

func fail(msg string) {
	json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg}) //nolint:errcheck
	os.Exit(2)
}
