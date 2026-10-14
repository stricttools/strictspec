package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/internal/codes"
	"github.com/stricttools/strictspec/go/internal/diag"
	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/fixturetoml"
	"github.com/stricttools/strictspec/go/internal/interp"
	"github.com/stricttools/strictspec/go/internal/jsondoc"
	"github.com/stricttools/strictspec/go/internal/render"
	"github.com/stricttools/strictspec/go/internal/schema"
	"github.com/stricttools/strictspec/go/internal/tomldoc"
)

// The validation fixtures under testdata/fixtures/ are pure data: each case
// file names a schema, an input document, optional cross-document evidence,
// and the expected outcome (valid, or an ordered list of diagnostics carrying
// code, path, and slot values, from which the expected message is rendered).
// Directories whose name starts with `_` hold shared assets, not cases.
//
// TestValidationFixtures runs every case through both ways the Go toolchain
// validates a document, the reference interpreter and a generated validator
// compiled against the runtime, and asserts each one's ordered
// (code, path, message) outcome equals the case's expectation.

// fixtureCategories is the closed set of case categories; a case declaring any
// other category is malformed.
var fixtureCategories = map[string]bool{
	"construct-coverage": true, "unions": true, "numbers": true, "datetimes": true,
	"duplicate-keys": true, "tagged-entry-equivalence": true, "unknown-keys": true,
	"opaque-leaves": true, "shared-types": true, "enum-sourcing": true,
	"aggregates": true, "cross-document-constraints": true, "versioning": true,
	"boundary-invariant": true, "error-reporting": true, "write-side": true,
	"aliases": true, "edge-inputs": true, "unicode": true, "meta-schema": true,
}

// optionalSlots are the template slots a case may omit; they render empty.
var optionalSlots = map[string]bool{"suggestion": true}

var slotPattern = regexp.MustCompile(`\{(\w+)\}`)

type fixtureCase struct {
	file        string
	name        string
	schemaPath  string
	inputPath   string
	inputInline string
	syntax      string
	evidence    map[string][]map[string]any
	valid       bool
	want        []observedDiag
}

// loadFixtureCases reads and structurally validates every case under root. A
// malformed case is a test failure, never a skip.
func loadFixtureCases(t *testing.T, root string) []fixtureCase {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && strings.HasPrefix(d.Name(), "_") {
			return filepath.SkipDir
		}
		if !d.IsDir() && filepath.Ext(path) == ".toml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var cases []fixtureCase
	for _, f := range files {
		c, err := parseFixtureCase(root, f)
		if err != nil {
			t.Fatalf("malformed fixture %s: %v", f, err)
		}
		cases = append(cases, c)
	}
	return cases
}

func parseFixtureCase(root, file string) (fixtureCase, error) {
	data, err := fixturetoml.Load(file)
	if err != nil {
		return fixtureCase{}, err
	}
	str := func(key string) (string, bool) {
		v, ok := data[key].(string)
		return v, ok
	}
	c := fixtureCase{file: file}
	var ok bool
	if c.name, ok = str("name"); !ok {
		return c, fmt.Errorf("missing string key name")
	}
	category, ok := str("category")
	if !ok || !fixtureCategories[category] {
		return c, fmt.Errorf("category %q is not one of the fixture categories", category)
	}
	if _, ok := str("provenance"); !ok {
		return c, fmt.Errorf("missing string key provenance")
	}
	schemaRel, ok := str("schema")
	if !ok {
		return c, fmt.Errorf("missing string key schema")
	}
	c.schemaPath = filepath.Join(root, schemaRel)
	if _, err := os.Stat(c.schemaPath); err != nil {
		return c, fmt.Errorf("schema file: %w", err)
	}
	inputRel, hasFile := str("input")
	inline, hasInline := str("input_inline")
	if hasFile == hasInline {
		return c, fmt.Errorf("exactly one of input or input_inline is required")
	}
	c.syntax, _ = str("input_syntax")
	if hasFile {
		c.inputPath = filepath.Join(root, inputRel)
		if _, err := os.Stat(c.inputPath); err != nil {
			return c, fmt.Errorf("input file: %w", err)
		}
		if c.syntax == "" {
			c.syntax = map[string]string{".json": "json", ".toml": "toml", ".jsonl": "jsonl"}[filepath.Ext(c.inputPath)]
		}
	} else {
		c.inputInline = inline
	}
	switch c.syntax {
	case "json", "toml", "jsonl":
	default:
		return c, fmt.Errorf("input_syntax %q is not json, toml, or jsonl", c.syntax)
	}
	if ev, present := data["evidence"]; present {
		table, ok := ev.(map[string]any)
		if !ok {
			return c, fmt.Errorf("evidence must be a table")
		}
		c.evidence = map[string][]map[string]any{}
		for resolver, rows := range table {
			list, ok := rows.([]any)
			if !ok {
				return c, fmt.Errorf("evidence %q must be an array of tables", resolver)
			}
			for _, row := range list {
				m, ok := row.(map[string]any)
				if !ok {
					return c, fmt.Errorf("evidence %q must be an array of tables", resolver)
				}
				c.evidence[resolver] = append(c.evidence[resolver], m)
			}
		}
	}
	expected, ok := data["expected"].(map[string]any)
	if !ok {
		return c, fmt.Errorf("missing [expected] table")
	}
	if v, _ := expected["valid"].(bool); v {
		if _, has := expected["diagnostics"]; has {
			return c, fmt.Errorf("expected.valid = true must not also list diagnostics")
		}
		c.valid = true
		return c, nil
	}
	raw, ok := expected["diagnostics"].([]any)
	if !ok || len(raw) == 0 {
		return c, fmt.Errorf("expected outcome must be valid = true or a non-empty diagnostics list")
	}
	for _, r := range raw {
		d, err := parseExpectedDiag(r)
		if err != nil {
			return c, err
		}
		c.want = append(c.want, d)
	}
	return c, nil
}

