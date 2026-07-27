// Package doc defines the format-neutral, tagged, lexeme-retaining document
// model that the entire strictspec toolchain reads. It is the shared contract
// every backend (TOML via go-toml-edit, JSON/JSONL via an ordered decoder)
// populates: one model, three syntaxes. Values carry their lexeme class (Kind)
// and their exact source bytes (Lexeme), which is what makes both verdict
// identity (read side) and byte-stable writes (write side, later) possible.
//
// This package is intentionally dependency-light and knows nothing about
// diagnostics, schemas, or validation. Parse failures surface as the typed
// ParseError below (with a source position); conversion to toolchain
// diagnostics happens later, at the validator layer.
package doc

// Format names the concrete surface syntax a Document was parsed from.
type Format string

const (
	FormatTOML  Format = "toml"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

// Document is one parsed document: its source format, its root Node, and the
// exact source bytes it was parsed from.
type Document struct {
	// Format is the surface syntax the document was parsed from.
	Format Format
	// Root is the document's root node. For TOML and JSON it is typically a
	// Record (TOML's implicit root table; a JSON object) but may be any Node
	// (a bare JSON scalar or array is a valid JSON document).
	Root Node

	source []byte
}

// NewDocument builds a Document. The source slice is copied so the returned
// Document owns a private, immutable snapshot: Bytes() is a byte-identical
// round-trip of exactly these bytes for as long as the model is unmodified.
func NewDocument(format Format, root Node, source []byte) *Document {
	cp := make([]byte, len(source))
	copy(cp, source)
	return &Document{Format: format, Root: root, source: cp}
}

// Bytes returns the exact source bytes the document was parsed from. Because
// the model is read-only, this is a byte-identical round-trip of the source.
// The returned slice is a fresh copy; mutating it does not affect the Document.
func (d *Document) Bytes() []byte {
	out := make([]byte, len(d.source))
	copy(out, d.source)
	return out
}

// ParseError is a typed parse failure with a source position. It is the ONLY
// error a backend's Parse returns. It deliberately does NOT reference the
// toolchain's diagnostic types — backends stay decoupled from the validator
// layer, which converts these into diagnostics later.
type ParseError struct {
	Format   Format
	Position Position
	Message  string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return string(e.Format) + " parse error at line " +
		itoa(e.Position.Line) + ", column " + itoa(e.Position.Column) + ": " + e.Message
}

// itoa is a tiny dependency-free integer formatter for Error messages.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
