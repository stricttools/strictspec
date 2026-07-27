// Package tomldoc is the TOML backend of the strictspec document model. It
// parses TOML bytes with the lossless go-toml-edit substrate and folds the
// result into the format-neutral doc model: every TOML value node maps to a
// tagged, lexeme-retaining doc.Node, dotted keys resolve into the record tree
// per TOML semantics (while spans point at the source), and the original bytes
// round-trip byte-identically via Document.Bytes().
package tomldoc

import (
	"errors"

	tomledit "github.com/smm-h/go-toml-edit"
	"github.com/smm-h/strictspec/go/internal/doc"
)

// Parse parses TOML source into a doc.Document. On a syntax or TOML-semantic
// error (including duplicate keys, which the substrate rejects), it returns a
// typed *doc.ParseError with a source position and a nil document. Parse never
// returns a plain error — the only failure type is *doc.ParseError.
func Parse(src []byte) (*doc.Document, *doc.ParseError) {
	root, err := tomledit.Parse(src)
	if err != nil {
		return nil, toParseError(err)
	}
	c := &converter{src: src, lineStarts: newLineStarts(src)}
	rootNode := c.buildRoot(root)
	return doc.NewDocument(doc.FormatTOML, rootNode, src), nil
}

// toParseError converts a go-toml-edit error into a typed doc.ParseError,
// preserving the substrate's line/column/byte-offset when available.
func toParseError(err error) *doc.ParseError {
	var pe *tomledit.ParseError
	if errors.As(err, &pe) {
		return &doc.ParseError{
			Format: doc.FormatTOML,
			Position: doc.Position{
				Line:       pe.Line,
				Column:     pe.Column,
				ByteOffset: pe.Offset,
			},
			Message: pe.Message,
		}
	}
	return &doc.ParseError{Format: doc.FormatTOML, Message: err.Error()}
}

// converter holds the per-parse state needed to translate go-toml-edit spans
// (1-based line/column only) into doc spans (which also carry byte offsets).
type converter struct {
	src        []byte
	lineStarts []int // lineStarts[i] is the byte offset of the start of line (i+1)
}

// newLineStarts builds the byte offset of each line's first byte. Index i holds
// the start offset of line number i+1, so line L (1-based) starts at
// lineStarts[L-1].
func newLineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// pos converts a substrate position to a doc position, computing the 0-based
// byte offset from the 1-based line/column and the line index.
func (c *converter) pos(p tomledit.Position) doc.Position {
	if !p.IsValid() {
		return doc.Position{}
	}
	off := 0
	if idx := p.Line - 1; idx >= 0 && idx < len(c.lineStarts) {
		off = c.lineStarts[idx] + (p.Column - 1)
	}
	return doc.Position{Line: p.Line, Column: p.Column, ByteOffset: off}
}

// span converts a substrate span to a doc span.
func (c *converter) span(s tomledit.Span) doc.Span {
	return doc.Span{Start: c.pos(s.Start), End: c.pos(s.End)}
}

// docEndPos returns the position immediately past the last source byte, used as
// the root record's span end.
func (c *converter) docEndPos() doc.Position {
	n := len(c.src)
	line := len(c.lineStarts)
	col := n - c.lineStarts[line-1] + 1
	return doc.Position{Line: line, Column: col, ByteOffset: n}
}

