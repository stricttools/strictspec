package diag

// Slot is a single template slot binding: the typed value that fills one
// `{name}` placeholder in a diagnostic template. It is a small tagged union
// covering the slot-type vocabulary of appendix-error-codes.md §2:
// string, int, code, identifier, version, path, value, and list<T>. (The `code`
// type is in the vocabulary though no current template uses it.)
//
// Two placeholders are handled specially by the renderer and are NEVER supplied
// as ordinary Slots on a Diagnostic:
//   - {path} is auto-injected from the Diagnostic's own Path (Part B contract).
//   - {suggestion} is computed by the renderer from a SlotSuggestion, which
//     carries the unknown token and candidate set (Part C did-you-mean).
type Slot interface{ isSlot() }

// SlotString fills a `string`-typed slot. A `string` slot is a PROSE insertion
// (appendix-error-codes.md §2, appendix-rendering.md A.7): rendered BARE, never
// quoted or escaped — kind-names, field names, remediation commands, and the
// pre-composed {condition} expression (Part D). Document-derived values,
// INCLUDING regex `pattern` slots, use SlotValue and render quoted per A.1.
type SlotString struct{ S string }

// SlotInt fills an `int`-typed slot. Renders as decimal digits (A.1 integer).
type SlotInt struct{ N int64 }

// SlotCode fills a `code`-typed slot: a bare STRICTSPEC_* string, rendered
// verbatim (unquoted).
type SlotCode struct{ Code string }

// SlotIdentifier fills an `identifier`-typed slot (a schema-declared name).
// Rendered bare (unquoted).
type SlotIdentifier struct{ Name string }

// SlotVersion fills a `version`-typed slot: an integer format_version or
// meta_version. Renders as decimal digits.
type SlotVersion struct{ V int64 }

// SlotPath fills a `path`-typed slot with a structured Path, rendered per the
// Part B grammar. (Distinct from the auto-injected {path}: some templates carry
// an additional path-typed slot under a different name.)
type SlotPath struct{ P Path }

// SlotValue fills a `value`-typed slot with a document Value, rendered per A.1.
type SlotValue struct{ V Value }

// SlotList fills a `list<T>`-typed slot, rendered as the A.5 truncated inline
// array form (at most 3 elements + ", ..."). Elements are document Values.
type SlotList struct{ Elems []Value }

// SlotSuggestion fills the `{suggestion}` slot. The renderer computes the
// did-you-mean clause (Part C) from Unknown against Candidates: empty when no
// candidate is within edit distance 2, else " Did you mean ...?".
type SlotSuggestion struct {
	Unknown    string
	Candidates []string
}

func (SlotString) isSlot()     {}
func (SlotInt) isSlot()        {}
func (SlotCode) isSlot()       {}
func (SlotIdentifier) isSlot() {}
func (SlotVersion) isSlot()    {}
func (SlotPath) isSlot()       {}
func (SlotValue) isSlot()      {}
func (SlotList) isSlot()       {}
func (SlotSuggestion) isSlot() {}
