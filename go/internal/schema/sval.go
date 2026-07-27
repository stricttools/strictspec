package schema

import (
	"strconv"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// SVal is a schema-authored literal value (an enum member, a `literal` value, a
// numeric/datetime bound, or a condition operand). It captures the value's
// lexeme class and its decoded content so the interpreter can compare it against
// document values by class-and-value and render it per appendix-rendering A.1.
type SVal struct {
	Kind   doc.Kind // doc.String / Integer / Float / Bool / DateTime* etc.
	Lexeme string   // raw source lexeme (retained)
	Str    string   // decoded string content (String kind)
	Int    int64    // parsed value (Integer)
	IsInt  bool
	Float  float64 // parsed value (Float)
	Bool   bool
}

// svalFromNode builds an SVal from a schema-document scalar node. Schemas are
// always TOML, so string lexemes decode with the TOML decoder.
func svalFromNode(n doc.Node) SVal {
	if n == nil {
		return SVal{}
	}
	s := SVal{Kind: n.Kind(), Lexeme: n.Lexeme()}
	switch n.Kind() {
	case doc.String:
		s.Str = strdecode.TOML(n.Lexeme())
	case doc.Integer:
		if v, err := strconv.ParseInt(n.Lexeme(), 10, 64); err == nil {
			s.Int = v
			s.IsInt = true
		}
	case doc.Float:
		if v, err := strconv.ParseFloat(n.Lexeme(), 64); err == nil {
			s.Float = v
		}
	case doc.Bool:
		s.Bool = n.Lexeme() == "true"
	}
	return s
}
