// Package schema is the strictspec meta-schema reader: it parses a schema file
// (or a type-definition file) authored in the pinned TOML surface
// (spec/appendix-surface-syntax.md) into a typed Schema model, and emits the
// catalogued STRICTSPEC_SCHEMA_*/STRICTSPEC_IMPORT_* authoring diagnostics that
// meta-schema validation requires. The reference interpreter (internal/interp)
// consumes the typed Schema to validate documents.
//
// Type-reference resolution (is a `type = "Foo"` a builtin scalar, a registered
// custom scalar, or a named/imported type?) is DEFERRED to the interpreter,
// which resolves against builtins + named types + the manifest's custom scalars.
// The reader records reference names verbatim; it never emits a dangling-ref
// diagnostic, so it can load a schema without its manifest (the examples/ sweep
// relies on this).
package schema

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
)

// Kind is the category of a type site (appendix-surface-syntax.md §3).
type Kind int

const (
	// KindRef is a reference: a builtin scalar, a registered custom scalar, or a
	// declared/imported named type. Ref holds the name; resolution is deferred.
	KindRef Kind = iota
	KindRecord
	KindMap
	KindArray
	KindTuple
	KindEnum
	KindLiteral
	KindDiscriminatedUnion
	KindNodeKindUnion
	KindNullable
	KindOpaque
)

// Type is one type site (a field, an array item, a map value, a union arm, a
// tuple element by reference, a named type, or the root).
type Type struct {
	Kind Kind
	Ref  string // KindRef: the referenced builtin/custom/named name

	// Scalar refinements (KindRef scalar sites and scalar-refinement named types).
	Min          *SVal
	Max          *SVal
	ExclusiveMin *SVal
	ExclusiveMax *SVal
	MinLength    *int
	MaxLength    *int
	NonEmpty     bool
	Regex        string
	HasRegex     bool
	DatetimeKind string // "offset" | "local"

	// Record / discriminated fields (ordered).
	Fields []*Field

	// Map.
	KeyPattern string
	Order      string
	Value      *Type

	// Array.
	MinLen *int
	MaxLen *int
	Item   *Type

	// Tuple.
	Elements []string

	// Enum.
	EnumValues []SVal   // inline `values = [...]`
	Sourced    bool     // sourced-from-document enum
	Baked      []string // baked arms (the accepted set)
	SourceDoc  string
	SourceSel  string

	// Literal.
	Literal SVal

	// Union.
	Discriminator string
	Arms          []*Arm

	// Nullable.
	Inner *Type

	// Opaque.
	ConsumerCheck    string
	HasConsumerCheck bool
	Unchecked        bool
	HasUnchecked     bool
	UncheckedReason  string
	HasReason        bool

	// Constraints attached at this record/type scope.
	Constraints []*Constraint

	// SchemaPath is this site's location WITHIN THE SCHEMA document (for
	// meta-schema authoring diagnostics), e.g. $.types.ToolSchema.fields.x.
	SchemaPath diag.Path
}

// Field is one record field (a named type site) with presence and aliases.
type Field struct {
	Name     string
	Type     *Type
	Required bool
	Aliases  []string
}

// Arm is one union arm: an arm identifier (the key) and its type site.
type Arm struct {
	Name string
	Type *Type
}

// Constraint is one phase-2 vocabulary form attached at a record/type scope.
type Constraint struct {
	Form string

	// Gated forms.
	Field string
	When  *Condition

	// conditional-value.
	EqualsLiteral SVal
	HasEquals     bool

	// field-set forms.
	Fields []string

	// collections-disjoint.
	Left, Right string

	// unique-by / pairwise-distinct / ranges-disjoint.
	Collection    string
	UniqField     string
	Normalization string
	Start, Length string

	// ordered-pair.
	Less, Than string

	// references.
	Reference    string
	ResolvesInto string
	ResolvesBy   string

	// cross-document.
	Source    string
	Selection string
	Compare   string
	Limit     SVal
	HasLimit  bool
	SumField  string
}

// Condition is a gate condition object (closed six-kind set, §5.2).
type Condition struct {
	Field     string
	Predicate string // present|absent|equals|not-equals|in|not-in
	Value     SVal
	HasValue  bool
	Values    []SVal
}

// Import is one entry of the top-level `imports` array.
type Import struct {
	File  string
	Types []string
}

// Scalar is one custom-scalar registration from the manifest (appendix-custom-scalars.md).
type Scalar struct {
	Name       string
	Base       string
	LexemeRule string
	LenMin     *int
	LenMax     *int
	NonEmpty   bool
}

// Schema is a parsed schema or type-definition file.
type Schema struct {
	Name              string
	MetaVersion       int64
	HasMetaVersion    bool
	MetaVersionKind   doc.Kind
	FormatVersion     int64
	HasFormatVersion  bool
	FormatVersionKind doc.Kind
	DocumentSyntax    string
	Role              string
	Description       string
	Root              string
	Targets           []string
	SafeIntegers      bool
	Imports           []Import
	Types             map[string]*Type
	TypeOrder         []string
	Dir               string // directory of the schema file (for import resolution)
}

// LookupType returns a named type by name.
func (s *Schema) LookupType(name string) (*Type, bool) {
	t, ok := s.Types[name]
	return t, ok
}