// parseExpectedDiag reads one expected diagnostic and renders its message from
// the catalogue template and the case's pre-rendered slot values. A template's
// {path} slot defaults to the diagnostic's own path.
func parseExpectedDiag(raw any) (observedDiag, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return observedDiag{}, fmt.Errorf("each expected diagnostic must be a table")
	}
	code, _ := m["code"].(string)
	entry, ok := codes.Lookup(code)
	if !ok {
		return observedDiag{}, fmt.Errorf("code %q is not in the catalogue", code)
	}
	path, ok := m["path"].(string)
	if !ok {
		return observedDiag{}, fmt.Errorf("diagnostic %s: path must be a string", code)
	}
	slots := map[string]string{}
	if rawSlots, present := m["slots"]; present {
		table, ok := rawSlots.(map[string]any)
		if !ok {
			return observedDiag{}, fmt.Errorf("diagnostic %s: slots must be a table", code)
		}
		for k, v := range table {
			s, ok := v.(string)
			if !ok {
				return observedDiag{}, fmt.Errorf("diagnostic %s: slot %q must be a pre-rendered string", code, k)
			}
			slots[k] = s
		}
	}
	templateSlots := map[string]bool{}
	for _, match := range slotPattern.FindAllStringSubmatch(entry.Template, -1) {
		templateSlots[match[1]] = true
	}
	if templateSlots["path"] {
		if _, set := slots["path"]; !set {
			slots["path"] = path
		}
	}
	for k := range slots {
		if !templateSlots[k] {
			return observedDiag{}, fmt.Errorf("diagnostic %s: unknown slot %q", code, k)
		}
	}
	for k := range templateSlots {
		if _, set := slots[k]; !set && !optionalSlots[k] {
			return observedDiag{}, fmt.Errorf("diagnostic %s: missing slot %q", code, k)
		}
	}
	msg := slotPattern.ReplaceAllStringFunc(entry.Template, func(s string) string {
		return slots[s[1:len(s)-1]]
	})
	return observedDiag{Code: code, Path: path, Message: msg}, nil
}

func (c fixtureCase) input(t *testing.T) []byte {
	t.Helper()
	if c.inputPath == "" {
		return []byte(c.inputInline)
	}
	src, err := os.ReadFile(c.inputPath)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// isMetaSchema reports whether a case validates its input AS a schema file: the
// meta-schema reader's authoring diagnostics are then the outcome, the same for
// both ways of validating, since no validator is generated for schemas.
func isMetaSchema(t *testing.T, schemaPath string) bool {
	t.Helper()
	s, _, err := schema.LoadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	return s.Name == "strictspec-meta-schema"
}

func toObserved(diags []diag.Diagnostic) []observedDiag {
	out := []observedDiag{}
	for _, d := range diags {
		out = append(out, observedDiag{Code: d.Code, Path: d.Path.Render(), Message: render.Render(d)})
	}
	return out
}

func fixtureParseDiag(pe *doc.ParseError) diag.Diagnostic {
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	slots := map[string]diag.Slot{"detail": diag.SlotString{S: pe.Message}}
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
		slots["line"] = diag.SlotInt{N: int64(pe.Position.Line)}
	}
	return diag.Diagnostic{Code: code, Path: diag.NewPath(), Slots: slots}
}

