// Package codes is the generated strictspec error-code catalogue: every
// STRICTSPEC_* code with its area, message template, and declared slots, parsed
// from spec/appendix-error-codes.md (the single normative source). The table
// lives in catalogue_gen.go and is produced by tools/gencodes; hand-transcription
// is forbidden. A freshness test regenerates and byte-compares (drift = failure).
package codes

//go:generate go run github.com/smm-h/strictspec/go/tools/gencodes

import "sort"

// SlotType is one of the slot-type vocabulary of appendix-error-codes.md §2.
type SlotType int

const (
	SlotTypeString SlotType = iota
	SlotTypeInt
	SlotTypeCode
	SlotTypeIdentifier
	SlotTypeVersion
	SlotTypePath
	SlotTypeValue
	SlotTypeList
)

// String returns the appendix spelling of the slot type (list types render as
// "list<elem>").
func (t SlotType) String() string {
	switch t {
	case SlotTypeString:
		return "string"
	case SlotTypeInt:
		return "int"
	case SlotTypeCode:
		return "code"
	case SlotTypeIdentifier:
		return "identifier"
	case SlotTypeVersion:
		return "version"
	case SlotTypePath:
		return "path"
	case SlotTypeValue:
		return "value"
	case SlotTypeList:
		return "list"
	default:
		return "unknown"
	}
}

// SlotSpec is one declared slot: its placeholder name and its declared type.
// For SlotTypeList, ElemType is the element type (e.g. list<string>).
type SlotSpec struct {
	Name     string
	Type     SlotType
	ElemType SlotType
}

// Entry is one catalogue row: the code, its area, the pinned message template
// (with literal `{name}` placeholders), and the slots the template interpolates
// in placeholder order.
type Entry struct {
	Code     string
	Area     string
	Template string
	Slots    []SlotSpec
}

// Lookup returns the catalogue entry for a code and whether it exists.
func Lookup(code string) (Entry, bool) {
	e, ok := catalogue[code]
	return e, ok
}

// All returns every catalogue entry, sorted by code.
func All() []Entry {
	out := make([]Entry, 0, len(catalogue))
	for _, e := range catalogue {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
