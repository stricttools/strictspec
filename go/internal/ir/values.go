package ir

import (
	"math"
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// nodeKindName renders a document node's lexeme-class name for a `{got}`/`{expected}`
// string slot in a type-mismatch diagnostic (lowercase, per the catalogue prose).
func nodeKindName(k doc.Kind) string {
	switch k {
	case doc.Record:
		return "record"
	case doc.Array:
		return "array"
	case doc.String:
		return "string"
	case doc.Integer:
		return "integer"
	case doc.Float:
		return "float"
	case doc.Bool:
		return "boolean"
	case doc.Null:
		return "null"
	case doc.DateTimeOffset, doc.DateTimeLocal:
		return "datetime"
	case doc.DateLocal:
		return "date"
	case doc.TimeLocal:
		return "time"
	default:
		return "value"
	}
}

// nodeCategory groups a node kind into scalar/record/array for node-kind-union
// dispatch.
func nodeCategory(k doc.Kind) string {
	switch k {
	case doc.Record:
		return "record"
	case doc.Array:
		return "array"
	default:
		return "scalar"
	}
}

// decodeString decodes a string scalar's retained lexeme per the document format.
func (v *exec) decodeString(n doc.Node) string {
	if n == nil {
		return ""
	}
	if v.format == doc.FormatTOML {
		return strdecode.TOML(n.Lexeme())
	}
	return strdecode.JSON(n.Lexeme())
}

// valueOf builds a diag.Value for a document node (a `value`-typed slot), per the
// A.1 rendering table.
func (v *exec) valueOf(n doc.Node) diag.Value {
	if n == nil {
		return diag.NullVal{}
	}
	switch n.Kind() {
	case doc.String:
		return diag.StringVal(v.decodeString(n))
	case doc.Integer:
		return diag.NumberVal{Lexeme: n.Lexeme(), IntClass: true}
	case doc.Float:
		return diag.FloatVal{Lexeme: n.Lexeme(), HasLexeme: true}
	case doc.Bool:
		return diag.BoolVal(n.Lexeme() == "true")
	case doc.Null:
		return diag.NullVal{}
	case doc.DateLocal:
		return diag.DateVal(v.decodeString(n))
	case doc.TimeLocal:
		return diag.TimeVal(v.decodeString(n))
	case doc.DateTimeOffset, doc.DateTimeLocal:
		return diag.DatetimeVal(v.decodeString(n))
	default:
		return diag.StringVal(n.Lexeme())
	}
}

// svalToValue builds a diag.Value for a schema-authored literal (an enum member,
// a bound, a `literal` value).
func svalToValue(sv schema.SVal) diag.Value {
	switch sv.Kind {
	case doc.String:
		return diag.StringVal(sv.Str)
	case doc.Integer:
		return diag.NumberVal{Lexeme: sv.Lexeme, IntClass: true}
	case doc.Float:
		return diag.FloatVal{Lexeme: sv.Lexeme, HasLexeme: true}
	case doc.Bool:
		return diag.BoolVal(sv.Bool)
	default:
		return diag.StringVal(sv.Str)
	}
}

// numOf parses a numeric node's value as float64.
func numOf(n doc.Node) (float64, bool) {
	if n == nil {
		return 0, false
	}
	switch n.Kind() {
	case doc.Integer, doc.Float:
		f, err := strconv.ParseFloat(n.Lexeme(), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// svalNum returns a schema literal's numeric value.
func svalNum(sv schema.SVal) float64 {
	if sv.Kind == doc.Integer {
		return float64(sv.Int)
	}
	return sv.Float
}

// exactlyRepresentable reports whether an integer lexeme's exact value survives a
// round-trip through float64 (the `number` scalar's no-silent-precision-loss rule).
func exactlyRepresentable(lexeme string) bool {
	i, err := strconv.ParseInt(lexeme, 10, 64)
	if err != nil {
		// Beyond int64: definitely not exactly float64-representable in general.
		return false
	}
	f := float64(i)
	if math.Abs(f) >= (1 << 53) {
		// Above 2^53 the round-trip must be exact bit-for-bit.
		return int64(f) == i
	}
	return true
}

// entryOf returns a record's child value by key.
func entryOf(rec doc.Node, key string) (doc.Node, bool) {
	if rec == nil || rec.Kind() != doc.Record {
		return nil, false
	}
	for _, e := range rec.Entries() {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

// hasKey reports whether a record contains a key.
func hasKey(rec doc.Node, key string) bool {
	_, ok := entryOf(rec, key)
	return ok
}
