package strictspec

import "testing"

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

func TestVersion(t *testing.T) {
	if Version != "0.0.0" {
		t.Fatalf("Version = %q, want %q", Version, "0.0.0")
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
	if s, ok := lf.String(); !ok || s != "hi" {
		t.Fatalf("label.String() = %q,%v", s, ok)
	}
	ff, _ := v.Field("flag")
	if b, ok := ff.Bool(); !ok || !b {
		t.Fatalf("flag.Bool() = %v,%v", b, ok)
	}
}
