package jsondoc

import (
	"sort"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// mustParse parses src as JSON and fails the test on any parse error.
func mustParse(t *testing.T, src string) *doc.Document {
	t.Helper()
	d, perr := Parse([]byte(src))
	if perr != nil {
		t.Fatalf("unexpected parse error: %v", perr)
	}
	return d
}

// field returns the value node bound to key in a Record node.
func field(t *testing.T, n doc.Node, key string) doc.Node {
	t.Helper()
	if n.Kind() != doc.Record {
		t.Fatalf("field %q: node is %v, not a Record", key, n.Kind())
	}
	for _, e := range n.Entries() {
		if e.Key == key {
			return e.Value
		}
	}
	t.Fatalf("key %q not found", key)
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
	if d.Format != doc.FormatJSON {
		t.Errorf("Format = %q, want json", d.Format)
	}
}

// TestSpanLexemeExactness: for every scalar, source[span] == Lexeme(). This is
// the core proof that byte offsets are computed correctly, including through the
// many newlines of the pretty-printed torture document.
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

// TestKeySpanExactness: every object key's KeySpan recovers the raw key lexeme
// (quotes included) from the source.
func TestKeySpanExactness(t *testing.T) {
	d := mustParse(t, torture)
	src := d.Bytes()
	var check func(n doc.Node)
	seen := 0
	check = func(n doc.Node) {
		switch n.Kind() {
		case doc.Record:
			for _, e := range n.Entries() {
				sp := e.KeySpan
				if !sp.IsValid() {
					t.Errorf("key %q has invalid KeySpan", e.Key)
					continue
				}
				raw := string(src[sp.Start.ByteOffset:sp.End.ByteOffset])
				if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
					t.Errorf("key %q KeySpan does not bracket a quoted string: %q", e.Key, raw)
				}
				seen++
				check(e.Value)
			}
		case doc.Array:
			for _, it := range n.Items() {
				check(it)
			}
		}
	}
	check(d.Root)
	if seen == 0 {
		t.Fatal("no keys checked")
	}
}

// TestNumberLexemeClassification: the pinned integer-vs-float classification
// table. A lexeme with '.', 'e', or 'E' is a Float; otherwise an Integer. Every
// lexeme is retained exactly as written, including "-0", "-0.0", both exponent
// spellings, and a magnitude far beyond float64 (retained verbatim, never
// rejected or normalized at the lossless layer).
func TestNumberLexemeClassification(t *testing.T) {
	d := mustParse(t, torture)
	nums := field(t, d.Root, "numbers")
	cases := []struct {
		key    string
		kind   doc.Kind
		lexeme string
	}{
		{"int", doc.Integer, "42"},
		{"neg", doc.Integer, "-17"},
		{"zero", doc.Integer, "0"},
		{"neg_zero", doc.Integer, "-0"},
		{"float", doc.Float, "3.14"},
		{"neg_zero_float", doc.Float, "-0.0"},
		{"exp_lower", doc.Float, "1e5"},
		{"exp_upper", doc.Float, "1E-5"},
		{"exp_signed", doc.Float, "6.626e-34"},
		{"small_frac", doc.Float, "0.1"},
		{"big_beyond_f64", doc.Integer, "123456789012345678901234567890"},
		{"big_float", doc.Float, "1.7976931348623159e308"},
	}
	for _, c := range cases {
		n := field(t, nums, c.key)
		if n.Kind() != c.kind {
			t.Errorf("%s: Kind = %v, want %v", c.key, n.Kind(), c.kind)
		}
		if n.Lexeme() != c.lexeme {
			t.Errorf("%s: Lexeme = %q, want %q", c.key, n.Lexeme(), c.lexeme)
		}
	}
}

// TestScalarKindsAndLexemes: strings, bools, and null map to their kinds with
// exact lexemes (strings retain their quotes and raw escapes as written).
func TestScalarKindsAndLexemes(t *testing.T) {
	d := mustParse(t, torture)
	title := field(t, d.Root, "title")
	if title.Kind() != doc.String || title.Lexeme() != `"basic \"quoted\" string"` {
		t.Errorf("title = %v %q", title.Kind(), title.Lexeme())
	}
	flags := field(t, d.Root, "flags")
	if flags.Kind() != doc.Array || len(flags.Items()) != 3 {
		t.Fatalf("flags should be an Array of 3, got %v", flags.Kind())
	}
	if flags.Items()[0].Kind() != doc.Bool || flags.Items()[0].Lexeme() != "true" {
		t.Errorf("flags[0] = %v %q, want Bool true", flags.Items()[0].Kind(), flags.Items()[0].Lexeme())
	}
	if flags.Items()[1].Kind() != doc.Bool || flags.Items()[1].Lexeme() != "false" {
		t.Errorf("flags[1] = %v %q, want Bool false", flags.Items()[1].Kind(), flags.Items()[1].Lexeme())
	}
	if flags.Items()[2].Kind() != doc.Null || flags.Items()[2].Lexeme() != "null" {
		t.Errorf("flags[2] = %v %q, want Null null", flags.Items()[2].Kind(), flags.Items()[2].Lexeme())
	}
}

