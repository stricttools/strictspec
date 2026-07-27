package render

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
)

// TestHarnessParity cross-checks the Go renderer against the conformance
// harness's Python renderer (conformance/harness/templates.py) for identical
// inputs. For each case we hand the harness the SAME pre-rendered slot values
// (and path) that the Go renderer produces per slot, then assert byte-identity
// of the full message. Any difference means one side's template/assembly is
// wrong. This validates template identity + slot substitution + {path}
// injection + {suggestion}/{condition} assembly across the two implementations;
// per-slot value rendering (A.1) is validated separately by the spec-derived
// golden tests.
//
// The test is hermetic-with-skip: it locates the conformance project relative to
// this source file and runs the harness via `uv`. If `uv` or the conformance
// project is unavailable, it skips (so `go test ./...` stays green in minimal
// environments) rather than failing spuriously.
func TestHarnessParity(t *testing.T) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		t.Skip("uv not found in PATH; skipping cross-target harness parity check")
	}
	conformanceDir := locateConformanceDir(t)

	type parityCase struct {
		diag         diag.Diagnostic
		harnessSlots map[string]string // pre-rendered slot values fed to the harness
	}
	cases := []parityCase{
		{
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_TYPE_NOT_INTEGER",
				Path:  diag.NewPath(diag.Key{Name: "count"}),
				Slots: slots("got", diag.SlotString{S: "float"}),
			},
			harnessSlots: map[string]string{"path": "$.count", "got": `"float"`},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_KEY_UNKNOWN",
				Path: diag.NewPath(diag.Key{Name: "config"}),
				Slots: slots(
					"key", diag.SlotString{S: "colr"},
					"suggestion", diag.SlotSuggestion{Unknown: "colr", Candidates: []string{"color", "width", "height"}},
				),
			},
			harnessSlots: map[string]string{"path": "$.config", "key": `"colr"`, "suggestion": " Did you mean color?"},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_VALUE_STRING_REGEX",
				Path: diag.NewPath(diag.Key{Name: "slug"}),
				Slots: slots(
					"actual", diag.SlotValue{V: diag.StringVal("Hello World")},
					"pattern", diag.SlotString{S: `^[a-z-]+$`},
				),
			},
			harnessSlots: map[string]string{"path": "$.slug", "actual": `"Hello World"`, "pattern": `"^[a-z-]+$"`},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
				Path: diag.NewPath(diag.Key{Name: "color"}),
				Slots: slots(
					"got", diag.SlotValue{V: diag.StringVal("gren")},
					"expected", diag.SlotList{Elems: []diag.Value{
						diag.StringVal("red"), diag.StringVal("green"), diag.StringVal("blue"), diag.StringVal("cyan"),
					}},
					"suggestion", diag.SlotSuggestion{Unknown: "gren", Candidates: []string{"red", "green", "blue", "cyan"}},
				),
			},
			harnessSlots: map[string]string{
				"path":       "$.color",
				"got":        `"gren"`,
				"expected":   `["red", "green", "blue", ...]`,
				"suggestion": " Did you mean green or red?",
			},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_GATE_UNSUPPORTED",
				Path: diag.NewPath(),
				Slots: slots(
					"got", diag.SlotVersion{V: 2},
					"schema", diag.SlotIdentifier{Name: "canvas"},
					"expected", diag.SlotVersion{V: 3},
					"migset", diag.SlotIdentifier{Name: "canvas_v2_v3"},
					"invocation", diag.SlotString{S: "strictspec migrate --schema canvas --to 3 doc.json"},
				),
			},
			harnessSlots: map[string]string{
				"got": "2", "schema": "canvas", "expected": "3", "migset": "canvas_v2_v3",
				"invocation": `"strictspec migrate --schema canvas --to 3 doc.json"`,
			},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_FORBIDDEN_WHEN",
				Path: diag.NewPath(diag.Key{Name: "legacy"}),
				Slots: slots(
					"key", diag.SlotString{S: "legacy"},
					"condition", diag.SlotString{S: `mode == "strict"`},
				),
			},
			harnessSlots: map[string]string{"path": "$.legacy", "key": `"legacy"`, "condition": `mode == "strict"`},
		},
		{
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_NUM_SAFE_INTEGER",
				Path:  diag.NewPath(diag.Key{Name: "id"}),
				Slots: slots("actual", diag.SlotValue{V: diag.IntVal(9007199254740993)}),
			},
			harnessSlots: map[string]string{"path": "$.id", "actual": "9007199254740993"},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_EXACTLY_ONE_OF",
				Path: diag.NewPath(diag.Key{Name: "payment"}),
				Slots: slots(
					"fields", diag.SlotList{Elems: []diag.Value{diag.StringVal("card"), diag.StringVal("bank")}},
					"actual", diag.SlotList{Elems: []diag.Value{diag.StringVal("card"), diag.StringVal("bank")}},
				),
			},
			harnessSlots: map[string]string{"path": "$.payment", "fields": `["card", "bank"]`, "actual": `["card", "bank"]`},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX",
				Path: diag.NewPath().WithAnchor(3, 12),
				Slots: slots(
					"line", diag.SlotInt{N: 3},
					"detail", diag.SlotString{S: "unexpected end of input"},
				),
			},
			harnessSlots: map[string]string{"path": "$@L3:12", "line": "3", "detail": `"unexpected end of input"`},
		},
		{
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_ALIAS_BOTH_PRESENT",
				Path: diag.NewPath(diag.Key{Name: "node"}),
				Slots: slots(
					"alias", diag.SlotIdentifier{Name: "colour"},
					"canonical", diag.SlotIdentifier{Name: "color"},
				),
			},
			harnessSlots: map[string]string{"path": "$.node", "alias": "colour", "canonical": "color"},
		},
	}

	// Build the payload for the harness.
	type req struct {
		Code  string            `json:"code"`
		Slots map[string]string `json:"slots"`
	}
	payload := make([]req, len(cases))
	for i, c := range cases {
		payload[i] = req{Code: c.diag.Code, Slots: c.harnessSlots}
	}
	in, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	const script = `import sys, json
from harness import templates
data = json.load(sys.stdin)
print(json.dumps([templates.render(x["code"], x["slots"]) for x in data]))`

	cmd := exec.Command(uv, "run", "python", "-c", script)
	cmd.Dir = conformanceDir
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run the conformance harness via uv (%v); stderr:\n%s", err, stderr.String())
	}

	var harnessMsgs []string
	if err := json.Unmarshal(stdout.Bytes(), &harnessMsgs); err != nil {
		t.Fatalf("decoding harness output %q: %v", stdout.String(), err)
	}
	if len(harnessMsgs) != len(cases) {
		t.Fatalf("harness returned %d messages, want %d", len(harnessMsgs), len(cases))
	}

	for i, c := range cases {
		goMsg := Render(c.diag)
		pyMsg := harnessMsgs[i]
		if goMsg != pyMsg {
			t.Errorf("byte mismatch for %s:\n  go: %q\n  py: %q", c.diag.Code, goMsg, pyMsg)
		}
	}
}

// locateConformanceDir finds the sibling conformance/ project relative to this
// test's source file.
func locateConformanceDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot determine source location")
	}
	// this file: <repo>/go/internal/render/harness_parity_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	dir := filepath.Join(repoRoot, "conformance")
	fi, err := os.Stat(filepath.Join(dir, "harness", "templates.py"))
	if err != nil || fi.IsDir() {
		t.Skipf("conformance harness not found at %s; skipping harness parity", dir)
	}
	return dir
}
