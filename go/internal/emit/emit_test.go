package emit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/interp"
	"github.com/stricttools/strictspec/go/internal/jsondoc"
	"github.com/stricttools/strictspec/go/internal/render"
	"github.com/stricttools/strictspec/go/internal/schema"
	"github.com/stricttools/strictspec/go/internal/tomldoc"
)

// dirs resolves the go/ runtime module root and the conformance fixtures root
// from the test file location.
func dirs(t *testing.T) (runtimeDir, fixturesRoot string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	runtimeDir = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	fixturesRoot = filepath.Clean(filepath.Join(runtimeDir, "..", "conformance", "fixtures"))
	return
}

func osReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

type observedDiag struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}
type runnerResp struct {
	Valid       bool           `json:"valid"`
	Diagnostics []observedDiag `json:"diagnostics"`
}

// TestGoldenCompileAndParity generates a validator for three representative
// schemas, COMPILES it against the runtime, runs it on sample documents, and
// asserts verdict + code + path + message PARITY against the reference
// interpreter. Because both sides drive the shared emitter IR, any divergence is
// a real emitter or runtime bug.
func TestGoldenCompileAndParity(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	runtimeDir, fixturesRoot := dirs(t)
	cacheDir := t.TempDir()

	cases := []struct {
		schema string
		input  string
		syntax string
	}{
		{"private-project-character-preview.toml", "private-project/character-preview.valid.json", "json"},
		{"private-project-character-preview.toml", "private-project/character-preview.invalid-union-and-order.json", "json"},
		{"wavescript-score.schema.toml", "wavescript/valid-01-embedded-bank.json", "json"},
		{"wavescript-score.schema.toml", "wavescript/invalid-01-gate-violations.json", "json"},
		{"shared-canvas.toml", "shared/canvas.valid.json", "json"},
		{"shared-canvas.toml", "shared/canvas.invalid.json", "json"},
	}

	for _, c := range cases {
		t.Run(c.schema+"/"+filepath.Base(c.input), func(t *testing.T) {
			schemaPath := filepath.Join(fixturesRoot, "_schemas", c.schema)
			inputPath := filepath.Join(fixturesRoot, "_inputs", c.input)

			built, err := Build(schemaPath, cacheDir, runtimeDir, runtimeVersion)
			if err != nil {
				t.Fatalf("Build (generate+compile) failed: %v", err)
			}

			got := runValidator(t, built.BinPath, inputPath, c.syntax)
			want := interpOutcome(t, schemaPath, inputPath, c.syntax)

			if len(got.Diagnostics) != len(want) {
				t.Fatalf("parity break: generated %d diagnostics, interpreter %d\n gen: %v\n int: %v",
					len(got.Diagnostics), len(want), got.Diagnostics, want)
			}
			for i := range want {
				if got.Diagnostics[i].Code != want[i].Code ||
					got.Diagnostics[i].Path != want[i].Path ||
					got.Diagnostics[i].Message != want[i].Message {
					t.Errorf("parity break at diag[%d]:\n gen: %+v\n int: %+v", i, got.Diagnostics[i], want[i])
				}
			}
		})
	}
}

func runValidator(t *testing.T, bin, inputPath, syntax string) runnerResp {
	t.Helper()
	src, err := osReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(map[string]any{"input": string(src), "syntax": syntax})
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(string(req))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("runner failed: %v", err)
	}
	var resp runnerResp
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad runner output: %v\n%s", err, out)
	}
	return resp
}

func interpOutcome(t *testing.T, schemaPath, inputPath, syntax string) []observedDiag {
	t.Helper()
	s, sdiags, err := schema.LoadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) != 0 {
		t.Fatalf("schema authoring diagnostics: %v", sdiags)
	}
	scalars := schema.LoadManifestScalars(s.Dir)
	src, err := osReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	var diags []observedDiag
	switch syntax {
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, dg := range interp.Validate(s, d.Root, interp.Options{Scalars: scalars, Format: doc.FormatTOML}) {
			diags = append(diags, observedDiag{Code: dg.Code, Path: dg.Path.Render(), Message: render.Render(dg)})
		}
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, dg := range interp.Validate(s, d.Root, interp.Options{Scalars: scalars, Format: doc.FormatJSON}) {
			diags = append(diags, observedDiag{Code: dg.Code, Path: dg.Path.Render(), Message: render.Render(dg)})
		}
	}
	return diags
}

// unionRootSchema is a schema whose root is a discriminated union rather than a
// record, so its entry points return strictspec.Value instead of a pointer.
const unionRootSchema = `name = "union-root"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Event"
targets = ["go"]
description = "A document whose root is a discriminated union."

[types.Started]
type = "record"

[types.Started.fields.kind]
type = "literal"
value = "started"
required = true

[types.Started.fields.at]
type = "string"
required = true

[types.Ended]
type = "record"

[types.Ended.fields.kind]
type = "literal"
value = "ended"
required = true

[types.Ended.fields.outcome]
type = "enum"
required = true
values = ["exited", "crashed"]

[types.Event]
type = "discriminated-union"
discriminator = "kind"

[types.Event.arms.started]
type = "Started"

[types.Event.arms.ended]
type = "Ended"
`

// TestUnionRootCompiles generates the validator of a schema whose root is a
// discriminated union, compiles it against the runtime, and checks its verdicts
// against the reference interpreter for a valid and an invalid document.
func TestUnionRootCompiles(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	runtimeDir, _ := dirs(t)
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "union-root.schema.toml")
	if err := os.WriteFile(schemaPath, []byte(unionRootSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := Build(schemaPath, t.TempDir(), runtimeDir, runtimeVersion)
	if err != nil {
		t.Fatalf("Build (generate+compile) failed: %v", err)
	}
	inputs := map[string]string{
		"valid.json":   `{"kind": "ended", "outcome": "exited"}`,
		"invalid.json": `{"kind": "ended", "outcome": "vanished"}`,
	}
	for name, body := range inputs {
		inputPath := filepath.Join(dir, name)
		if err := os.WriteFile(inputPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got := runValidator(t, built.BinPath, inputPath, "json")
		want := interpOutcome(t, schemaPath, inputPath, "json")
		if got.Valid != (len(want) == 0) || len(got.Diagnostics) != len(want) {
			t.Fatalf("%s: generated valid=%v %v, interpreter %v", name, got.Valid, got.Diagnostics, want)
		}
		for i := range want {
			if got.Diagnostics[i] != want[i] {
				t.Errorf("%s: parity break at diag[%d]:\n gen: %+v\n int: %+v", name, i, got.Diagnostics[i], want[i])
			}
		}
	}
}