// TestEmptyContainers: {} and [] parse to empty Record and Array nodes.
func TestEmptyContainers(t *testing.T) {
	d := mustParse(t, torture)
	eo := field(t, d.Root, "empty_object")
	if eo.Kind() != doc.Record || len(eo.Entries()) != 0 {
		t.Errorf("empty_object = %v with %d entries", eo.Kind(), len(eo.Entries()))
	}
	ea := field(t, d.Root, "empty_array")
	if ea.Kind() != doc.Array || len(ea.Items()) != 0 {
		t.Errorf("empty_array = %v with %d items", ea.Kind(), len(ea.Items()))
	}
	es := field(t, d.Root, "empty_string")
	if es.Kind() != doc.String || es.Lexeme() != `""` {
		t.Errorf("empty_string = %v %q", es.Kind(), es.Lexeme())
	}
}

// TestNestedNavigation: deep nesting resolves to the leaf with the right kind
// and lexeme, and the leaf's span still points at real source bytes.
func TestNestedNavigation(t *testing.T) {
	d := mustParse(t, torture)
	items := field(t, field(t, field(t, field(t, d.Root, "nested"), "level1"), "level2"), "items")
	if items.Kind() != doc.Array || len(items.Items()) != 3 {
		t.Fatalf("items should be Array of 3, got %v", items.Kind())
	}
	inner := items.Items()[2]
	if inner.Kind() != doc.Array || len(inner.Items()) != 3 {
		t.Fatalf("items[2] should be Array of 3, got %v", inner.Kind())
	}
	deep := field(t, inner.Items()[2], "deep")
	if deep.Kind() != doc.String || deep.Lexeme() != `"value"` {
		t.Errorf("deep = %v %q", deep.Kind(), deep.Lexeme())
	}
	src := d.Bytes()
	sp := deep.Span()
	if string(src[sp.Start.ByteOffset:sp.End.ByteOffset]) != `"value"` {
		t.Errorf("deep span does not point at source")
	}
}

