package emit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	strictspecroot "github.com/stricttools/strictspec/go"
)

// runtimeVersion is the strictspec release version embedded from go/VERSION
// (== strictspecroot.Version). The exec-parity tests generate code at THIS
// version so the generated file records the release that is actually installed;
// the pairing guard itself no longer looks at it (it pairs on the generated-code
// format), which is what TestGeneratedCodeOutlivesTheGeneratingRelease proves.
// The static hygiene/determinism tests below keep "0.0.0" because they only
// assert on emitted text and never execute the generated module.
var runtimeVersion = strictspecroot.Version

// The Python and TypeScript EMITTERS are gated the same way the Go emitter is:
// generate the validator for representative schemas, EXECUTE the generated code
// (uv / node) over the sample documents, and assert the ordered (code, path,
// message) output is byte-identical to the reference interpreter's on the same
// inputs. Because every target drives the shared emitter IR, any divergence is a
// real emitter or runtime bug.

// otherDirs resolves the python/ and ts/ runtime module roots (siblings of go/).
func otherDirs(t *testing.T) (pythonDir, tsDir string) {
	t.Helper()
	runtimeDir, _ := dirs(t)
	repo := filepath.Dir(runtimeDir)
	return filepath.Join(repo, "python"), filepath.Join(repo, "ts")
}

// parityCases are the three representative schemas with a valid + an invalid
// sample document each — the same corpus the Go golden test uses.
var parityCases = []struct {
	schema string
	input  string
	syntax string
}{
	{"pixelweaver-character-preview.toml", "pixelweaver/character-preview.valid.json", "json"},
	{"pixelweaver-character-preview.toml", "pixelweaver/character-preview.invalid-union-and-order.json", "json"},
	{"wavescript-score.schema.toml", "wavescript/valid-01-embedded-bank.json", "json"},
	{"wavescript-score.schema.toml", "wavescript/invalid-01-gate-violations.json", "json"},
	{"shared-canvas.toml", "shared/canvas.valid.json", "json"},
	{"shared-canvas.toml", "shared/canvas.invalid.json", "json"},
}

func assertParity(t *testing.T, got runnerResp, want []observedDiag) {
	t.Helper()
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
}

// --- Python -------------------------------------------------------------------

const pyDriver = `import importlib.util, json, sys

mod_path, input_path, syntax = sys.argv[1], sys.argv[2], sys.argv[3]
spec = importlib.util.spec_from_file_location("gen_module", mod_path)
m = importlib.util.module_from_spec(spec)
sys.modules["gen_module"] = m  # dataclass decorator resolves cls.__module__ here
spec.loader.exec_module(m)  # runs the pairing guard + compile-at-import
with open(input_path, "rb") as f:
    data = f.read()
_root, diags = m.validate_bytes_with_evidence(data, syntax, None)
out = {
    "valid": len(diags) == 0,
    "diagnostics": [{"code": d.code, "path": d.path, "message": d.message} for d in diags],
}
json.dump(out, sys.stdout)
`