// metaOutcome reads the case input as a schema file and returns the
// meta-schema reader's authoring diagnostics.
func metaOutcome(t *testing.T, c fixtureCase) []observedDiag {
	t.Helper()
	d, perr := tomldoc.Parse(c.input(t))
	if perr != nil {
		return toObserved([]diag.Diagnostic{fixtureParseDiag(perr)})
	}
	_, diags := schema.ReadSchema(d.Root, c.inputPath)
	return toObserved(diags)
}

// interpreterOutcome validates the case through the reference interpreter.
func interpreterOutcome(t *testing.T, c fixtureCase) []observedDiag {
	t.Helper()
	s, sdiags, err := schema.LoadFile(c.schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) > 0 {
		return toObserved(sdiags)
	}
	scalars, err := schema.LoadManifestScalars(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	src := c.input(t)
	switch c.syntax {
	case "jsonl":
		docs, perr := jsondoc.ParseLines(src)
		if perr != nil {
			return toObserved([]diag.Diagnostic{fixtureParseDiag(perr)})
		}
		starts := []int{0}
		for i, b := range src {
			if b == '\n' {
				starts = append(starts, i+1)
			}
		}
		var out []diag.Diagnostic
		for i, d := range docs {
			ls := 0
			if i < len(starts) {
				ls = starts[i]
			}
			out = append(out, interp.Validate(s, d.Root, interp.Options{
				Scalars: scalars, Format: doc.FormatJSONL, Evidence: c.evidence,
				JSONL: true, Line: i + 1, LineStart: ls,
			})...)
		}
		return toObserved(out)
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			return toObserved([]diag.Diagnostic{fixtureParseDiag(perr)})
		}
		return toObserved(interp.Validate(s, d.Root, interp.Options{Scalars: scalars, Format: doc.FormatTOML, Evidence: c.evidence}))
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			return toObserved([]diag.Diagnostic{fixtureParseDiag(perr)})
		}
		return toObserved(interp.Validate(s, d.Root, interp.Options{Scalars: scalars, Format: doc.FormatJSON, Evidence: c.evidence}))
	}
}

// multiRunnerMain is the main package of the module buildGeneratedValidators
// writes: it reads a JSON array of requests on stdin, validates each through the
// generated validator its schema key names, and writes the outcomes as a JSON
// array on stdout. %s is the import block, %s the dispatch table entries.
const multiRunnerMain = `package main

import (
	"encoding/json"
	"os"

	"github.com/stricttools/strictspec/go/strictspec"
%s)

type validateFunc func([]byte, string, map[string][]map[string]any) []strictspec.Diagnostic

var validators = map[string]validateFunc{
%s}

type request struct {
	Schema   string                      ` + "`json:\"schema\"`" + `
	Input    string                      ` + "`json:\"input\"`" + `
	Syntax   string                      ` + "`json:\"syntax\"`" + `
	Evidence map[string][]map[string]any ` + "`json:\"evidence\"`" + `
}

type observed struct {
	Code    string ` + "`json:\"code\"`" + `
	Path    string ` + "`json:\"path\"`" + `
	Message string ` + "`json:\"message\"`" + `
}

func main() {
	var reqs []request
	if err := json.NewDecoder(os.Stdin).Decode(&reqs); err != nil {
		os.Exit(2)
	}
	out := make([][]observed, len(reqs))
	for i, r := range reqs {
		v, ok := validators[r.Schema]
		if !ok {
			os.Exit(3)
		}
		out[i] = []observed{}
		for _, d := range v([]byte(r.Input), r.Syntax, r.Evidence) {
			out[i] = append(out[i], observed{Code: d.Code, Path: d.Path, Message: d.Message})
		}
	}
	json.NewEncoder(os.Stdout).Encode(out)
}
`

