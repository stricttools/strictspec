package strictspec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// Kind is the lexeme class of a tagged document value (the public projection of
// the document model's Kind). It records how a value was WRITTEN — integer vs
// float lexeme class, datetime flavor — never a schema-level interpretation.
type Kind int

const (
	KindRecord Kind = iota
	KindArray
	KindString
	KindInteger
	KindFloat
	KindBool
	KindNull
	KindDatetime
	KindDate
	KindTime
)

// Value is a TAGGED, lexeme-retaining document value: the second entry point's
// input and the source generated typed constructors bind from. It comes from a
// lossless parse (LoadValue) or from generated typed constructors. Raw untagged
// Go maps are never Values — ambiguity never enters the model.
type Value struct {
	node   doc.Node
	format doc.Format
}

// KV is one ordered key/value binding of a semantic-order map or record.
// Semantic-order maps bind to []KV (go/DESIGN.md bindings), preserving order.
type KV struct {
	Key   string
	Value Value
}

// LoadValue losslessly parses raw bytes in the given syntax ("json" | "toml" |
// "jsonl") into a tagged Value. For "jsonl" it parses the FIRST line (use
// LoadValues for a stream). A parse failure is an error — there is no lenient
// mode.
func LoadValue(input []byte, syntax string) (Value, error) {
	switch syntax {
	case "toml":
		d, perr := tomldoc.Parse(input)
		if perr != nil {
			return Value{}, perr
		}
		return Value{node: d.Root, format: doc.FormatTOML}, nil
	case "jsonl":
		docs, perr := jsondoc.ParseLines(input)
		if perr != nil {
			return Value{}, perr
		}
		if len(docs) == 0 {
			return Value{}, fmt.Errorf("strictspec: empty JSONL stream")
		}
		return Value{node: docs[0].Root, format: doc.FormatJSONL}, nil
	default:
		d, perr := jsondoc.Parse(input)
		if perr != nil {
			return Value{}, perr
		}
		return Value{node: d.Root, format: doc.FormatJSON}, nil
	}
}

// LoadValues losslessly parses a JSONL stream into one Value per line.
func LoadValues(input []byte) ([]Value, error) {
	docs, perr := jsondoc.ParseLines(input)
	if perr != nil {
		return nil, perr
	}
	out := make([]Value, len(docs))
	for i, d := range docs {
		out[i] = Value{node: d.Root, format: doc.FormatJSONL}
	}
	return out, nil
}

// Kind returns the value's lexeme class.
func (v Value) Kind() Kind {
	if v.node == nil {
		return KindNull
	}
	switch v.node.Kind() {
	case doc.Record:
		return KindRecord
	case doc.Array:
		return KindArray
	case doc.String:
		return KindString
	case doc.Integer:
		return KindInteger
	case doc.Float:
		return KindFloat
	case doc.Bool:
		return KindBool
	case doc.Null:
		return KindNull
	case doc.DateTimeOffset, doc.DateTimeLocal:
		return KindDatetime
	case doc.DateLocal:
		return KindDate
	case doc.TimeLocal:
		return KindTime
	default:
		return KindNull
	}
}

// Field returns a record field's value by key (present reports absence). Only
// meaningful for record-kinded values.
func (v Value) Field(name string) (Value, bool) {
	if v.node == nil || v.node.Kind() != doc.Record {
		return Value{}, false
	}
	for _, e := range v.node.Entries() {
		if e.Key == name {
			return Value{node: e.Value, format: v.format}, true
		}
	}
	return Value{}, false
}

// Entries returns a record's / map's ordered key/value bindings ([]KV binding).
func (v Value) Entries() []KV {
	if v.node == nil || v.node.Kind() != doc.Record {
		return nil
	}
	es := v.node.Entries()
	out := make([]KV, len(es))
	for i, e := range es {
		out[i] = KV{Key: e.Key, Value: Value{node: e.Value, format: v.format}}
	}
	return out
}