func genPythonSource(t *testing.T, schemaPath, version string) string {
	t.Helper()
	loaded, err := LoadForEmit(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Diags) > 0 {
		t.Fatalf("schema authoring diagnostics: %v", loaded.Diags)
	}
	src, err := GeneratePython(loaded.Schema, PyParams{
		MainFile:         loaded.MainFile,
		Files:            loaded.Files,
		GeneratorVersion: version,
		RegenCommand:     "strictspec gen",
	})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// runPython writes the generated module + a driver into a temp dir and executes
// it through the python/ project's uv environment. Returns stdout-parsed outcome,
// combined stderr, and any exec error.
func runPython(t *testing.T, pythonDir, src, inputPath, syntax string) (runnerResp, string, error) {
	t.Helper()
	dir := t.TempDir()
	modPath := filepath.Join(dir, "gen_module.py")
	if err := os.WriteFile(modPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(dir, "driver.py")
	if err := os.WriteFile(driverPath, []byte(pyDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "python", driverPath, modPath, inputPath, syntax)
	cmd.Dir = pythonDir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return runnerResp{}, stderr.String(), err
	}
	var resp runnerResp
	if jerr := json.Unmarshal([]byte(stdout.String()), &resp); jerr != nil {
		t.Fatalf("bad python driver output: %v\nstdout: %s\nstderr: %s", jerr, stdout.String(), stderr.String())
	}
	return resp, stderr.String(), nil
}

func TestPythonGoldenExecParity(t *testing.T) {
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not available")
	}
	_, fixturesRoot := dirs(t)
	pythonDir, _ := otherDirs(t)

	for _, c := range parityCases {
		t.Run(c.schema+"/"+filepath.Base(c.input), func(t *testing.T) {
			schemaPath := filepath.Join(fixturesRoot, "_schemas", c.schema)
			inputPath := filepath.Join(fixturesRoot, "_inputs", c.input)
			src := genPythonSource(t, schemaPath, runtimeVersion)
			got, stderr, err := runPython(t, pythonDir, src, inputPath, c.syntax)
			if err != nil {
				t.Fatalf("generated python failed to run: %v\n%s", err, stderr)
			}
			want := interpOutcome(t, schemaPath, inputPath, c.syntax)
			assertParity(t, got, want)
		})
	}
}

// --- TypeScript ---------------------------------------------------------------

var tsBuildOnce sync.Once
var tsBuildErr error

// ensureTSBuild builds ts/dist once so generated code's `import "strictspec"`
// self-reference resolves to the compiled runtime.
func ensureTSBuild(t *testing.T, tsDir string) {
	t.Helper()
	tsBuildOnce.Do(func() {
		if _, err := os.Stat(filepath.Join(tsDir, "dist", "index.js")); err == nil {
			return
		}
		cmd := exec.Command("npm", "run", "build")
		cmd.Dir = tsDir
		if out, err := cmd.CombinedOutput(); err != nil {
			tsBuildErr = errWith("npm run build failed", string(out), err)
		}
	})
	if tsBuildErr != nil {
		t.Fatal(tsBuildErr)
	}
}

const tsDriver = `import { readFileSync } from "node:fs";
import { validateBytesWithEvidence } from "./gen.generated.js";

const inputPath = process.argv[2];
const syntax = process.argv[3];
const rawText = readFileSync(inputPath, "utf-8");
const [, diags] = validateBytesWithEvidence(rawText, syntax as any, null);
const out = {
	valid: diags.length === 0,
	diagnostics: diags.map((d) => ({ code: d.code, path: d.path, message: d.message })),
};
process.stdout.write(JSON.stringify(out));
`

const tsLocalTsconfig = `{
	"compilerOptions": {
		"module": "nodenext",
		"moduleResolution": "nodenext",
		"target": "es2023",
		"skipLibCheck": true,
		"strict": false,
		"types": ["node"],
		"outDir": "out"
	},
	"include": ["gen.generated.ts", "driver.ts"]
}`

func genTSSource(t *testing.T, schemaPath, version string) string {
	t.Helper()
	loaded, err := LoadForEmit(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Diags) > 0 {
		t.Fatalf("schema authoring diagnostics: %v", loaded.Diags)
	}
	src, err := GenerateTypeScript(loaded.Schema, TSParams{
		MainFile:         loaded.MainFile,
		Files:            loaded.Files,
		GeneratorVersion: version,
		RegenCommand:     "strictspec gen",
	})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// tsGenDir returns a unique gen dir INSIDE ts/ (so the "strictspec" self-reference
// resolves) and registers cleanup.
func tsGenDir(t *testing.T, tsDir string) string {
	t.Helper()
	base := filepath.Join(tsDir, ".gen-test")
	dir, err := os.MkdirTemp(base, "case-")
	if err != nil {
		if mkErr := os.MkdirAll(base, 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		dir, err = os.MkdirTemp(base, "case-")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// compileAndRunTS compiles the generated module + driver with tsc and runs the
// result under node. Returns outcome, combined stderr, and exec error.
func compileAndRunTS(t *testing.T, tsDir, genDir, src, inputPath, syntax string) (runnerResp, string, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(genDir, "gen.generated.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "driver.ts"), []byte(tsDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "tsconfig.json"), []byte(tsLocalTsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	tsc := filepath.Join(tsDir, "node_modules", ".bin", "tsc")
	compile := exec.Command(tsc, "-p", "tsconfig.json")
	compile.Dir = genDir
	if out, err := compile.CombinedOutput(); err != nil {
		return runnerResp{}, string(out), errWith("tsc compile failed", string(out), err)
	}
	run := exec.Command("node", filepath.Join("out", "driver.js"), inputPath, syntax)
	run.Dir = genDir
	var stdout, stderr strings.Builder
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		return runnerResp{}, stderr.String(), err
	}
	var resp runnerResp
	if jerr := json.Unmarshal([]byte(stdout.String()), &resp); jerr != nil {
		t.Fatalf("bad ts driver output: %v\nstdout: %s\nstderr: %s", jerr, stdout.String(), stderr.String())
	}
	return resp, stderr.String(), nil
}

func TestTSGoldenExecParity(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	_, fixturesRoot := dirs(t)
	_, tsDir := otherDirs(t)
	if _, err := os.Stat(filepath.Join(tsDir, "node_modules", ".bin", "tsc")); err != nil {
		t.Skip("ts node_modules not installed")
	}
	ensureTSBuild(t, tsDir)

	for _, c := range parityCases {
		t.Run(c.schema+"/"+filepath.Base(c.input), func(t *testing.T) {
			schemaPath := filepath.Join(fixturesRoot, "_schemas", c.schema)
			inputPath := filepath.Join(fixturesRoot, "_inputs", c.input)
			src := genTSSource(t, schemaPath, runtimeVersion)
			genDir := tsGenDir(t, tsDir)
			got, stderr, err := compileAndRunTS(t, tsDir, genDir, src, inputPath, c.syntax)
			if err != nil {
				t.Fatalf("generated ts failed to run: %v\n%s", err, stderr)
			}
			want := interpOutcome(t, schemaPath, inputPath, c.syntax)
			assertParity(t, got, want)
		})
	}
}

// --- pairing guard ------------------------------------------------------------

// TestGeneratedCodeOutlivesTheGeneratingRelease is the regression test for the
// outage this pairing rule exists to prevent: a validator generated by a
// DIFFERENT release than the installed runtime must still import and validate,
// because pairing is on the generated-code format, not on the release string.
// Before this rule, every strictspec release broke every already-published
// consumer at import until it regenerated and re-released.
func TestGeneratedCodeOutlivesTheGeneratingRelease(t *testing.T) {
	_, fixturesRoot := dirs(t)
	pythonDir, tsDir := otherDirs(t)
	schemaPath := filepath.Join(fixturesRoot, "_schemas", "shared-canvas.toml")
	inputPath := filepath.Join(fixturesRoot, "_inputs", "shared/canvas.valid.json")
	const olderRelease = "0.0.1-some-older-release"

	t.Run("python", func(t *testing.T) {
		if _, err := exec.LookPath("uv"); err != nil {
			t.Skip("uv not available")
		}
		src := genPythonSource(t, schemaPath, olderRelease)
		got, stderr, err := runPython(t, pythonDir, src, inputPath, "json")
		if err != nil {
			t.Fatalf("generated python from another release must still run: %v\n%s", err, stderr)
		}
		if !got.Valid {
			t.Errorf("expected a valid verdict, got %+v", got.Diagnostics)
		}
	})

	t.Run("ts", func(t *testing.T) {
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("node not available")
		}
		if _, err := os.Stat(filepath.Join(tsDir, "node_modules", ".bin", "tsc")); err != nil {
			t.Skip("ts node_modules not installed")
		}
		ensureTSBuild(t, tsDir)
		src := genTSSource(t, schemaPath, olderRelease)
		genDir := tsGenDir(t, tsDir)
		got, stderr, err := compileAndRunTS(t, tsDir, genDir, src, inputPath, "json")
		if err != nil {
			t.Fatalf("generated ts from another release must still run: %v\n%s", err, stderr)
		}
		if !got.Valid {
			t.Errorf("expected a valid verdict, got %+v", got.Diagnostics)
		}
	})
}

// TestPairingGuardFailure asserts that generated code declaring a format the
// runtime does not read hard-errors at import/init in BOTH targets — the pairing
// guard is the intended surfacing of a real generated-code shape change. The
// emitted constant is rewritten to an unread format, which is exactly what a
// file generated by a future format bump would carry.
func TestPairingGuardFailure(t *testing.T) {
	_, fixturesRoot := dirs(t)
	pythonDir, tsDir := otherDirs(t)
	schemaPath := filepath.Join(fixturesRoot, "_schemas", "shared-canvas.toml")
	inputPath := filepath.Join(fixturesRoot, "_inputs", "shared/canvas.valid.json")

	// An unread format: one past what the paired runtime accepts.
	fromFormat := fmt.Sprintf("GENERATED_CODE_FORMAT = %d", GeneratedCodeFormat)
	toFormat := fmt.Sprintf("GENERATED_CODE_FORMAT = %d", GeneratedCodeFormat+1)
	retarget := func(t *testing.T, src string) string {
		t.Helper()
		if !strings.Contains(src, fromFormat) {
			t.Fatalf("generated source does not declare %q", fromFormat)
		}
		return strings.Replace(src, fromFormat, toFormat, 1)
	}

	t.Run("python", func(t *testing.T) {
		if _, err := exec.LookPath("uv"); err != nil {
			t.Skip("uv not available")
		}
		src := retarget(t, genPythonSource(t, schemaPath, runtimeVersion))
		_, stderr, err := runPython(t, pythonDir, src, inputPath, "json")
		if err == nil {
			t.Fatal("expected a pairing hard error at import, got success")
		}
		if !strings.Contains(stderr, "strictspec generated-code format mismatch") {
			t.Errorf("stderr does not mention the pairing mismatch:\n%s", stderr)
		}
	})

	t.Run("ts", func(t *testing.T) {
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("node not available")
		}
		if _, err := os.Stat(filepath.Join(tsDir, "node_modules", ".bin", "tsc")); err != nil {
			t.Skip("ts node_modules not installed")
		}
		ensureTSBuild(t, tsDir)
		src := retarget(t, genTSSource(t, schemaPath, runtimeVersion))
		genDir := tsGenDir(t, tsDir)
		_, stderr, err := compileAndRunTS(t, tsDir, genDir, src, inputPath, "json")
		if err == nil {
			t.Fatal("expected a pairing hard error at init, got success")
		}
		if !strings.Contains(stderr, "strictspec generated-code format mismatch") {
			t.Errorf("stderr does not mention the pairing mismatch:\n%s", stderr)
		}
	})
}

// --- header / hygiene ---------------------------------------------------------

// TestGeneratedHeaderHygiene asserts the pinned generated-file header and the
// target ecosystem's lint-suppression/formatter markers are present in the
// emitted source (.stricttools/docs/DESIGN.md — Generated-file hygiene).
func TestGeneratedHeaderHygiene(t *testing.T) {
	_, fixturesRoot := dirs(t)
	schemaPath := filepath.Join(fixturesRoot, "_schemas", "shared-canvas.toml")

	py := genPythonSource(t, schemaPath, "0.0.0")
	for _, want := range []string{
		"# strictspec generated validator. DO NOT EDIT.",
		"# strictspec generator: 0.0.0",
		"canvas (format_version 1)",
		"# regenerate:          strictspec gen",
		"# ruff: noqa",
		"GENERATED_BY = \"0.0.0\"",
		fmt.Sprintf("GENERATED_CODE_FORMAT = %d", GeneratedCodeFormat),
		"strictspec.require_generated_code_format(GENERATED_CODE_FORMAT, GENERATED_BY)",
		"strictspec.compile_embedded(_EMBEDDED_SCHEMA, _EMBEDDED_MAIN_FILE)",
	} {
		if !strings.Contains(py, want) {
			t.Errorf("python generated source missing %q", want)
		}
	}

	ts := genTSSource(t, schemaPath, "0.0.0")
	for _, want := range []string{
		"// strictspec generated validator. DO NOT EDIT.",
		"// strictspec generator: 0.0.0",
		"canvas (format_version 1)",
		"// regenerate:          strictspec gen",
		"/* eslint-disable */",
		"// prettier-ignore",
		"biome-ignore",
		"export const GENERATED_BY = \"0.0.0\"",
		fmt.Sprintf("export const GENERATED_CODE_FORMAT = %d;", GeneratedCodeFormat),
		"requireGeneratedCodeFormat(GENERATED_CODE_FORMAT, GENERATED_BY)",
		"compileFromSource(EMBEDDED_SCHEMA, EMBEDDED_MAIN_FILE)",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("ts generated source missing %q", want)
		}
	}
}

// TestGeneratedDeterminism asserts the emitters are deterministic (byte-identical
// on regeneration), which the `check` drift gate relies on.
func TestGeneratedDeterminism(t *testing.T) {
	_, fixturesRoot := dirs(t)
	for _, s := range []string{"pixelweaver-character-preview.toml", "shared-canvas.toml"} {
		schemaPath := filepath.Join(fixturesRoot, "_schemas", s)
		if a, b := genPythonSource(t, schemaPath, "0.0.0"), genPythonSource(t, schemaPath, "0.0.0"); a != b {
			t.Errorf("python emitter non-deterministic for %s", s)
		}
		if a, b := genTSSource(t, schemaPath, "0.0.0"), genTSSource(t, schemaPath, "0.0.0"); a != b {
			t.Errorf("ts emitter non-deterministic for %s", s)
		}
	}
}

// snippetAround returns up to a few lines of s starting at the first line that
// contains marker, for readable failure messages.
func snippetAround(s, marker string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.Contains(ln, marker) {
			hi := i + 10
			if hi > len(lines) {
				hi = len(lines)
			}
			return strings.Join(lines[i:hi], "\n")
		}
	}
	return "(marker " + marker + " not found)"
}

// TestOptionalRecordFieldTypeText pins the typed-binding fix for optional
// (required=false) record-typed fields. shared-canvas's Canvas.origin is a
// `required = false` field of the imported record type Point: when the field is
// absent the binder yields None (Python) / an absent property (TS), so the
// emitted TYPE must admit absence. Go already emits a pointer (*Point, nil when
// absent, since record refs are always pointers); this guards the Python and TS
// emitters against the same over-promise. Required scalar fields keep the
// zero-value convention (Go parity) and must NOT be widened.
func TestOptionalRecordFieldTypeText(t *testing.T) {
	_, fixturesRoot := dirs(t)
	schemaPath := filepath.Join(fixturesRoot, "_schemas", "shared-canvas.toml")

	py := genPythonSource(t, schemaPath, "0.0.0")
	if !strings.Contains(py, "origin: Point | None = None") {
		t.Errorf("python: optional record field must be nullable with a None default (`origin: Point | None = None`):\n%s", snippetAround(py, "class Canvas"))
	}
	if !strings.Contains(py, "@dataclass(frozen=True, kw_only=True)") {
		t.Errorf("python: records must be keyword-only so an optional field's None default never breaks required-field ordering")
	}
	if strings.Contains(py, "background: str | None") {
		t.Errorf("python: required scalar field must not be widened to | None (zero-value convention, Go parity)")
	}

	ts := genTSSource(t, schemaPath, "0.0.0")
	if !strings.Contains(ts, "readonly origin?: Point;") {
		t.Errorf("ts: optional record field must be an optional property (`readonly origin?: Point;`):\n%s", snippetAround(ts, "interface Canvas"))
	}
	if !strings.Contains(ts, "readonly background: string;") {
		t.Errorf("ts: required field must stay a non-optional property (`readonly background: string;`):\n%s", snippetAround(ts, "interface Canvas"))
	}
}

func errWith(msg, out string, err error) error {
	return &execError{msg: msg, out: out, err: err}
}

type execError struct {
	msg string
	out string
	err error
}

func (e *execError) Error() string {
	return e.msg + ": " + e.err.Error() + "\n" + e.out
}
