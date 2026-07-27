package doc

import "testing"

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		Record:         "Record",
		Array:          "Array",
		String:         "String",
		Integer:        "Integer",
		Float:          "Float",
		Bool:           "Bool",
		Null:           "Null",
		DateTimeOffset: "DateTimeOffset",
		DateTimeLocal:  "DateTimeLocal",
		DateLocal:      "DateLocal",
		TimeLocal:      "TimeLocal",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
	if got := Kind(99).String(); got != "Kind(99)" {
		t.Errorf("unknown kind = %q, want %q", got, "Kind(99)")
	}
}

func TestScalarNode(t *testing.T) {
	sp := Span{Start: Position{1, 1, 0}, End: Position{1, 6, 5}}
	n := NewScalar(Integer, "1_000", sp)
	if n.Kind() != Integer {
		t.Errorf("Kind = %v, want Integer", n.Kind())
	}
	if n.Lexeme() != "1_000" {
		t.Errorf("Lexeme = %q, want %q", n.Lexeme(), "1_000")
	}
	if n.Span() != sp {
		t.Errorf("Span = %+v, want %+v", n.Span(), sp)
	}
	if n.Entries() != nil {
		t.Errorf("Entries on scalar should be nil")
	}
	if n.Items() != nil {
		t.Errorf("Items on scalar should be nil")
	}
}

func TestNewScalarPanicsOnContainerKind(t *testing.T) {
	for _, k := range []Kind{Record, Array} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewScalar(%v) should panic", k)
				}
			}()
			NewScalar(k, "", Span{})
		}()
	}
}

func TestRecordNode(t *testing.T) {
	child := NewScalar(String, `"v"`, Span{})
	entries := []Entry{{Key: "k", Value: child}}
	n := NewRecord(entries, Span{})
	if n.Kind() != Record {
		t.Errorf("Kind = %v, want Record", n.Kind())
	}
	if n.Lexeme() != "" {
		t.Errorf("record Lexeme should be empty, got %q", n.Lexeme())
	}
	if len(n.Entries()) != 1 || n.Entries()[0].Key != "k" {
		t.Errorf("Entries mismatch: %+v", n.Entries())
	}
	if n.Items() != nil {
		t.Errorf("Items on record should be nil")
	}
}

func TestArrayNode(t *testing.T) {
	items := []Node{NewScalar(Integer, "1", Span{}), NewScalar(Integer, "2", Span{})}
	n := NewArray(items, Span{})
	if n.Kind() != Array {
		t.Errorf("Kind = %v, want Array", n.Kind())
	}
	if n.Lexeme() != "" {
		t.Errorf("array Lexeme should be empty, got %q", n.Lexeme())
	}
	if len(n.Items()) != 2 {
		t.Errorf("Items len = %d, want 2", len(n.Items()))
	}
	if n.Entries() != nil {
		t.Errorf("Entries on array should be nil")
	}
}

func TestDocumentBytesCopyIndependence(t *testing.T) {
	src := []byte("a = 1\n")
	d := NewDocument(FormatTOML, NewRecord(nil, Span{}), src)
	// Mutating the original source must not affect the document.
	src[0] = 'X'
	if got := d.Bytes(); string(got) != "a = 1\n" {
		t.Errorf("Document did not snapshot source: got %q", got)
	}
	// Mutating a returned slice must not affect the document.
	b := d.Bytes()
	b[0] = 'Z'
	if got := d.Bytes(); string(got) != "a = 1\n" {
		t.Errorf("Bytes() returned an aliased slice: got %q", got)
	}
}

func TestParseErrorMessage(t *testing.T) {
	e := &ParseError{Format: FormatTOML, Position: Position{Line: 3, Column: 5, ByteOffset: 20}, Message: "boom"}
	want := "toml parse error at line 3, column 5: boom"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// ParseError satisfies the error interface.
	var _ error = e
}
