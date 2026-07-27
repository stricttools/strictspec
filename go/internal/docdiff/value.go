package docdiff

import (
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// NodeValue converts a document node into a diag.Value for A.1 rendering
// (appendix-rendering.md Part A). Numbers keep their SOURCE lexeme so the
// rendered form is lexeme-class-aware: an integer lexeme renders as an integer,
// a float lexeme renders float-marked from its lexeme (1 vs 1.0 is a change).
func NodeValue(n doc.Node, format doc.Format) diag.Value {
	if n == nil {
		return diag.NullVal{}
	}
	switch n.Kind() {
	case doc.Integer:
		v, err := strconv.ParseInt(n.Lexeme(), 10, 64)
		if err != nil {
			return diag.StringVal(n.Lexeme())
		}
		return diag.IntVal(v)
	case doc.Float:
		f, _ := strconv.ParseFloat(n.Lexeme(), 64)
		return diag.FloatVal{F: f, Lexeme: n.Lexeme(), HasLexeme: true}
	case doc.String:
		if format == doc.FormatTOML {
			return diag.StringVal(strdecode.TOML(n.Lexeme()))
		}
		return diag.StringVal(strdecode.JSON(n.Lexeme()))
	case doc.Bool:
		return diag.BoolVal(n.Lexeme() == "true")
	case doc.Null:
		return diag.NullVal{}
	case doc.DateTimeOffset, doc.DateTimeLocal:
		return diag.DatetimeVal(n.Lexeme())
	case doc.DateLocal:
		return diag.DateVal(n.Lexeme())
	case doc.TimeLocal:
		return diag.TimeVal(n.Lexeme())
	case doc.Array:
		items := n.Items()
		elems := make([]diag.Value, len(items))
		for i, it := range items {
			elems[i] = NodeValue(it, format)
		}
		return diag.ArrayVal(elems)
	case doc.Record:
		ents := n.Entries()
		keys := make([]string, len(ents))
		vals := make([]diag.Value, len(ents))
		for i, e := range ents {
			keys[i] = e.Key
			vals[i] = NodeValue(e.Value, format)
		}
		return diag.RecordVal{Keys: keys, Vals: vals}
	default:
		return diag.StringVal(n.Lexeme())
	}
}
