package ir

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// compiledRegex wraps a compiled RE2 pattern (Go's regexp is RE2).
type compiledRegex = regexp.Regexp

var regexCache = map[string]*regexp.Regexp{}

func (v *exec) compile(pattern string) *compiledRegex {
	if re, ok := regexCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = regexp.MustCompile(`\A\z`) // never matches; a bad schema regex fails closed
	}
	regexCache[pattern] = re
	return re
}

// walkScalar validates a builtin or custom-scalar reference site.
func (v *exec) walkScalar(t *schema.Type, n doc.Node, path diag.Path) {
	switch t.Ref {
	case "string":
		v.validateString(t, n, path)
	case "integer":
		v.validateInteger(t, n, path)
	case "float":
		v.validateFloat(t, n, path)
	case "number":
		v.validateNumber(t, n, path)
	case "boolean":
		v.validateBool(t, n, path)
	case "date", "time", "datetime":
		v.validateDatetime(t, n, path)
	default:
		if cs, ok := v.scalars[t.Ref]; ok {
			v.validateCustomScalar(cs, n, path)
		}
		// Unknown ref (should not happen for a valid schema): skip silently.
	}
}

func (v *exec) validateString(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.String {
		v.emit("STRICTSPEC_TYPE_NOT_STRING", path, n,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKindName(kindOf(n))}})
		return
	}
	val := v.decodeString(n)
	length := len([]rune(val))
	if t.NonEmpty && length == 0 {
		v.emit("STRICTSPEC_VALUE_STRING_EMPTY", path, n, map[string]diag.Slot{})
	}
	if t.MinLength != nil && length < *t.MinLength {
		v.emit("STRICTSPEC_VALUE_STRING_TOO_SHORT", path, n, map[string]diag.Slot{
			"actual": diag.SlotInt{N: int64(length)}, "limit": diag.SlotInt{N: int64(*t.MinLength)},
		})
	}
	if t.MaxLength != nil && length > *t.MaxLength {
		v.emit("STRICTSPEC_VALUE_STRING_TOO_LONG", path, n, map[string]diag.Slot{
			"actual": diag.SlotInt{N: int64(length)}, "limit": diag.SlotInt{N: int64(*t.MaxLength)},
		})
	}
	if t.HasRegex && !v.compile(t.Regex).MatchString(val) {
		v.emit("STRICTSPEC_VALUE_STRING_REGEX", path, n, map[string]diag.Slot{
			"actual":  diag.SlotValue{V: diag.StringVal(val)},
			"pattern": diag.SlotValue{V: diag.StringVal(t.Regex)},
		})
	}
}

func (v *exec) validateInteger(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Integer {
		got := nodeKindName(kindOf(n))
		if n != nil && n.Kind() == doc.Float {
			got = "float"
		}
		v.emit("STRICTSPEC_TYPE_NOT_INTEGER", path, n, map[string]diag.Slot{"got": diag.SlotString{S: got}})
		return
	}
	iv, err := strconv.ParseInt(n.Lexeme(), 10, 64)
	if err != nil {
		v.emit("STRICTSPEC_NUM_INT_OVERFLOW", path, n,
			map[string]diag.Slot{"actual": diag.SlotValue{V: v.valueOf(n)}})
		return
	}
	if v.s.SafeIntegers && absInt64(iv) >= (1<<53) {
		v.emit("STRICTSPEC_NUM_SAFE_INTEGER", path, n,
			map[string]diag.Slot{"actual": diag.SlotValue{V: v.valueOf(n)}})
	}
	v.checkNumericBounds(t, n, path, float64(iv))
}

func (v *exec) validateFloat(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Float {
		got := nodeKindName(kindOf(n))
		if n != nil && n.Kind() == doc.Integer {
			got = "integer"
		}
		v.emit("STRICTSPEC_TYPE_NOT_FLOAT", path, n, map[string]diag.Slot{"got": diag.SlotString{S: got}})
		return
	}
	f, err := strconv.ParseFloat(n.Lexeme(), 64)
	if err != nil {
		v.emit("STRICTSPEC_NUM_FLOAT_OVERFLOW", path, n,
			map[string]diag.Slot{"actual": diag.SlotValue{V: v.valueOf(n)}})
		return
	}
	v.checkNumericBounds(t, n, path, f)
}

