package tomldoc

import (
	"sort"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// mustParse parses src and fails the test on any parse error.
func mustParse(t *testing.T, src string) *doc.Document {
	t.Helper()
	d, perr := Parse([]byte(src))
	if perr != nil {
		t.Fatalf("unexpected parse error: %v", perr)
	}
	return d
}

// rootEntry returns the value node bound to key at the document root.
func rootEntry(t *testing.T, d *doc.Document, key string) doc.Node {
	t.Helper()
	for _, e := range d.Root.Entries() {
		if e.Key == key {
			return e.Value
		}
	}
	t.Fatalf("root key %q not found", key)
	return nil
}

// walkScalars invokes f on every scalar (leaf) node in the tree.
func walkScalars(n doc.Node, f func(doc.Node)) {
	switch n.Kind() {
	case doc.Record:
		for _, e := range n.Entries() {
			walkScalars(e.Value, f)
		}
	case doc.Array:
		for _, it := range n.Items() {
			walkScalars(it, f)
		}
	default:
		f(n)
	}
}

// TestRoundTripByteIdentity: Bytes() reproduces the source exactly.
func TestRoundTripByteIdentity(t *testing.T) {
	d := mustParse(t, torture)
	if got := string(d.Bytes()); got != torture {
		t.Fatalf("Bytes() is not byte-identical to source\n--- got ---\n%q\n--- want ---\n%q", got, torture)
	}
	if d.Format != doc.FormatTOML {
		t.Errorf("Format = %q, want toml", d.Format)
	}
}

// TestSpanLexemeExactness: for every scalar, source[span] == Lexeme(). This is
// the core validation that byte offsets are computed correctly from the
// substrate's line/column positions.
func TestSpanLexemeExactness(t *testing.T) {
	d := mustParse(t, torture)
	src := d.Bytes()
	count := 0
	walkScalars(d.Root, func(n doc.Node) {
		count++
		sp := n.Span()
		if !sp.IsValid() {
			t.Errorf("scalar %q (%v) has invalid span", n.Lexeme(), n.Kind())
			return
		}
		s, e := sp.Start.ByteOffset, sp.End.ByteOffset
		if s < 0 || e > len(src) || s > e {
			t.Errorf("scalar %q has out-of-range span [%d,%d) len=%d", n.Lexeme(), s, e, len(src))
			return
		}
		if got := string(src[s:e]); got != n.Lexeme() {
			t.Errorf("span/lexeme mismatch: source[%d:%d]=%q but Lexeme()=%q (kind %v)", s, e, got, n.Lexeme(), n.Kind())
		}
	})
	if count == 0 {
		t.Fatal("no scalars walked")
	}
}

// TestLexemeExactValues: representative values retain their exact non-canonical
// lexeme and correct lexeme-class Kind.
func TestLexemeExactValues(t *testing.T) {
	d := mustParse(t, torture)
	cases := []struct {
		key    string
		kind   doc.Kind
		lexeme string
	}{
		{"title", doc.String, `"basic \"quoted\" string"`},
		{"literal", doc.String, `'C:\path\no\escape'`},
		{"dec", doc.Integer, "1_000"},
		{"neg", doc.Integer, "-17"},
		{"hex", doc.Integer, "0xDEAD_beef"},
		{"oct", doc.Integer, "0o755"},
		{"bin", doc.Integer, "0b1010"},
		{"big", doc.Integer, "9_223_372_036_854_775_807"},
		{"f1", doc.Float, "1.0"},
		{"f2", doc.Float, "3.14"},
		{"f3", doc.Float, "1e5"},
		{"negzero", doc.Float, "-0.0"},
		{"planck", doc.Float, "6.626e-34"},
		{"inf_pos", doc.Float, "inf"},
		{"inf_neg", doc.Float, "-inf"},
		{"not_a_num", doc.Float, "nan"},
		{"yes", doc.Bool, "true"},
		{"no", doc.Bool, "false"},
		{"odt", doc.DateTimeOffset, "1979-05-27T07:32:00Z"},
		{"ldt", doc.DateTimeLocal, "1979-05-27T07:32:00"},
		{"ld", doc.DateLocal, "1979-05-27"},
		{"lt", doc.TimeLocal, "07:32:00"},
	}
	for _, c := range cases {
		n := rootEntry(t, d, c.key)
		if n.Kind() != c.kind {
			t.Errorf("%s: Kind = %v, want %v", c.key, n.Kind(), c.kind)
		}
		if n.Lexeme() != c.lexeme {
			t.Errorf("%s: Lexeme = %q, want %q", c.key, n.Lexeme(), c.lexeme)
		}
	}
}

// TestFloatIntegerDistinctByKind: 1.0 is a Float, 1000 is an Integer, though a
// naive reader might treat them as equal numbers. The lexeme class distinguishes.
func TestFloatIntegerDistinctByKind(t *testing.T) {
	d := mustParse(t, "i = 1\nf = 1.0\ne = 1e3\n")
	if got := rootEntry(t, d, "i").Kind(); got != doc.Integer {
		t.Errorf("1 should be Integer, got %v", got)
	}
	if got := rootEntry(t, d, "f").Kind(); got != doc.Float {
		t.Errorf("1.0 should be Float, got %v", got)
	}
	if got := rootEntry(t, d, "e").Kind(); got != doc.Float {
		t.Errorf("1e3 should be Float, got %v", got)
	}
}

// TestScalarSpansNonOverlapping: distinct scalar value spans never overlap.
func TestScalarSpansNonOverlapping(t *testing.T) {
	d := mustParse(t, torture)
	type rng struct{ s, e int }
	var spans []rng
	walkScalars(d.Root, func(n doc.Node) {
		sp := n.Span()
		spans = append(spans, rng{sp.Start.ByteOffset, sp.End.ByteOffset})
	})
	sort.Slice(spans, func(i, j int) bool { return spans[i].s < spans[j].s })
	for i := 1; i < len(spans); i++ {
		if spans[i].s < spans[i-1].e {
			t.Errorf("overlapping scalar spans: [%d,%d) and [%d,%d)", spans[i-1].s, spans[i-1].e, spans[i].s, spans[i].e)
		}
	}
}

// TestOrderedEntriesPreserved: root entries appear in document order.
func TestOrderedEntriesPreserved(t *testing.T) {
	d := mustParse(t, "z = 1\na = 2\nm = 3\nb = 4\n")
	var got []string
	for _, e := range d.Root.Entries() {
		got = append(got, e.Key)
	}
	want := []string{"z", "a", "m", "b"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

// TestDottedKeyResolution: fruit.name / fruit.color fold into a fruit record
// with two ordered string entries, while leaf spans still point at the source.
func TestDottedKeyResolution(t *testing.T) {
	d := mustParse(t, torture)
	fruit := rootEntry(t, d, "fruit")
	if fruit.Kind() != doc.Record {
		t.Fatalf("fruit should be a Record, got %v", fruit.Kind())
	}
	entries := fruit.Entries()
	if len(entries) != 2 || entries[0].Key != "name" || entries[1].Key != "color" {
		t.Fatalf("fruit entries = %+v, want name,color", entries)
	}
	if entries[0].Value.Lexeme() != `"apple"` || entries[1].Value.Lexeme() != `"red"` {
		t.Errorf("fruit values = %q,%q", entries[0].Value.Lexeme(), entries[1].Value.Lexeme())
	}
	// Leaf span still points at real source bytes.
	src := d.Bytes()
	sp := entries[0].Value.Span()
	if string(src[sp.Start.ByteOffset:sp.End.ByteOffset]) != `"apple"` {
		t.Errorf("dotted-key leaf span does not point at source")
	}
}

// TestDeepDottedKey: a three-level dotted key nests records.
func TestDeepDottedKey(t *testing.T) {
	d := mustParse(t, "a.b.c = 42\n")
	a := rootEntry(t, d, "a")
	if a.Kind() != doc.Record {
		t.Fatalf("a should be Record")
	}
	b := a.Entries()[0].Value
	if a.Entries()[0].Key != "b" || b.Kind() != doc.Record {
		t.Fatalf("a.b should be Record")
	}
	c := b.Entries()[0].Value
	if b.Entries()[0].Key != "c" || c.Kind() != doc.Integer || c.Lexeme() != "42" {
		t.Fatalf("a.b.c mismatch: %+v", c)
	}
}

// TestNestedTables: [table_a] then [table_a.sub] nest correctly with order
// key, count, sub.
func TestNestedTables(t *testing.T) {
	d := mustParse(t, torture)
	ta := rootEntry(t, d, "table_a")
	if ta.Kind() != doc.Record {
		t.Fatalf("table_a should be Record")
	}
	var keys []string
	for _, e := range ta.Entries() {
		keys = append(keys, e.Key)
	}
	if len(keys) != 3 || keys[0] != "key" || keys[1] != "count" || keys[2] != "sub" {
		t.Fatalf("table_a entries = %v, want key,count,sub", keys)
	}
	sub := ta.Entries()[2].Value
	if sub.Kind() != doc.Record || sub.Entries()[0].Key != "deep" || sub.Entries()[0].Value.Kind() != doc.Bool {
		t.Fatalf("table_a.sub mismatch: %+v", sub)
	}
}

// TestArrayOfTables: [[products]] folds into an Array of two records.
func TestArrayOfTables(t *testing.T) {
	d := mustParse(t, torture)
	products := rootEntry(t, d, "products")
	if products.Kind() != doc.Array {
		t.Fatalf("products should be Array, got %v", products.Kind())
	}
	items := products.Items()
	if len(items) != 2 {
		t.Fatalf("products len = %d, want 2", len(items))
	}
	for i, want := range []string{`"hammer"`, `"nail"`} {
		rec := items[i]
		if rec.Kind() != doc.Record {
			t.Fatalf("products[%d] should be Record", i)
		}
		name := rec.Entries()[0]
		if name.Key != "name" || name.Value.Lexeme() != want {
			t.Errorf("products[%d].name = %q, want %q", i, name.Value.Lexeme(), want)
		}
		if rec.Entries()[1].Key != "sku" || rec.Entries()[1].Value.Kind() != doc.Integer {
			t.Errorf("products[%d].sku mismatch", i)
		}
	}
}

// TestInlineTableAndArrays: inline table and inline/multiline arrays map to
// Record and Array nodes.
func TestInlineTableAndArrays(t *testing.T) {
	d := mustParse(t, torture)

	inline := rootEntry(t, d, "inline")
	if inline.Kind() != doc.Record {
		t.Fatalf("inline should be Record, got %v", inline.Kind())
	}
	var ikeys []string
	for _, e := range inline.Entries() {
		ikeys = append(ikeys, e.Key)
	}
	if len(ikeys) != 3 || ikeys[0] != "x" || ikeys[1] != "y" || ikeys[2] != "label" {
		t.Fatalf("inline entries = %v, want x,y,label", ikeys)
	}
	if inline.Entries()[1].Value.Kind() != doc.Float || inline.Entries()[1].Value.Lexeme() != "2.5" {
		t.Errorf("inline.y mismatch")
	}

	arr := rootEntry(t, d, "arr")
	if arr.Kind() != doc.Array || len(arr.Items()) != 3 {
		t.Fatalf("arr should be Array of 3, got %v len %d", arr.Kind(), len(arr.Items()))
	}
	for i, want := range []string{"1", "2", "3"} {
		if arr.Items()[i].Lexeme() != want || arr.Items()[i].Kind() != doc.Integer {
			t.Errorf("arr[%d] = %q", i, arr.Items()[i].Lexeme())
		}
	}

	nested := rootEntry(t, d, "nested_arr")
	if nested.Kind() != doc.Array || len(nested.Items()) != 2 {
		t.Fatalf("nested_arr should be Array of 2")
	}
	if nested.Items()[0].Lexeme() != `"a"` || nested.Items()[1].Lexeme() != `"b"` {
		t.Errorf("nested_arr items = %q,%q", nested.Items()[0].Lexeme(), nested.Items()[1].Lexeme())
	}
}

// TestMultilineStringSpansAcrossLines: a multiline basic string's lexeme spans
// several source lines and recovers exactly from its byte-offset span.
func TestMultilineStringSpansAcrossLines(t *testing.T) {
	d := mustParse(t, torture)
	ml := rootEntry(t, d, "multiline")
	if ml.Kind() != doc.String {
		t.Fatalf("multiline should be String")
	}
	want := "\"\"\"\nline one\nline two\"\"\""
	if ml.Lexeme() != want {
		t.Errorf("multiline lexeme = %q, want %q", ml.Lexeme(), want)
	}
	sp := ml.Span()
	if sp.Start.Line == sp.End.Line {
		t.Errorf("multiline span should cross lines, got start line %d end line %d", sp.Start.Line, sp.End.Line)
	}
	src := d.Bytes()
	if string(src[sp.Start.ByteOffset:sp.End.ByteOffset]) != want {
		t.Errorf("multiline span does not recover source lexeme")
	}
}

// TestEmptyAndWhitespaceDocuments: pinned edge inputs parse to empty records
// and round-trip byte-identically.
func TestEmptyAndWhitespaceDocuments(t *testing.T) {
	for _, src := range []string{"", "\n", "   \n\t\n", "# just a comment\n"} {
		d, perr := Parse([]byte(src))
		if perr != nil {
			t.Fatalf("empty/whitespace parse error for %q: %v", src, perr)
		}
		if d.Root.Kind() != doc.Record {
			t.Errorf("root of %q should be Record", src)
		}
		if len(d.Root.Entries()) != 0 {
			t.Errorf("root of %q should have no entries", src)
		}
		if string(d.Bytes()) != src {
			t.Errorf("byte-identity failed for %q", src)
		}
	}
}

// TestParseErrors: invalid inputs yield a typed *doc.ParseError with a position,
// no partial document, and the toml format tag.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing value", "key = \n"},
		{"missing key", "= 1\n"},
		{"unterminated string", "key = \"unterminated\n"},
		{"unclosed table header", "[unclosed\n"},
		{"duplicate key", "a = 1\na = 2\n"},
		{"duplicate table", "[t]\nx = 1\n[t]\ny = 2\n"},
		{"bad integer", "n = 1__000\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, perr := Parse([]byte(c.src))
			if perr == nil {
				t.Fatalf("expected parse error for %q, got none", c.src)
			}
			if d != nil {
				t.Errorf("document should be nil on parse error")
			}
			if perr.Format != doc.FormatTOML {
				t.Errorf("Format = %q, want toml", perr.Format)
			}
			if !perr.Position.IsValid() {
				t.Errorf("parse error should carry a valid position, got %+v", perr.Position)
			}
			// error message is non-empty and formats without panicking.
			if perr.Error() == "" {
				t.Errorf("empty error string")
			}
		})
	}
}

// TestParseErrorByteOffset: the reported byte offset points into the source at
// the reported line/column.
func TestParseErrorByteOffset(t *testing.T) {
	src := "ok = 1\nbad = \n"
	_, perr := Parse([]byte(src))
	if perr == nil {
		t.Fatal("expected parse error")
	}
	off := perr.Position.ByteOffset
	if off < 0 || off > len(src) {
		t.Fatalf("byte offset %d out of range for len %d", off, len(src))
	}
}
