package doc

import "fmt"

// Kind is the lexeme class of a document-model node. It is FORMAT-NEUTRAL and
// LEXICAL: it records how the value was written (integer vs float lexeme class,
// which datetime flavor), never a schema-level interpretation.
//
// Per the constitution (spec/DESIGN.md, primitives appendix item 4): the NUMBER
// scalar is a SCHEMA-level concept layered over the Integer and Float lexeme
// classes. The document model records the lexeme class and nothing more — there
// is no Number kind. A schema decides, later, whether a field typed `number`
// accepts an Integer- or Float-classed node.
type Kind int

const (
	Record         Kind = iota // an ordered map of string keys to nodes
	Array                      // an ordered sequence of nodes
	String                     // a string scalar
	Integer                    // an integer-classed numeric lexeme (no '.', 'e', or 'E')
	Float                      // a float-classed numeric lexeme, incl. inf/nan/-0.0
	Bool                       // a boolean scalar
	Null                       // the JSON/JSONL null value; TOML has no null, so this Kind never appears in a TOML document
	DateTimeOffset             // an offset date-time (RFC 3339 with offset)
	DateTimeLocal              // a local date-time (no offset)
	DateLocal                  // a local date
	TimeLocal                  // a local time
)

var kindNames = [...]string{
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

// String returns the kind's canonical name.
func (k Kind) String() string {
	if int(k) >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// isScalar reports whether k is a scalar (non-container) kind.
func (k Kind) isScalar() bool { return k != Record && k != Array }

// Entry is one ordered key/value binding inside a Record. Key is the DECODED
// (unquoted, code-point) key string; KeySpan points at the key's source
// location. Value is the bound node.
type Entry struct {
	Key     string
	KeySpan Span
	Value   Node
}

// Node is the format-neutral, tagged, lexeme-retaining document-model value the
// whole toolchain consumes. One model, three syntaxes (TOML, JSON, JSONL): a
// backend's parser is the only thing that constructs Nodes, and every backend
// constructs the SAME Node contract via the NewScalar/NewRecord/NewArray
// constructors below.
//
// The model is READ-FOCUSED and IMMUTABLE by convention: there is deliberately
// NO mutation API here. Byte-stable edits arrive later, through the migration
// engine, which rewrites only the touched subtrees and re-renders from retained
// lexemes; consumers of this package treat Nodes as read-only.
//
// DUPLICATE KEYS are never silently merged by this model. Records preserve
// whatever the backend's parser produced, and each backend decides at PARSE
// time how duplicates are handled, per the constitution:
//   - TOML: a duplicate key is a parse error raised by the substrate
//     (go-toml-edit), so a duplicate never reaches a Record.
//   - JSON/JSONL: a duplicate key is a canonical hard error the JSON backend
//     raises at parse time (it must read via ordered pairs to even see them).
//
// If a Record ever contained two Entries with the same Key, that would be a
// backend bug — this model neither dedups nor last-wins-merges.
type Node interface {
	// Kind returns the node's lexeme class.
	Kind() Kind

	// Lexeme returns the EXACT source bytes of a scalar value (e.g. "1_000",
	// "1e3", `"""multi"""`, "2026-07-27"). For containers (Record, Array) it
	// returns the empty string.
	Lexeme() string

	// Span returns the node's source range.
	Span() Span

	// Entries returns the ordered key/value bindings of a Record, in document
	// order. It returns nil for every non-Record node.
	Entries() []Entry

	// Items returns the ordered elements of an Array. It returns nil for every
	// non-Array node.
	Items() []Node
}

// scalarNode is the immutable implementation of a scalar Node.
type scalarNode struct {
	kind   Kind
	lexeme string
	span   Span
}

func (n *scalarNode) Kind() Kind       { return n.kind }
func (n *scalarNode) Lexeme() string   { return n.lexeme }
func (n *scalarNode) Span() Span       { return n.span }
func (n *scalarNode) Entries() []Entry { return nil }
func (n *scalarNode) Items() []Node    { return nil }

// recordNode is the immutable implementation of a Record Node.
type recordNode struct {
	entries []Entry
	span    Span
}

func (n *recordNode) Kind() Kind       { return Record }
func (n *recordNode) Lexeme() string   { return "" }
func (n *recordNode) Span() Span       { return n.span }
func (n *recordNode) Entries() []Entry { return n.entries }
func (n *recordNode) Items() []Node    { return nil }

// arrayNode is the immutable implementation of an Array Node.
type arrayNode struct {
	items []Node
	span  Span
}

func (n *arrayNode) Kind() Kind       { return Array }
func (n *arrayNode) Lexeme() string   { return "" }
func (n *arrayNode) Span() Span       { return n.span }
func (n *arrayNode) Entries() []Entry { return nil }
func (n *arrayNode) Items() []Node    { return n.items }

// NewScalar builds a scalar Node with the given lexeme class, exact source
// lexeme, and span. It panics if kind is a container kind (Record or Array):
// that is a backend programming error, not a runtime condition.
func NewScalar(kind Kind, lexeme string, span Span) Node {
	if !kind.isScalar() {
		panic(fmt.Sprintf("doc.NewScalar: %s is not a scalar kind", kind))
	}
	return &scalarNode{kind: kind, lexeme: lexeme, span: span}
}

// NewRecord builds a Record Node from ordered entries. The entries slice is
// retained as-is (ownership transfers to the node); callers must not mutate it
// afterward.
func NewRecord(entries []Entry, span Span) Node {
	return &recordNode{entries: entries, span: span}
}

// NewArray builds an Array Node from ordered items. The items slice is retained
// as-is (ownership transfers to the node); callers must not mutate it afterward.
func NewArray(items []Node, span Span) Node {
	return &arrayNode{items: items, span: span}
}