func (v *exec) validateNumber(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || (n.Kind() != doc.Integer && n.Kind() != doc.Float) {
		v.emit("STRICTSPEC_TYPE_MISMATCH", path, n, map[string]diag.Slot{
			"expected": diag.SlotString{S: "number"},
			"got":      diag.SlotString{S: nodeKindName(kindOf(n))},
		})
		return
	}
	if n.Kind() == doc.Integer {
		if !exactlyRepresentable(n.Lexeme()) {
			v.emit("STRICTSPEC_NUM_UNREPRESENTABLE", path, n,
				map[string]diag.Slot{"actual": diag.SlotValue{V: v.valueOf(n)}})
			return
		}
	}
	f, err := strconv.ParseFloat(n.Lexeme(), 64)
	if err != nil {
		v.emit("STRICTSPEC_NUM_UNREPRESENTABLE", path, n,
			map[string]diag.Slot{"actual": diag.SlotValue{V: v.valueOf(n)}})
		return
	}
	v.checkNumericBounds(t, n, path, f)
}

func (v *exec) validateBool(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Bool {
		v.emit("STRICTSPEC_TYPE_NOT_BOOLEAN", path, n,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKindName(kindOf(n))}})
	}
}

func (v *exec) checkNumericBounds(t *schema.Type, n doc.Node, path diag.Path, val float64) {
	if t.Min != nil && val < svalNum(*t.Min) {
		v.emit("STRICTSPEC_VALUE_NUM_TOO_SMALL", path, n, map[string]diag.Slot{
			"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: svalToValue(*t.Min)},
		})
	}
	if t.ExclusiveMin != nil && val <= svalNum(*t.ExclusiveMin) {
		v.emit("STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE", path, n, map[string]diag.Slot{
			"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: svalToValue(*t.ExclusiveMin)},
		})
	}
	if t.Max != nil && val > svalNum(*t.Max) {
		v.emit("STRICTSPEC_VALUE_NUM_TOO_LARGE", path, n, map[string]diag.Slot{
			"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: svalToValue(*t.Max)},
		})
	}
	if t.ExclusiveMax != nil && val >= svalNum(*t.ExclusiveMax) {
		v.emit("STRICTSPEC_VALUE_NUM_TOO_LARGE_EXCLUSIVE", path, n, map[string]diag.Slot{
			"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: svalToValue(*t.ExclusiveMax)},
		})
	}
}

func (v *exec) validateCustomScalar(cs *schema.Scalar, n doc.Node, path diag.Path) {
	// Base class: every corpus custom scalar refines `string`.
	if cs.Base == "string" && (n == nil || n.Kind() != doc.String) {
		v.emit("STRICTSPEC_SCALAR_BASE_MISMATCH", path, n, map[string]diag.Slot{
			"expected": diag.SlotString{S: cs.Base},
			"name":     diag.SlotIdentifier{Name: cs.Name},
		})
		return
	}
	val := v.decodeString(n)
	length := len([]rune(val))
	// Length refinement is checked BEFORE the lexeme rule (an empty value violates
	// length.min before it fails an `^.+$` rule — matches the pgdesign fixture).
	if cs.LenMin != nil && length < *cs.LenMin {
		v.emitScalarLength(cs, n, path, length, *cs.LenMin)
		return
	}
	if cs.NonEmpty && length == 0 {
		v.emitScalarLength(cs, n, path, length, 1)
		return
	}
	if cs.LenMax != nil && length > *cs.LenMax {
		v.emitScalarLength(cs, n, path, length, *cs.LenMax)
		return
	}
	if cs.LexemeRule != "" && !v.compile(cs.LexemeRule).MatchString(val) {
		v.emit("STRICTSPEC_SCALAR_LEXEME", path, n, map[string]diag.Slot{
			"actual":  diag.SlotValue{V: diag.StringVal(val)},
			"name":    diag.SlotIdentifier{Name: cs.Name},
			"pattern": diag.SlotValue{V: diag.StringVal(cs.LexemeRule)},
		})
	}
}

