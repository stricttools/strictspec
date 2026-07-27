// Package write is the strictspec CANONICAL SERIALIZATION path (write side)
// pinned in spec/DESIGN.md (Canonical serialization appendix) and
// spec/appendix-rendering.md A.6. It has three jobs:
//
//  1. RenderConstructed — render a CONSTRUCTED or type-coerced value (produced by
//     a migration op injecting a literal) into a target-format lexeme per the A.6
//     constructed-value table: a float always float-marked (float64(5) -> "5.0"),
//     integers bare, strings A.2-escaped, booleans lowercase, null only for
//     JSON/JSONL, datetimes RFC 3339.
//
//  2. Doc — a byte-splicing editor over an ORIGINAL source document: untouched
//     values serialize BYTE-IDENTICALLY (lexeme retention via splicing over the
//     original bytes); only the exact edited spans are rewritten. This is what
//     the migration engine writes through.
//
//  3. Serialize — the PRODUCER-CURRENT-ONLY guard: the write path hard-errors
//     (STRICTSPEC_SERIALIZE_NONCURRENT) when asked to serialize a document at any
//     format_version other than the schema's current one.
//
// Within-backend fidelity: the Go, Python, and TS backends emit different bytes
// for the same migrated document; each backend is self-consistent (a read-then-
// write fixpoint). This package is the Go backend.
package write

import (
	"fmt"
	"math"
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// RenderConstructed renders a constructed value node into a target-format lexeme
// per appendix-rendering.md A.6. The node comes from a migration file's literal
// (parsed as TOML) and is being INJECTED into a document of format `format`; it
// is a constructed value (it did not exist in the source), so it renders per the
// constructed-value table, NOT by lexeme retention.
func RenderConstructed(n doc.Node, format doc.Format) ([]byte, error) {
	var b []byte
	if err := renderInto(&b, n, format, true); err != nil {
		return nil, err
	}
	return b, nil
}

// renderInto appends the rendering of n to *out. fromTOML reports whether n came
// from a TOML source (migration literals always do), which decides how a string
// lexeme is decoded before re-escaping.
func renderInto(out *[]byte, n doc.Node, format doc.Format, fromTOML bool) error {
	switch n.Kind() {
	case doc.Integer:
		*out = append(*out, n.Lexeme()...)
	case doc.Float:
		f, err := strconv.ParseFloat(n.Lexeme(), 64)
		if err != nil {
			return fmt.Errorf("write: bad float lexeme %q", n.Lexeme())
		}
		*out = append(*out, RenderFloat(f)...)
	case doc.String:
		var decoded string
		if fromTOML {
			decoded = strdecode.TOML(n.Lexeme())
		} else {
			decoded = strdecode.JSON(n.Lexeme())
		}
		*out = append(*out, '"')
		*out = append(*out, diag.EscapeString(decoded)...)
		*out = append(*out, '"')
	case doc.Bool:
		*out = append(*out, n.Lexeme()...)
	case doc.Null:
		if format == doc.FormatTOML {
			return fmt.Errorf("write: TOML has no null value")
		}
		*out = append(*out, "null"...)
	case doc.DateTimeOffset, doc.DateTimeLocal, doc.DateLocal, doc.TimeLocal:
		// Constructed datetimes render RFC 3339 with the declared kind. The TOML
		// datetime lexeme is already RFC 3339; JSON carries it as a string.
		if format == doc.FormatTOML {
			*out = append(*out, n.Lexeme()...)
		} else {
			*out = append(*out, '"')
			*out = append(*out, n.Lexeme()...)
			*out = append(*out, '"')
		}
	case doc.Array:
		if err := renderArray(out, n, format, fromTOML); err != nil {
			return err
		}
	case doc.Record:
		if err := renderRecord(out, n, format, fromTOML); err != nil {
			return err
		}
	default:
		return fmt.Errorf("write: cannot render kind %s", n.Kind())
	}
	return nil
}

func renderArray(out *[]byte, n doc.Node, format doc.Format, fromTOML bool) error {
	*out = append(*out, '[')
	for i, it := range n.Items() {
		if i > 0 {
			*out = append(*out, ", "...)
		}
		if err := renderInto(out, it, format, fromTOML); err != nil {
			return err
		}
	}
	*out = append(*out, ']')
	return nil
}

func renderRecord(out *[]byte, n doc.Node, format doc.Format, fromTOML bool) error {
	if format == doc.FormatTOML {
		*out = append(*out, "{ "...)
		for i, e := range n.Entries() {
			if i > 0 {
				*out = append(*out, ", "...)
			}
			*out = append(*out, e.Key...)
			*out = append(*out, " = "...)
			if err := renderInto(out, e.Value, format, fromTOML); err != nil {
				return err
			}
		}
		*out = append(*out, " }"...)
		return nil
	}
	*out = append(*out, '{')
	for i, e := range n.Entries() {
		if i > 0 {
			*out = append(*out, ", "...)
		}
		*out = append(*out, '"')
		*out = append(*out, diag.EscapeString(e.Key)...)
		*out = append(*out, '"', ':', ' ')
		if err := renderInto(out, e.Value, format, fromTOML); err != nil {
			return err
		}
	}
	*out = append(*out, '}')
	return nil
}

// RenderFloat renders a constructed float per appendix-rendering.md A.3: the
// shortest decimal string that round-trips to the same float64, ALWAYS
// float-marked (a value with no fractional part gains ".0"; exponent forms use a
// lowercase 'e' with a signed exponent). float64(5) -> "5.0"; -0.0 -> "-0.0".
func RenderFloat(f float64) string {
	// Non-finite values never reach a valid document (rejected on read); guard.
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	// Go's 'g' already uses lowercase 'e' and a signed exponent. Float-mark when
	// the shortest form is a bare integer (no '.' and no 'e').
	hasPoint := false
	hasExp := false
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			hasPoint = true
		}
		if s[i] == 'e' || s[i] == 'E' {
			hasExp = true
		}
	}
	if !hasPoint && !hasExp {
		s += ".0"
	}
	return s
}
