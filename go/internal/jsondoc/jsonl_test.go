package jsondoc

import (
	"strings"
	"testing"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// TestJSONLMultiLineValid: several documents, one per line, parse into an ordered
// slice; each document is FormatJSONL and round-trips its own line bytes.
func TestJSONLMultiLineValid(t *testing.T) {
	src := "{\"a\":1}\n[1,2,3]\n\"bare\"\n42\ntrue\n"
	docs, perr := ParseLines([]byte(src))
	if perr != nil {
		t.Fatalf("unexpected parse error: %v", perr)
	}
	if len(docs) != 5 {
		t.Fatalf("got %d documents, want 5", len(docs))
	}
	wantKinds := []doc.Kind{doc.Record, doc.Array, doc.String, doc.Integer, doc.Bool}
	wantBytes := []string{`{"a":1}`, `[1,2,3]`, `"bare"`, `42`, `true`}
	for i, d := range docs {
		if d.Format != doc.FormatJSONL {
			t.Errorf("doc %d format = %q, want jsonl", i, d.Format)
		}
		if d.Root.Kind() != wantKinds[i] {
			t.Errorf("doc %d kind = %v, want %v", i, d.Root.Kind(), wantKinds[i])
		}
		if string(d.Bytes()) != wantBytes[i] {
			t.Errorf("doc %d bytes = %q, want %q", i, string(d.Bytes()), wantBytes[i])
		}
	}
}

// TestJSONLGlobalPositions: positions are relative to the WHOLE input. A value on
// line 3 reports its true global byte offset and global line number.
func TestJSONLGlobalPositions(t *testing.T) {
	// Line 1: {"a":1}\n  (8 bytes, offsets 0..7)
	// Line 2: {"b":2}\n  (8 bytes, offsets 8..15)
	// Line 3: {"c":333}  (starts at offset 16)
	src := "{\"a\":1}\n{\"b\":2}\n{\"c\":333}\n"
	docs, perr := ParseLines([]byte(src))
	if perr != nil {
		t.Fatalf("unexpected parse error: %v", perr)
	}
	if len(docs) != 3 {
		t.Fatalf("got %d documents, want 3", len(docs))
	}
	c := field(t, docs[2].Root, "c")
	sp := c.Span()
	if c.Lexeme() != "333" {
		t.Fatalf("line-3 value lexeme = %q, want 333", c.Lexeme())
	}
	if sp.Start.Line != 3 {
		t.Errorf("line-3 value line = %d, want 3", sp.Start.Line)
	}
	if sp.Start.ByteOffset != 21 {
		t.Errorf("line-3 value byte offset = %d, want 21 (global)", sp.Start.ByteOffset)
	}
	// The global offset indexes into the whole input, not the per-line document.
	if string([]byte(src)[sp.Start.ByteOffset:sp.End.ByteOffset]) != "333" {
		t.Errorf("global span does not recover the value from the whole input")
	}
}

// TestJSONLFinalLineWithoutLF: a final line lacking a trailing LF is valid (the
// pinned edge case), and a trailing LF does not create an empty final document.
func TestJSONLFinalLineWithoutLF(t *testing.T) {
	withLF, _ := ParseLines([]byte("{\"a\":1}\n{\"b\":2}\n"))
	noLF, _ := ParseLines([]byte("{\"a\":1}\n{\"b\":2}"))
	if len(withLF) != 2 || len(noLF) != 2 {
		t.Fatalf("counts = %d (with LF) and %d (no LF), want 2 and 2", len(withLF), len(noLF))
	}
	single, perr := ParseLines([]byte("42"))
	if perr != nil || len(single) != 1 || single[0].Root.Lexeme() != "42" {
		t.Errorf("single unterminated line: docs=%d perr=%v", len(single), perr)
	}
}

// TestJSONLEmptyInput: empty input yields zero documents and no error.
func TestJSONLEmptyInput(t *testing.T) {
	docs, perr := ParseLines(nil)
	if perr != nil {
		t.Fatalf("empty input should not error, got %v", perr)
	}
	if len(docs) != 0 {
		t.Errorf("empty input should yield 0 documents, got %d", len(docs))
	}
}

// TestJSONLBlankLineError: a blank (empty or whitespace-only) line in the middle
// of a stream is a hard error at that line — never skipped.
func TestJSONLBlankLineError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    string
		line   int
		offset int
	}{
		{"empty middle line", "{}\n\n{}\n", 2, 3},
		{"whitespace middle line", "{}\n   \n{}\n", 2, 3},
		{"leading blank line", "\n{}\n", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs, perr := ParseLines([]byte(tc.src))
			if perr == nil {
				t.Fatalf("expected blank-line error for %q", tc.src)
			}
			if docs != nil {
				t.Error("documents should be nil on error")
			}
			if perr.Format != doc.FormatJSONL {
				t.Errorf("format = %q, want jsonl", perr.Format)
			}
			if perr.Position.Line != tc.line || perr.Position.ByteOffset != tc.offset {
				t.Errorf("position = %+v, want line %d offset %d", perr.Position, tc.line, tc.offset)
			}
			if !strings.Contains(perr.Message, "lank line") {
				t.Errorf("message = %q, want blank-line wording", perr.Message)
			}
		})
	}
}