func (v *exec) emitScalarLength(cs *schema.Scalar, n doc.Node, path diag.Path, actual, limit int) {
	v.emit("STRICTSPEC_SCALAR_LENGTH", path, n, map[string]diag.Slot{
		"name":   diag.SlotIdentifier{Name: cs.Name},
		"actual": diag.SlotInt{N: int64(actual)},
		"limit":  diag.SlotInt{N: int64(limit)},
	})
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// --- datetime ---------------------------------------------------------------

var (
	reDate       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reTime       = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(\.\d+)?$`)
	reOffsetSufx = regexp.MustCompile(`(Z|[+-]\d{2}:\d{2})$`)
)

func (v *exec) validateDatetime(t *schema.Type, n doc.Node, path diag.Path) {
	// Determine the value's datetime form. JSON carries RFC 3339 strings; TOML
	// natives carry their own kind.
	var form string // "date" | "time" | "datetime-offset" | "datetime-local" | ""
	if n != nil && n.Kind() == doc.String {
		form = classifyRFC3339(v.decodeString(n))
	} else if n != nil {
		switch n.Kind() {
		case doc.DateLocal:
			form = "date"
		case doc.TimeLocal:
			form = "time"
		case doc.DateTimeOffset:
			form = "datetime-offset"
		case doc.DateTimeLocal:
			form = "datetime-local"
		}
	}
	switch t.Ref {
	case "date":
		if form != "date" {
			v.emit("STRICTSPEC_TYPE_NOT_DATE", path, n, map[string]diag.Slot{"got": diag.SlotString{S: formGot(form, n)}})
		}
		return
	case "time":
		if form != "time" {
			v.emit("STRICTSPEC_TYPE_NOT_TIME", path, n, map[string]diag.Slot{"got": diag.SlotString{S: formGot(form, n)}})
		}
		return
	case "datetime":
		if form != "datetime-offset" && form != "datetime-local" {
			v.emit("STRICTSPEC_TYPE_NOT_DATETIME", path, n, map[string]diag.Slot{"got": diag.SlotString{S: formGot(form, n)}})
			return
		}
		want := t.DatetimeKind // "offset" | "local"
		got := "offset"
		if form == "datetime-local" {
			got = "local"
		}
		if want != "" && want != got {
			v.emit("STRICTSPEC_TYPE_DATETIME_KIND", path, n, map[string]diag.Slot{
				"expected": diag.SlotString{S: want}, "got": diag.SlotString{S: got},
			})
			return // kind mismatch short-circuits the range comparison
		}
		v.checkDatetimeRange(t, n, path)
	}
}

func classifyRFC3339(s string) string {
	switch {
	case reDate.MatchString(s):
		return "date"
	case reTime.MatchString(s):
		return "time"
	case strings.ContainsRune(s, 'T'):
		if reOffsetSufx.MatchString(s) {
			return "datetime-offset"
		}
		return "datetime-local"
	default:
		return ""
	}
}

func formGot(form string, n doc.Node) string {
	if form == "datetime-offset" || form == "datetime-local" {
		return "datetime"
	}
	if form != "" {
		return form
	}
	return nodeKindName(kindOf(n))
}

func (v *exec) checkDatetimeRange(t *schema.Type, n doc.Node, path diag.Path) {
	val := v.decodeString(n)
	vi, ok := parseInstant(val)
	if !ok {
		return
	}
	if t.Min != nil && t.Min.Kind == doc.String {
		if mi, ok := parseInstant(t.Min.Str); ok && vi < mi {
			v.emit("STRICTSPEC_VALUE_DATETIME_BEFORE", path, n, map[string]diag.Slot{
				"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: diag.DatetimeVal(t.Min.Str)},
			})
		}
	}
	if t.Max != nil && t.Max.Kind == doc.String {
		if ma, ok := parseInstant(t.Max.Str); ok && vi > ma {
			v.emit("STRICTSPEC_VALUE_DATETIME_AFTER", path, n, map[string]diag.Slot{
				"actual": diag.SlotValue{V: v.valueOf(n)}, "limit": diag.SlotValue{V: diag.DatetimeVal(t.Max.Str)},
			})
		}
	}
}
