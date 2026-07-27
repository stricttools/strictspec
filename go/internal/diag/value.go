package diag

// Value is a rendered document value: the payload of a `value`-typed slot
// (appendix-error-codes.md §2) and of `list<T>` elements. It is a small tagged
// union covering the document value kinds the A.1 diagnostic-rendering table
// enumerates. Rendering lives in the render package; these types carry only the
// data needed to render per A.1 / A.3 / A.5.
type Value interface{ isValue() }

// IntVal is an int64 integer. Renders as decimal digits with an optional
// leading '-' (A.1). Note: int64 has no negative zero, so -0 renders "0".
type IntVal int64

// FloatVal is a float value. When HasLexeme is true it renders from its source
// Lexeme UNCHANGED (A.1: exponent form preserved, e.g. "1e3"). When HasLexeme is
// false it is a constructed/lexeme-less float and renders per the canonical
// shortest-round-trip rule (A.3), always float-marked (e.g. float64(5) -> "5.0",
// -0.0 -> "-0.0").
type FloatVal struct {
	F         float64
	Lexeme    string
	HasLexeme bool
}

// NumberVal is a `number`-scalar value, rendered per its source lexeme class
// (A.1): an integer-classed source renders as an integer, a float-classed source
// renders float-marked. The Lexeme is the exact source text and IntClass records
// its lexeme class.
type NumberVal struct {
	Lexeme   string
	IntClass bool
}

// StringVal is a string. Renders double-quoted with A.2 escaping and A.4
// truncation (A.1).
type StringVal string

// BoolVal renders lowercase "true"/"false" (A.1).
type BoolVal bool

// NullVal renders lowercase "null" (A.1).
type NullVal struct{}

// DateVal, TimeVal, and DatetimeVal carry the already-formatted RFC 3339 text
// (offset forms preserve the source offset verbatim, A.1). The renderer emits
// them unchanged.
type DateVal string
type TimeVal string
type DatetimeVal string

// ArrayVal is a container value; renders as the A.5 truncated inline form
// [e1, e2, e3, ...] (at most 3 elements, nesting to at most 2 levels).
type ArrayVal []Value

// RecordVal is a container value in document order; renders as the A.5 truncated
// inline form {k1: v1, k2: v2, k3: v3, ...} (keys bare when identifier-shaped,
// else quoted per A.2; at most 3 pairs).
type RecordVal struct {
	Keys []string
	Vals []Value
}

func (IntVal) isValue()      {}
func (FloatVal) isValue()    {}
func (NumberVal) isValue()   {}
func (StringVal) isValue()   {}
func (BoolVal) isValue()     {}
func (NullVal) isValue()     {}
func (DateVal) isValue()     {}
func (TimeVal) isValue()     {}
func (DatetimeVal) isValue() {}
func (ArrayVal) isValue()    {}
func (RecordVal) isValue()   {}
