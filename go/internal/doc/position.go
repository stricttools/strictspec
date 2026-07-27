package doc

// Position is a single location in a source document. Line and Column are
// 1-based (matching editor and lossless-parser conventions); ByteOffset is a
// 0-based offset into the source bytes. All three are always populated for
// positions that originate from a parse; the zero Position (Line == 0) is the
// sentinel for "no source position".
type Position struct {
	Line       int // 1-based line number
	Column     int // 1-based column (counts bytes, not runes, matching the TOML substrate)
	ByteOffset int // 0-based byte offset into the source
}

// IsValid reports whether the position originated from source (Line >= 1).
func (p Position) IsValid() bool { return p.Line >= 1 }

// Span is the half-open source range [Start, End) covered by a node: Start is
// the position of the node's first byte and End is the position immediately
// after its last byte. For any scalar node parsed from source, the bytes in
// [Start.ByteOffset, End.ByteOffset) are exactly the node's Lexeme.
type Span struct {
	Start Position
	End   Position
}

// IsValid reports whether the span originated from source.
func (s Span) IsValid() bool { return s.Start.IsValid() }