// Items returns an array's ordered elements.
func (v Value) Items() []Value {
	if v.node == nil || v.node.Kind() != doc.Array {
		return nil
	}
	its := v.node.Items()
	out := make([]Value, len(its))
	for i, it := range its {
		out[i] = Value{node: it, format: v.format}
	}
	return out
}

// --- Coercers (node lift, extended per the scalar rules) --------------------
//
// Coercers extract a Go value from a validated tagged value. They are the
// binding half of the Generated API contract: `number` -> float64, datetimes ->
// their pinned string form (offset preserved verbatim), integer/float distinct.
// They report ok=false on a kind mismatch (a generated binder only calls a
// coercer after phase-1 validation has established the kind, so ok is true in
// generated code; the boolean lets standalone consumers guard).

// AsString returns the decoded (code-point) value of a string scalar. (Named
// AsString, not String, so it does not collide with the fmt.Stringer convention
// — a two-return-value String() would make Value satisfy no interface yet shadow
// the expected single-string Stringer signature, confusing fmt and readers.)
func (v Value) AsString() (string, bool) {
	if v.node == nil || v.node.Kind() != doc.String {
		return "", false
	}
	return v.decodeString(), true
}

// Int returns the int64 value of an integer-classed scalar (a float lexeme is
// NOT an integer).
func (v Value) Int() (int64, bool) {
	if v.node == nil || v.node.Kind() != doc.Integer {
		return 0, false
	}
	n, err := strconv.ParseInt(v.node.Lexeme(), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Float returns the float64 value of a float-classed scalar.
func (v Value) Float() (float64, bool) {
	if v.node == nil || v.node.Kind() != doc.Float {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.node.Lexeme(), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Number returns the float64 value of a `number`-scalar site (accepts either
// lexeme class; the schema's phase-1 check has already rejected lexemes float64
// cannot represent exactly).
func (v Value) Number() (float64, bool) {
	if v.node == nil {
		return 0, false
	}
	switch v.node.Kind() {
	case doc.Integer, doc.Float:
		f, err := strconv.ParseFloat(v.node.Lexeme(), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// Bool returns the boolean value of a boolean scalar (a boolean is NOT an
// integer).
func (v Value) Bool() (bool, bool) {
	if v.node == nil || v.node.Kind() != doc.Bool {
		return false, false
	}
	return v.node.Lexeme() == "true", true
}

// Datetime returns the RFC 3339 text of a datetime/date/time scalar with its
// source lexeme preserved (offset forms keep the source offset verbatim; no
// normalization). ok is true for any of the three datetime kinds.
func (v Value) Datetime() (string, bool) {
	if v.node == nil {
		return "", false
	}
	switch v.node.Kind() {
	case doc.DateTimeOffset, doc.DateTimeLocal, doc.DateLocal, doc.TimeLocal:
		return v.node.Lexeme(), true
	case doc.String:
		// JSON datetimes are RFC 3339 strings.
		return v.decodeString(), true
	default:
		return "", false
	}
}

// IsNull reports whether the value is JSON/JSONL null.
func (v Value) IsNull() bool {
	return v.node == nil || v.node.Kind() == doc.Null
}

func (v Value) decodeString() string {
	if v.node == nil {
		return ""
	}
	if v.format == doc.FormatTOML {
		return strdecode.TOML(v.node.Lexeme())
	}
	return strdecode.JSON(v.node.Lexeme())
}

// embeddedSchemaError composes a compile-time error listing the authoring
// diagnostics of an embedded schema (a programmer/gen error, since gen rejects
// unemittable schemas).
func embeddedSchemaError(diags []diag.Diagnostic) error {
	var b strings.Builder
	b.WriteString("strictspec: embedded schema failed meta-schema validation (regenerate):")
	for _, d := range diags {
		b.WriteString("\n  ")
		b.WriteString(d.Code)
		b.WriteString(" at ")
		b.WriteString(d.Path.Render())
	}
	return fmt.Errorf("%s", b.String())
}
