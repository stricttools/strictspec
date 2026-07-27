package strictspec

import (
	"os"
	"strings"
	"testing"
)

const miniSchema = `
name = "point"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Point"

[types.Point]
type = "record"
[types.Point.fields.x]
type = "integer"
required = true
[types.Point.fields.label]
type = "string"
required = true
non_empty = true
`

func compile(t *testing.T) *Program {
	t.Helper()
	p, err := CompileEmbedded(map[string]string{"point.schema.toml": miniSchema}, "point.schema.toml")
	if err != nil {
		t.Fatalf("CompileEmbedded: %v", err)
	}
	return p
}

// TestVersionMatchesFile is the drift-impossibility gate: the runtime pairing
// constant MUST equal the go/VERSION file that rlsbl's Go release target bumps
// during `rlsbl release run`. Because Version is embedded from that file (see
// go/version.go), the two can never diverge — this test would fail the instant a
// hand-maintained constant fell out of sync with the released version, which is
// exactly the false-pairing hazard it exists to prevent.
func TestVersionMatchesFile(t *testing.T) {
	b, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("reading go/VERSION: %v", err)
	}
	want := strings.TrimSpace(string(b))
	if Version != want {
		t.Fatalf("Version = %q, want %q (from go/VERSION); the pairing constant must be embedded from the VERSION file, never hand-maintained", Version, want)
	}
}

func TestPairingGuard(t *testing.T) {
	if err := CheckRuntimeVersion(Version); err != nil {
		t.Fatalf("matching version should pair: %v", err)
	}
	if err := CheckRuntimeVersion("9.9.9"); err == nil {
		t.Fatal("mismatched version must not pair")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("RequireRuntimeVersion should panic on mismatch")
		}
	}()
	RequireRuntimeVersion("9.9.9")
}

func TestValidateBytesValid(t *testing.T) {
	p := compile(t)
	res := p.Validate([]byte(`{"format_version":1,"x":3,"label":"ok"}`), "json")
	if !res.Valid {
		t.Fatalf("expected valid, got %+v", res.Diagnostics)
	}
}

func TestValidateBytesInvalid(t *testing.T) {
	p := compile(t)
	res := p.Validate([]byte(`{"format_version":1,"x":"nope","label":""}`), "json")
	if res.Valid {
		t.Fatal("expected invalid")
	}
	want := []string{"STRICTSPEC_TYPE_NOT_INTEGER", "STRICTSPEC_VALUE_STRING_EMPTY"}
	if len(res.Diagnostics) != len(want) {
		t.Fatalf("got %d diagnostics %+v, want %d", len(res.Diagnostics), res.Diagnostics, len(want))
	}
	for i, w := range want {
		if res.Diagnostics[i].Code != w {
			t.Errorf("diag[%d] code = %s, want %s", i, res.Diagnostics[i].Code, w)
		}
		if res.Diagnostics[i].Message == "" {
			t.Errorf("diag[%d] has empty rendered message", i)
		}
	}
}

func TestGateTerminal(t *testing.T) {
	p := compile(t)
	res := p.Validate([]byte(`{"x":3,"label":"ok"}`), "json")
	if res.Valid || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "STRICTSPEC_GATE_ABSENT" {
		t.Fatalf("gate-absent should be the sole diagnostic, got %+v", res.Diagnostics)
	}
}

func TestValidateValueEntryPoint(t *testing.T) {
	p := compile(t)
	v, err := LoadValue([]byte(`{"format_version":1,"x":3,"label":"ok"}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	if res := p.ValidateValue(v); !res.Valid {
		t.Fatalf("tagged-value entry point: expected valid, got %+v", res.Diagnostics)
	}
}

func TestCoercers(t *testing.T) {
	v, err := LoadValue([]byte(`{"x":3,"label":"hi","ratio":1.5,"flag":true}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind() != KindRecord {
		t.Fatalf("root kind = %v", v.Kind())
	}
	xf, ok := v.Field("x")
	if !ok {
		t.Fatal("missing field x")
	}
	if n, ok := xf.Int(); !ok || n != 3 {
		t.Fatalf("x.Int() = %d,%v", n, ok)
	}
	rf, _ := v.Field("ratio")
	if _, ok := rf.Int(); ok {
		t.Fatal("float must not coerce to Int")
	}
	if f, ok := rf.Float(); !ok || f != 1.5 {
		t.Fatalf("ratio.Float() = %v,%v", f, ok)
	}
	lf, _ := v.Field("label")
	if s, ok := lf.AsString(); !ok || s != "hi" {
		t.Fatalf("label.AsString() = %q,%v", s, ok)
	}
	ff, _ := v.Field("flag")
	if b, ok := ff.Bool(); !ok || !b {
		t.Fatalf("flag.Bool() = %v,%v", b, ok)
	}
}

// TestValidateJSONParseErrorRenders is the regression for the parse-error render
// panic: a malformed JSON document must surface STRICTSPEC_PARSE_JSON_SYNTAX with
// a rendered message, never panic. The JSON parse template carries no {line}
// slot, so binding a `line` slot in parseDiag (as the JSONL template needs) makes
// render.Render panic on the unknown slot. This path was previously untested.
func TestValidateJSONParseErrorRenders(t *testing.T) {
	p := compile(t)
	res := p.Validate([]byte(`{"format_version": 1, "x": }`), "json")
	if res.Valid {
		t.Fatal("malformed JSON must not validate")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "STRICTSPEC_PARSE_JSON_SYNTAX" {
		t.Fatalf("expected PARSE_JSON_SYNTAX, got %+v", res.Diagnostics)
	}
	if res.Diagnostics[0].Message == "" {
		t.Fatalf("parse diagnostic message must render, got empty")
	}
}

// TestValidateTOMLParseErrorRenders is the same regression for TOML: the TOML
// parse template carries no {line} slot either.
func TestValidateTOMLParseErrorRenders(t *testing.T) {
	p := compile(t)
	res := p.Validate([]byte("x = = 3\n"), "toml")
	if res.Valid {
		t.Fatal("malformed TOML must not validate")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "STRICTSPEC_PARSE_TOML_SYNTAX" {
		t.Fatalf("expected PARSE_TOML_SYNTAX, got %+v", res.Diagnostics)
	}
	if res.Diagnostics[0].Message == "" {
		t.Fatalf("parse diagnostic message must render, got empty")
	}
}
