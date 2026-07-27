package tomldoc

import "github.com/smm-h/strictspec/go/internal/doc"

// builder is the mutable intermediate record used while folding TOML's flat AST
// (root key/values, table headers, array-of-tables headers, dotted keys) into a
// nested record tree. It preserves first-appearance key order and is finalized
// into an immutable doc.Node.
type builder struct {
	keys  []string         // keys in first-appearance order
	byKey map[string]*slot // key -> slot
	span  doc.Span         // the record's own anchoring span
}

// slot holds exactly one of: a finished value node (leaf key/value, including
// inline-table records and arrays), a sub-record builder (implicit or explicit
// [table] / dotted-key table), or a list of array-of-tables entry builders.
type slot struct {
	keySpan doc.Span
	value   doc.Node   // leaf value (scalar, array, or inline-table record)
	sub     *builder   // sub-record from a [table] header or dotted key
	arr     []*builder // array-of-tables entries
}

func newBuilder(span doc.Span) *builder {
	return &builder{byKey: map[string]*slot{}, span: span}
}

// note returns the slot for key, creating it (and recording its order and key
// span) on first appearance.
func (b *builder) note(key string, keySpan doc.Span) *slot {
	if s, ok := b.byKey[key]; ok {
		return s
	}
	s := &slot{keySpan: keySpan}
	b.byKey[key] = s
	b.keys = append(b.keys, key)
	return s
}

// finalize converts the builder tree into an immutable doc.Node (a Record),
// recursing into sub-records and array-of-tables entries.
func (b *builder) finalize() doc.Node {
	entries := make([]doc.Entry, 0, len(b.keys))
	for _, k := range b.keys {
		s := b.byKey[k]
		var v doc.Node
		switch {
		case s.value != nil:
			v = s.value
		case len(s.arr) > 0:
			items := make([]doc.Node, len(s.arr))
			for i, e := range s.arr {
				items[i] = e.finalize()
			}
			v = doc.NewArray(items, arrayTableSpan(s.arr))
		case s.sub != nil:
			v = s.sub.finalize()
		default:
			// A noted key with no value/sub/arr cannot occur: every note is
			// followed by exactly one assignment. Guard against a nil entry.
			v = doc.NewRecord(nil, s.keySpan)
		}
		entries = append(entries, doc.Entry{Key: k, KeySpan: s.keySpan, Value: v})
	}
	return doc.NewRecord(entries, b.span)
}

// arrayTableSpan returns a span covering all array-of-tables entries, from the
// first entry's start to the last entry's end. There is no single bracket span
// for an [[array-of-tables]] collection, so this is the best available source
// anchor.
func arrayTableSpan(entries []*builder) doc.Span {
	if len(entries) == 0 {
		return doc.Span{}
	}
	return doc.Span{Start: entries[0].span.Start, End: entries[len(entries)-1].span.End}
}