// buildGeneratedValidators generates one Go validator package per schema into a
// single module, with a runner dispatching on the schema path, and builds it
// once. It returns the runner binary.
func buildGeneratedValidators(t *testing.T, runtimeDir string, schemaPaths []string) string {
	t.Helper()
	modDir := t.TempDir()
	var imports, table strings.Builder
	for i, sp := range schemaPaths {
		loaded, err := LoadForEmit(sp)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Diags) > 0 {
			t.Fatalf("schema %s fails the meta-schema: %v", sp, toObserved(loaded.Diags))
		}
		pkg := fmt.Sprintf("s%02d", i)
		src, err := GenerateGo(loaded.Schema, loaded.Scalars, GoParams{
			Package:          pkg,
			MainFile:         loaded.MainFile,
			Files:            loaded.Files,
			GeneratorVersion: runtimeVersion,
			RegenCommand:     "strictspec gen",
		})
		if err != nil {
			t.Fatalf("generate %s: %v", sp, err)
		}
		if err := os.MkdirAll(filepath.Join(modDir, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modDir, pkg, "validator_gen.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&imports, "\t%s \"ssfixtures/%s\"\n", pkg, pkg)
		fmt.Fprintf(&table, "\t%q: func(in []byte, syntax string, ev map[string][]map[string]any) []strictspec.Diagnostic {\n\t\t_, d := %s.ValidateBytesWithEvidence(in, syntax, ev)\n\t\treturn d\n\t},\n", sp, pkg)
	}
	goMod := fmt.Sprintf("module ssfixtures\n\ngo 1.26\n\nrequire github.com/stricttools/strictspec/go v0.0.0\n\nreplace github.com/stricttools/strictspec/go => %s\n", runtimeDir)
	writes := map[string]string{
		"go.mod":  goMod,
		"main.go": fmt.Sprintf(multiRunnerMain, imports.String(), table.String()),
	}
	if sum, err := os.ReadFile(filepath.Join(runtimeDir, "go.sum")); err == nil {
		writes["go.sum"] = string(sum)
	}
	for name, body := range writes {
		if err := os.WriteFile(filepath.Join(modDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(modDir, "runner")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = modDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build of the generated validators failed: %v\n%s", err, out)
	}
	return bin
}

func TestValidationFixtures(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	runtimeDir, fixturesRoot := dirs(t)
	cases := loadFixtureCases(t, fixturesRoot)
	if len(cases) < 40 {
		t.Fatalf("expected the validation fixture cases, found %d", len(cases))
	}

	// Generated validators: one module for every schema a non-meta case names.
	var generatedCases []fixtureCase
	seen := map[string]bool{}
	var schemaPaths []string
	for _, c := range cases {
		if isMetaSchema(t, c.schemaPath) {
			continue
		}
		generatedCases = append(generatedCases, c)
		if !seen[c.schemaPath] {
			seen[c.schemaPath] = true
			schemaPaths = append(schemaPaths, c.schemaPath)
		}
	}
	bin := buildGeneratedValidators(t, runtimeDir, schemaPaths)
	type request struct {
		Schema   string                      `json:"schema"`
		Input    string                      `json:"input"`
		Syntax   string                      `json:"syntax"`
		Evidence map[string][]map[string]any `json:"evidence"`
	}
	reqs := make([]request, len(generatedCases))
	for i, c := range generatedCases {
		reqs[i] = request{Schema: c.schemaPath, Input: string(c.input(t)), Syntax: c.syntax, Evidence: c.evidence}
	}
	body, err := json.Marshal(reqs)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(string(body))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("generated validators runner failed: %v", err)
	}
	var generated [][]observedDiag
	if err := json.Unmarshal(out, &generated); err != nil {
		t.Fatalf("bad runner output: %v\n%s", err, out)
	}
	generatedByFile := map[string][]observedDiag{}
	for i, c := range generatedCases {
		generatedByFile[c.file] = generated[i]
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.want
			if want == nil {
				want = []observedDiag{}
			}
			if isMetaSchema(t, c.schemaPath) {
				assertOutcome(t, "meta-schema reader", metaOutcome(t, c), want)
				return
			}
			assertOutcome(t, "interpreter", interpreterOutcome(t, c), want)
			assertOutcome(t, "generated validator", generatedByFile[c.file], want)
		})
	}
}

func assertOutcome(t *testing.T, who string, got, want []observedDiag) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d diagnostics, want %d\n got: %+v\nwant: %+v", who, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: diag[%d]\n got: %+v\nwant: %+v", who, i, got[i], want[i])
		}
	}
}
