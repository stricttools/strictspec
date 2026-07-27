package write

import (
	"math"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// TestFixpoint: reading then writing an untouched document is byte-identical
// (lexeme retention via splicing over the original source).
func TestFixpoint(t *testing.T) {
	cases := []struct {
		format doc.Format
		src    string
	}{
		{doc.FormatJSON, "{\n  \"format_version\": 1,\n  \"a\": 1e3,\n  \"b\": [1, 2, 3]\n}"},
		{doc.FormatJSON, `{"x": -0.0, "y": 5.0, "z": "hi\tthere"}`},
		{doc.FormatTOML, "format_version = 1\nname = \"x\"\n[budget]\nmax = 5.0\n"},
	}
	for _, c := range cases {
		d, err := New(c.format, []byte(c.src))
		if err != nil {
			t.Fatalf("New(%s): %v", c.format, err)
		}
		if string(d.Bytes()) != c.src {
			t.Fatalf("fixpoint broken for %s:\n got %q\nwant %q", c.format, d.Bytes(), c.src)
		}
	}
}

// TestSerializeNonCurrentRefusal: the producer-current-only guard hard-errors
// when the document's format_version is not the schema's current one.
func TestSerializeNonCurrentRefusal(t *testing.T) {
	src := []byte(`{"format_version": 1, "a": 1}`)
	d, err := New(doc.FormatJSON, src)
	if err != nil {
		t.Fatal(err)
	}
	// Current schema is at version 2: writing a v1 doc is refused.
	_, refusal := Serialize(d.Root(), d.Bytes(), 2, "AgentDefinition")
	if refusal == nil || refusal.Code != "STRICTSPEC_SERIALIZE_NONCURRENT" {
		t.Fatalf("expected SERIALIZE_NONCURRENT, got %v", refusal)
	}
	// A v2 doc serializes byte-identically.
	src2 := []byte(`{"format_version": 2, "a": 1}`)
	d2, _ := New(doc.FormatJSON, src2)
	out, refusal2 := Serialize(d2.Root(), d2.Bytes(), 2, "AgentDefinition")
	if refusal2 != nil {
		t.Fatalf("unexpected refusal: %v", refusal2)
	}
	if string(out) != string(src2) {
		t.Fatalf("serialize not byte-identical: %q", out)
	}
}

// TestRenderFloat: constructed floats are always float-marked (A.3/A.6).
func TestRenderFloat(t *testing.T) {
	cases := map[float64]string{
		5:     "5.0",
		1.5:   "1.5",
		1000:  "1000.0",
		1e21:  "1e+21",
		0.001: "0.001",
	}
	for f, want := range cases {
		if got := RenderFloat(f); got != want {
			t.Errorf("RenderFloat(%v) = %q, want %q", f, got, want)
		}
	}
	// Negative zero retains its sign, float-marked (A.6 pin).
	if got := RenderFloat(math.Copysign(0, -1)); got != "-0.0" {
		t.Errorf("RenderFloat(-0.0) = %q, want -0.0", got)
	}
}

// TestRenderConstructedFloatMarked: a wrapped/injected integral float renders
// with the trailing .0 (the flagship [5.0] rule).
func TestRenderConstructedFloatMarked(t *testing.T) {
	// A TOML-parsed float literal 5.0 renders 5.0 in JSON.
	d, _ := New(doc.FormatTOML, []byte("v = 5.0\n"))
	var lit doc.Node
	for _, e := range d.Root().Entries() {
		if e.Key == "v" {
			lit = e.Value
		}
	}
	out, err := RenderConstructed(lit, doc.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "5.0" {
		t.Fatalf("constructed float = %q, want 5.0", out)
	}
}
