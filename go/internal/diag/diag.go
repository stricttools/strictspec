// Package diag defines the strictspec diagnostic model: the path grammar, the
// document-value and slot tagged unions, and the Diagnostic / Diagnostics types.
// It is the shared vocabulary the emitter IR populates and the render package
// consumes. It contains no message templates and no rendering of message text
// (that is the render package, driven by the generated codes catalogue) — only
// the structured data a diagnostic carries and the path/value primitives whose
// rendering is fixed by appendix-rendering.md.
package diag

// Diagnostic is one emitted hard error: a STRICTSPEC_* code, the Path it is
// attached to (auto-injected into the template's {path} slot), and the typed
// slot bindings that fill the remaining template placeholders.
//
// {path} is never present in Slots — it comes from Path. {suggestion}, when the
// template has it, is supplied as a SlotSuggestion under the key "suggestion".
type Diagnostic struct {
	Code  string
	Path  Path
	Slots map[string]Slot
}

// Diagnostics is an ordered, one-pass accumulation of diagnostics in emission
// order (the emitter-IR one-pass-accumulate discipline: renderers may not
// reorder). Zero value is an empty, ready-to-use collection.
type Diagnostics struct {
	items []Diagnostic
}

// Emit appends one diagnostic in emission order.
func (d *Diagnostics) Emit(diagnostic Diagnostic) {
	d.items = append(d.items, diagnostic)
}

// EmitCode is a convenience constructor: append a diagnostic with the given
// code, path, and slots in emission order.
func (d *Diagnostics) EmitCode(code string, path Path, slots map[string]Slot) {
	d.items = append(d.items, Diagnostic{Code: code, Path: path, Slots: slots})
}

// All returns the accumulated diagnostics in emission order. The returned slice
// aliases internal storage; callers must not mutate it.
func (d *Diagnostics) All() []Diagnostic {
	return d.items
}

// Len reports how many diagnostics have accumulated.
func (d *Diagnostics) Len() int {
	return len(d.items)
}