// TestOrderedEntriesPreserved: object entries appear in document order, never
// sorted (JS-object semantics are explicitly avoided by the model).
func TestOrderedEntriesPreserved(t *testing.T) {
	d := mustParse(t, `{"z":1,"a":2,"m":3,"b":4}`)
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

// TestUnicodeKeyDecoded: a key written in raw UTF-8 is stored decoded (by code
// points, no normalization) and its KeySpan brackets the raw bytes.
func TestUnicodeKeyDecoded(t *testing.T) {
	d := mustParse(t, torture)
	found := false
	for _, e := range d.Root.Entries() {
		if e.Key == "ünîcodé" {
			found = true
			src := d.Bytes()
			raw := string(src[e.KeySpan.Start.ByteOffset:e.KeySpan.End.ByteOffset])
			if raw != `"ünîcodé"` {
				t.Errorf("unicode KeySpan = %q", raw)
			}
		}
	}
	if !found {
		t.Fatal("unicode key not found in entries")
	}
}

// TestEscapeDecoding: object keys are decoded, including simple escapes, a BMP
// \u escape, and a \uXXXX astral surrogate pair.
func TestEscapeDecoding(t *testing.T) {
	cases := []struct {
		src     string
		decoded string
	}{
		{`{"a\tb":1}`, "a\tb"},
		{`{"quote\"end":1}`, `quote"end`},
		{`{"bmpé":1}`, "bmpé"},            // é via \u escape
		{`{"clef𝄞":1}`, "clef𝄞"},          // astral surrogate pair U+1D11E
		{`{"slash\/end":1}`, "slash/end"}, // escaped solidus
		{`{"nl\ncr\r":1}`, "nl\ncr\r"},    // newline + carriage return
	}
	for _, c := range cases {
		d := mustParse(t, c.src)
		got := d.Root.Entries()[0].Key
		if got != c.decoded {
			t.Errorf("%s: decoded key = %q, want %q", c.src, got, c.decoded)
		}
		// The value string node retains the raw lexeme regardless of escapes.
		if got := string(d.Bytes()); got != c.src {
			t.Errorf("round-trip failed for %q", c.src)
		}
	}
}

// TestBareScalarAndArrayDocuments: a bare scalar or array is a valid JSON
// document (the root need not be an object).
func TestBareScalarAndArrayDocuments(t *testing.T) {
	cases := []struct {
		src  string
		kind doc.Kind
	}{
		{`42`, doc.Integer},
		{`3.14`, doc.Float},
		{`"hi"`, doc.String},
		{`true`, doc.Bool},
		{`null`, doc.Null},
		{`[1,2,3]`, doc.Array},
		{`  "padded"  `, doc.String},
	}
	for _, c := range cases {
		d := mustParse(t, c.src)
		if d.Root.Kind() != c.kind {
			t.Errorf("%q: root kind = %v, want %v", c.src, d.Root.Kind(), c.kind)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%q: round-trip failed", c.src)
		}
	}
}

// TestDuplicateKeyError: a duplicate object key is a hard error positioned at the
// offending (second) key — never silently merged or last-wins.
func TestDuplicateKeyError(t *testing.T) {
	d, perr := Parse([]byte(`{"a":1,"a":2}`))
	if perr == nil {
		t.Fatal("expected duplicate-key parse error")
	}
	if d != nil {
		t.Error("document should be nil on parse error")
	}
	if perr.Position.ByteOffset != 7 || perr.Position.Line != 1 || perr.Position.Column != 8 {
		t.Errorf("duplicate-key position = %+v, want line 1 col 8 offset 7", perr.Position)
	}
	// Duplicate detection is by decoded code points: keys that decode equal but
	// are spelled differently still collide.
	if _, perr := Parse([]byte(`{"ab":1,"ab":2}`)); perr == nil {
		t.Error("expected duplicate error for escape-equal keys")
	}
}

// TestParseErrorsWithPositions: a spread of invalid inputs, each yielding a
// typed *doc.ParseError with the exact expected position and the json tag.
func TestParseErrorsWithPositions(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		line   int
		col    int
		offset int
	}{
		{"unterminated object", `{"a":1`, 1, 7, 6},
		{"unterminated array", `[1,2`, 1, 5, 4},
		{"unterminated string", `"nope`, 1, 6, 5},
		{"missing colon", `{"a" 1}`, 1, 6, 5},
		{"missing value", `{"a":}`, 1, 6, 5},
		{"trailing content", `123 456`, 1, 5, 4},
		{"invalid literal", `nul`, 1, 1, 0},
		{"trailing comma array", `[1,]`, 1, 4, 3},
		{"trailing comma object", `{"a":1,}`, 1, 8, 7},
		{"bare NaN", `NaN`, 1, 1, 0},
		{"bare Infinity", `Infinity`, 1, 1, 0},
		{"bad escape", `"\x"`, 1, 2, 1},
		{"fraction no digit", `1.`, 1, 3, 2},
		{"exponent no digit", `1e`, 1, 3, 2},
		{"control char in string", "\"line\nbreak\"", 1, 6, 5},
		{"empty input", ``, 1, 1, 0},
		{"whitespace only", "   \n  ", 1, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, perr := Parse([]byte(c.src))
			if perr == nil {
				t.Fatalf("expected parse error for %q, got none", c.src)
			}
			if d != nil {
				t.Error("document should be nil on parse error")
			}
			if perr.Format != doc.FormatJSON {
				t.Errorf("Format = %q, want json", perr.Format)
			}
			if perr.Position.Line != c.line || perr.Position.Column != c.col || perr.Position.ByteOffset != c.offset {
				t.Errorf("position = %+v, want line %d col %d offset %d", perr.Position, c.line, c.col, c.offset)
			}
			if !perr.Position.IsValid() {
				t.Errorf("position should be valid: %+v", perr.Position)
			}
			if perr.Error() == "" {
				t.Errorf("empty error string")
			}
		})
	}
}

// TestInvalidUTF8Position: an invalid UTF-8 byte inside a string is a parse error
// positioned at the bad byte.
func TestInvalidUTF8Position(t *testing.T) {
	src := "\"\xff\"" // opening quote, lone 0xFF, closing quote
	_, perr := Parse([]byte(src))
	if perr == nil {
		t.Fatal("expected invalid-UTF-8 parse error")
	}
	if perr.Position.ByteOffset != 1 || perr.Position.Line != 1 || perr.Position.Column != 2 {
		t.Errorf("position = %+v, want line 1 col 2 offset 1", perr.Position)
	}
}

// TestDeepNestingBounded: pathological nesting returns a clean ParseError rather
// than overflowing the stack (the bounded-recursion stack-safety guard).
func TestDeepNestingBounded(t *testing.T) {
	n := maxDepth + 50
	src := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		src = append(src, '[')
	}
	for i := 0; i < n; i++ {
		src = append(src, ']')
	}
	d, perr := Parse(src)
	if perr == nil {
		t.Fatal("expected a nesting-depth parse error, got none (possible stack risk)")
	}
	if d != nil {
		t.Error("document should be nil on parse error")
	}
	// A merely deep (but within-bound) document must still parse.
	ok := make([]byte, 0)
	for i := 0; i < 100; i++ {
		ok = append(ok, '[')
	}
	ok = append(ok, '1')
	for i := 0; i < 100; i++ {
		ok = append(ok, ']')
	}
	if _, perr := Parse(ok); perr != nil {
		t.Errorf("100-deep array should parse, got %v", perr)
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
