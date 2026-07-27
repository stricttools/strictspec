package emit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
)

// BuiltValidator is a compiled generated validator for one schema.
type BuiltValidator struct {
	// BinPath is a runner binary reading a JSON request {input, syntax, evidence}
	// on stdin and writing the outcome {valid, diagnostics:[{code,path,message}]}
	// on stdout — the same shape the conformance harness consumes.
	BinPath string
	// Package is the generated Go source (for golden inspection).
	Source string
}

// Build generates the Go validator for schemaPath, writes it into a
// self-contained temp module under cacheDir (keyed by a hash of the schema's
// embedded file set), and go-builds a runner binary. Results are CACHED per
// schema: an identical file set reuses the built binary. runtimeDir is the
// absolute path to the go/ runtime module (used by the temp module's replace
// directive). This is the machinery behind the conformance `go` target and the
// emitter golden tests: prove the emitted code compiles against the runtime and
// runs.
func Build(schemaPath, cacheDir, runtimeDir, generatorVersion string) (*BuiltValidator, error) {
	loaded, err := LoadForEmit(schemaPath)
	if err != nil {
		return nil, err
	}
	if len(loaded.Diags) > 0 {
		return nil, fmt.Errorf("schema %s fails the meta-schema (not emittable): %s",
			schemaPath, firstCodes(loaded.Diags))
	}

	src, err := GenerateGo(loaded.Schema, loaded.Scalars, GoParams{
		Package:          "gen",
		MainFile:         loaded.MainFile,
		Files:            loaded.Files,
		GeneratorVersion: generatorVersion,
		RegenCommand:     "strictspec gen",
	})
	if err != nil {
		return nil, err
	}

	key := hashFiles(loaded.Files)
	modDir := filepath.Join(cacheDir, key)
	binPath := filepath.Join(modDir, "runner")
	if fileExists(binPath) {
		return &BuiltValidator{BinPath: binPath, Source: src}, nil
	}

	if err := writeModule(modDir, runtimeDir, src); err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = modDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, berr := cmd.CombinedOutput(); berr != nil {
		return nil, fmt.Errorf("go build of generated validator failed: %v\n%s", berr, out)
	}
	return &BuiltValidator{BinPath: binPath, Source: src}, nil
}

func writeModule(modDir, runtimeDir, genSrc string) error {
	if err := os.MkdirAll(filepath.Join(modDir, "gen"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(modDir, "gen", "validator_gen.go"), []byte(genSrc), 0o644); err != nil {
		return err
	}
	goMod := fmt.Sprintf(`module ssgen

go 1.26

require github.com/smm-h/strictspec/go v0.0.0

replace github.com/smm-h/strictspec/go => %s
`, runtimeDir)
	if err := os.WriteFile(filepath.Join(modDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}
	// The temp module's transitive deps (via the replace target) equal the
	// runtime's; copying its go.sum satisfies verification with no network.
	if sum, err := os.ReadFile(filepath.Join(runtimeDir, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(modDir, "go.sum"), sum, 0o644)
	}
	if err := os.WriteFile(filepath.Join(modDir, "main.go"), []byte(runnerMain), 0o644); err != nil {
		return err
	}
	return nil
}

const runnerMain = `package main

import (
	"encoding/json"
	"os"

	gen "ssgen/gen"
)

type request struct {
	Input    string                        ` + "`json:\"input\"`" + `
	Syntax   string                        ` + "`json:\"syntax\"`" + `
	Evidence map[string][]map[string]any   ` + "`json:\"evidence\"`" + `
}

type observed struct {
	Code    string ` + "`json:\"code\"`" + `
	Path    string ` + "`json:\"path\"`" + `
	Message string ` + "`json:\"message\"`" + `
}

type response struct {
	Valid       bool       ` + "`json:\"valid\"`" + `
	Diagnostics []observed ` + "`json:\"diagnostics\"`" + `
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		os.Exit(2)
	}
	_, diags := gen.ValidateBytesWithEvidence([]byte(req.Input), req.Syntax, req.Evidence)
	resp := response{Valid: len(diags) == 0, Diagnostics: []observed{}}
	for _, d := range diags {
		resp.Diagnostics = append(resp.Diagnostics, observed{Code: d.Code, Path: d.Path, Message: d.Message})
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
`

func hashFiles(files map[string]string) string {
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write([]byte(files[n]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstCodes(diags []diag.Diagnostic) string {
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return strings.Join(codes, ", ")
}
