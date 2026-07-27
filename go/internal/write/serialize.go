package write

import (
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
)

// Serialize implements the PRODUCER-CURRENT-ONLY leg of the version-boundary
// invariant (spec/DESIGN.md canonical serialization appendix; A.6). Given a
// document's root node, its current source bytes, and the schema's current
// format_version, it returns the bytes to write — but HARD-ERRORS
// (STRICTSPEC_SERIALIZE_NONCURRENT) when the document's format_version is not the
// current one. No conforming producer can create new staleness.
//
// The returned bytes are byte-identical to src (the model retains lexemes;
// nothing re-renders what an op did not change). A nil diagnostic means the write
// is permitted.
func Serialize(root doc.Node, src []byte, currentFormatVersion int64, schemaName string) ([]byte, *diag.Diagnostic) {
	got, ok := formatVersionOf(root)
	if !ok || got != currentFormatVersion {
		d := &diag.Diagnostic{
			Code: "STRICTSPEC_SERIALIZE_NONCURRENT",
			Path: diag.NewPath(),
			Slots: map[string]diag.Slot{
				"got":      diag.SlotVersion{V: got},
				"schema":   diag.SlotIdentifier{Name: schemaName},
				"expected": diag.SlotVersion{V: currentFormatVersion},
			},
		}
		return nil, d
	}
	return append([]byte(nil), src...), nil
}

// formatVersionOf reads a document root's integer `format_version`. ok is false
// when the field is absent or not an integer lexeme.
func formatVersionOf(root doc.Node) (int64, bool) {
	if root == nil || root.Kind() != doc.Record {
		return 0, false
	}
	for _, e := range root.Entries() {
		if e.Key == "format_version" && e.Value.Kind() == doc.Integer {
			v, err := strconv.ParseInt(e.Value.Lexeme(), 10, 64)
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
}