// TestJSONLTrailingCRError: a line ending in CR (a CRLF ending) is a hard error;
// JSONL is LF-only. The position points at the CR byte.
func TestJSONLTrailingCRError(t *testing.T) {
	src := "{}\r\n{}\n" // line 1 ends with \r before the \n
	docs, perr := ParseLines([]byte(src))
	if perr == nil {
		t.Fatal("expected trailing-CR error")
	}
	if docs != nil {
		t.Error("documents should be nil on error")
	}
	if perr.Position.Line != 1 || perr.Position.Column != 3 || perr.Position.ByteOffset != 2 {
		t.Errorf("position = %+v, want line 1 col 3 offset 2", perr.Position)
	}
	if !strings.Contains(perr.Message, "carriage return") {
		t.Errorf("message = %q, want carriage-return wording", perr.Message)
	}
}

// TestJSONLOneBadLine: a malformed line reports an error positioned on that line;
// its global line number is correct.
func TestJSONLOneBadLine(t *testing.T) {
	src := "{\"a\":1}\n{bad}\n{\"c\":3}\n"
	docs, perr := ParseLines([]byte(src))
	if perr == nil {
		t.Fatal("expected a per-line parse error")
	}
	if docs != nil {
		t.Error("documents should be nil on error")
	}
	if perr.Position.Line != 2 {
		t.Errorf("error line = %d, want 2", perr.Position.Line)
	}
	// Offset is global: line 2 starts at byte 8 ({"a":1}\n = 8 bytes).
	if perr.Position.ByteOffset < 8 || perr.Position.ByteOffset > 12 {
		t.Errorf("error offset = %d, want within line 2 [8,12]", perr.Position.ByteOffset)
	}
}

// TestJSONLDuplicateKeyPerLine: duplicate keys are rejected per line, positioned
// with a global offset.
func TestJSONLDuplicateKeyPerLine(t *testing.T) {
	src := "{\"a\":1}\n{\"b\":2,\"b\":3}\n"
	_, perr := ParseLines([]byte(src))
	if perr == nil {
		t.Fatal("expected duplicate-key error on line 2")
	}
	if perr.Position.Line != 2 {
		t.Errorf("error line = %d, want 2", perr.Position.Line)
	}
	if !strings.Contains(perr.Message, "duplicate") {
		t.Errorf("message = %q, want duplicate wording", perr.Message)
	}
}

// TestStreamSliceParity: the streaming reader and the slice API produce identical
// documents (same count, kinds, lexemes, and GLOBAL spans) for the same input.
func TestStreamSliceParity(t *testing.T) {
	src := "{\"a\":1}\n[1,2,3]\n\"x\"\n{\"nested\":{\"k\":true}}\n42\n"

	slice, perr := ParseLines([]byte(src))
	if perr != nil {
		t.Fatalf("ParseLines error: %v", perr)
	}

	var streamed []*doc.Document
	sperr, ioErr := ParseStream(strings.NewReader(src), func(d *doc.Document) {
		streamed = append(streamed, d)
	})
	if ioErr != nil {
		t.Fatalf("ParseStream io error: %v", ioErr)
	}
	if sperr != nil {
		t.Fatalf("ParseStream parse error: %v", sperr)
	}

	if len(slice) != len(streamed) {
		t.Fatalf("doc counts differ: slice=%d stream=%d", len(slice), len(streamed))
	}
	for i := range slice {
		a := flatten(slice[i].Root)
		b := flatten(streamed[i].Root)
		if a != b {
			t.Errorf("doc %d differs:\n slice=%s\nstream=%s", i, a, b)
		}
		if string(slice[i].Bytes()) != string(streamed[i].Bytes()) {
			t.Errorf("doc %d bytes differ", i)
		}
	}
}

// TestStreamParseErrorParity: a framing/parse error surfaces identically from the
// streaming reader and the slice API.
func TestStreamParseErrorParity(t *testing.T) {
	src := "{\"a\":1}\n{bad}\n"
	_, slicePerr := ParseLines([]byte(src))
	streamPerr, ioErr := ParseStream(strings.NewReader(src), func(*doc.Document) {})
	if ioErr != nil {
		t.Fatalf("unexpected io error: %v", ioErr)
	}
	if slicePerr == nil || streamPerr == nil {
		t.Fatalf("both should error: slice=%v stream=%v", slicePerr, streamPerr)
	}
	if slicePerr.Position != streamPerr.Position || slicePerr.Message != streamPerr.Message {
		t.Errorf("errors differ:\n slice=%+v\nstream=%+v", slicePerr, streamPerr)
	}
}

// flatten renders a node's scalar lexemes and global byte spans into a stable
// string, used to compare documents from the two JSONL APIs.
func flatten(n doc.Node) string {
	var sb strings.Builder
	var walk func(doc.Node)
	walk = func(n doc.Node) {
		switch n.Kind() {
		case doc.Record:
			sb.WriteByte('{')
			for _, e := range n.Entries() {
				sb.WriteString(e.Key)
				sb.WriteByte('=')
				walk(e.Value)
				sb.WriteByte(';')
			}
			sb.WriteByte('}')
		case doc.Array:
			sb.WriteByte('[')
			for _, it := range n.Items() {
				walk(it)
				sb.WriteByte(';')
			}
			sb.WriteByte(']')
		default:
			sp := n.Span()
			sb.WriteString(n.Kind().String())
			sb.WriteByte(':')
			sb.WriteString(n.Lexeme())
			sb.WriteByte('@')
			sb.WriteString(itoa(sp.Start.ByteOffset))
			sb.WriteByte('-')
			sb.WriteString(itoa(sp.End.ByteOffset))
		}
	}
	walk(n)
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