// buildRoot folds the flat TOML AST (root key/values, [table] and
// [[array-table]] headers, dotted keys, inline tables) into the doc record
// tree, then finalizes it into an immutable doc.Node.
func (c *converter) buildRoot(root *tomledit.DocumentNode) doc.Node {
	rootSpan := doc.Span{Start: doc.Position{Line: 1, Column: 1, ByteOffset: 0}, End: c.docEndPos()}
	b := newBuilder(rootSpan)
	for _, child := range root.Children {
		switch n := child.(type) {
		case *tomledit.KeyValueNode:
			c.addKV(b, n)
		case *tomledit.TableNode:
			hdr := c.span(n.Span())
			t := c.descend(b, n.KeyPath, hdr, hdr)
			if !t.span.IsValid() {
				t.span = hdr
			}
			for _, ch := range n.Children {
				if kv, ok := ch.(*tomledit.KeyValueNode); ok {
					c.addKV(t, kv)
				}
			}
		case *tomledit.ArrayTableNode:
			hdr := c.span(n.Span())
			parentPath := n.KeyPath[:len(n.KeyPath)-1]
			last := n.KeyPath[len(n.KeyPath)-1]
			parent := c.descend(b, parentPath, hdr, hdr)
			s := parent.note(last, hdr)
			entry := newBuilder(hdr)
			s.arr = append(s.arr, entry)
			for _, ch := range n.Children {
				if kv, ok := ch.(*tomledit.KeyValueNode); ok {
					c.addKV(entry, kv)
				}
			}
		}
	}
	return b.finalize()
}

// addKV inserts one key = value pair, expanding a dotted key into intermediate
// implicit records and converting the value (including inline tables and
// arrays) into doc nodes.
func (c *converter) addKV(b *builder, kv *tomledit.KeyValueNode) {
	keySpan := c.span(kv.Key.Span())
	parts := kv.Key.Parts
	b = c.descend(b, parts[:len(parts)-1], keySpan, keySpan)
	key := parts[len(parts)-1]
	s := b.note(key, keySpan)
	s.value = c.convertValue(kv.Val)
}

// descend resolves a key path to the target builder, creating implicit records
// as needed. A segment naming an array-of-tables resolves to its last entry
// (standard TOML sub-table-of-array-entry addressing). keySpan anchors implicit
// keys created along the way; recSpan anchors implicit records.
func (c *converter) descend(b *builder, parts []string, keySpan, recSpan doc.Span) *builder {
	for _, part := range parts {
		s := b.note(part, keySpan)
		if len(s.arr) > 0 {
			b = s.arr[len(s.arr)-1]
			continue
		}
		if s.sub == nil {
			s.sub = newBuilder(recSpan)
		}
		b = s.sub
	}
	return b
}

// convertValue converts a go-toml-edit value node into a doc.Node.
func (c *converter) convertValue(node tomledit.Node) doc.Node {
	sp := c.span(node.Span())
	switch n := node.(type) {
	case *tomledit.StringNode:
		return doc.NewScalar(doc.String, string(n.Raw()), sp)
	case *tomledit.IntegerNode:
		return doc.NewScalar(doc.Integer, string(n.Raw()), sp)
	case *tomledit.FloatNode:
		return doc.NewScalar(doc.Float, string(n.Raw()), sp)
	case *tomledit.BooleanNode:
		return doc.NewScalar(doc.Bool, string(n.Raw()), sp)
	case *tomledit.DateTimeNode:
		return doc.NewScalar(doc.DateTimeOffset, string(n.Raw()), sp)
	case *tomledit.LocalDateTimeNode:
		return doc.NewScalar(doc.DateTimeLocal, string(n.Raw()), sp)
	case *tomledit.LocalDateNode:
		return doc.NewScalar(doc.DateLocal, string(n.Raw()), sp)
	case *tomledit.LocalTimeNode:
		return doc.NewScalar(doc.TimeLocal, string(n.Raw()), sp)
	case *tomledit.ArrayNode:
		items := make([]doc.Node, 0, len(n.Elements))
		for _, el := range n.Elements {
			items = append(items, c.convertValue(el))
		}
		return doc.NewArray(items, sp)
	case *tomledit.InlineTableNode:
		ib := newBuilder(sp)
		for _, ch := range n.Children {
			if kv, ok := ch.(*tomledit.KeyValueNode); ok {
				c.addKV(ib, kv)
			}
		}
		return ib.finalize()
	default:
		// Unreachable for valid TOML: every value node type is handled above.
		return doc.NewScalar(doc.String, string(node.Raw()), sp)
	}
}
