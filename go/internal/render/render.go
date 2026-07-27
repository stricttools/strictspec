// Package render turns a structured diag.Diagnostic into its pinned message
// text: it substitutes each template slot with the value rendering fixed by
// spec/appendix-rendering.md (Part A value rendering, Part B path grammar,
// Part C did-you-mean, Part D condition scheme). Templates come from the
// generated codes catalogue; there is no hand-written message string here.
//
// Programmer-error policy: an unknown code, an unknown slot binding, or a
// missing required slot at render time all PANIC. Rationale: a Diagnostic is
// constructed by generated emitter code that is slot-correct by construction;
// these conditions can only mean a bug in the emitter or catalogue, never
// malformed user input. Panicking fails loudly (the "hard errors, not warnings"
// discipline) rather than silently rendering a broken message. All three cases
// are handled identically and consistently.
package render

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/smm-h/strictspec/go/internal/codes"
	"github.com/smm-h/strictspec/go/internal/diag"
)

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

// Render produces the pinned message text for a diagnostic.
func Render(d diag.Diagnostic) string {
	entry, ok := codes.Lookup(d.Code)
	if !ok {
		panic(fmt.Sprintf("render: unknown code %q (not in the catalogue)", d.Code))
	}

	placeholders := placeholderSet(entry.Template)
	validateSlots(d, placeholders)

	return placeholderRe.ReplaceAllStringFunc(entry.Template, func(match string) string {
		name := match[1 : len(match)-1] // strip the braces
		switch name {
		case "path":
			// Auto-injected from the diagnostic's own path (Part B contract);
			// emitters never bind it manually.
			return d.Path.Render()
		case "suggestion":
			slot, ok := d.Slots["suggestion"]
			if !ok {
				return "" // optional: no candidate/clause
			}
			sug, ok := slot.(diag.SlotSuggestion)
			if !ok {
				panic(fmt.Sprintf("render: code %s slot \"suggestion\" must be a SlotSuggestion, got %T", d.Code, slot))
			}
			return renderSuggestion(sug)
		default:
			slot, ok := d.Slots[name]
			if !ok {
				panic(fmt.Sprintf("render: code %s missing required slot %q", d.Code, name))
			}
			return renderSlot(d.Code, name, slot)
		}
	})
}

func placeholderSet(template string) map[string]bool {
	set := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(template, -1) {
		set[m[1]] = true
	}
	return set
}

// validateSlots enforces slot-coverage invariants (all programmer errors):
//   - no binding may target {path} (auto-injected, never bound manually);
//   - every provided binding must be a template placeholder;
//   - every required placeholder (all but auto {path} and optional {suggestion})
//     must be bound.
func validateSlots(d diag.Diagnostic, placeholders map[string]bool) {
	for name := range d.Slots {
		if name == "path" {
			panic(fmt.Sprintf("render: code %s binds {path} manually; {path} is auto-injected from the diagnostic path", d.Code))
		}
		if !placeholders[name] {
			panic(fmt.Sprintf("render: code %s has unknown slot %q (not a template placeholder)", d.Code, name))
		}
	}
	for name := range placeholders {
		if name == "path" || name == "suggestion" {
			continue
		}
		if _, ok := d.Slots[name]; !ok {
			panic(fmt.Sprintf("render: code %s missing required slot %q", d.Code, name))
		}
	}
}

// renderSlot renders one slot binding per its dynamic type.
func renderSlot(code, name string, slot diag.Slot) string {
	switch s := slot.(type) {
	case diag.SlotString:
		// A `string` slot is a PROSE insertion (appendix-error-codes.md §2,
		// appendix-rendering.md A.7): rendered BARE, never quoted or escaped.
		// This covers kind-names, field names, remediation commands, and the
		// pre-composed {condition} expression (Part D), whose inner literals are
		// already A.1-rendered and must not be re-quoted. Document-derived values
		// (including regex patterns) are SlotValue, rendered quoted per A.1.
		return s.S
	case diag.SlotInt:
		return strconv.FormatInt(s.N, 10)
	case diag.SlotCode:
		return s.Code
	case diag.SlotIdentifier:
		return s.Name
	case diag.SlotVersion:
		return strconv.FormatInt(s.V, 10)
	case diag.SlotPath:
		return s.P.Render()
	case diag.SlotValue:
		return renderValue(s.V)
	case diag.SlotList:
		return renderArray(s.Elems, 1)
	case diag.SlotSuggestion:
		return renderSuggestion(s)
	default:
		panic(fmt.Sprintf("render: code %s slot %q has unknown slot type %T", code, name, slot))
	}
}

// --- Value rendering (appendix-rendering.md Part A) --------------------------

// renderValue renders a document value per A.1 (top-level container depth = 1).
func renderValue(v diag.Value) string {
	return renderValueAtDepth(v, 1)
}

func renderValueAtDepth(v diag.Value, depth int) string {
	switch x := v.(type) {
	case diag.IntVal:
		return strconv.FormatInt(int64(x), 10)
	case diag.FloatVal:
		if x.HasLexeme {
			return x.Lexeme // A.1: source lexeme unchanged (exponent form preserved)
		}
		return canonicalFloat(x.F) // A.3: constructed float, always float-marked
	case diag.NumberVal:
		return x.Lexeme // A.1: rendered per its source lexeme class
	case diag.StringVal:
		return renderQuotedString(string(x))
	case diag.BoolVal:
		if bool(x) {
			return "true"
		}
		return "false"
	case diag.NullVal:
		return "null"
	case diag.DateVal:
		return string(x)
	case diag.TimeVal:
		return string(x)
	case diag.DatetimeVal:
		return string(x)
	case diag.ArrayVal:
		if depth > 2 {
			return "[...]" // A.5: deeper than 2 levels renders as its kind sentinel
		}
		return renderArray([]diag.Value(x), depth)
	case diag.RecordVal:
		if depth > 2 {
			return "{...}"
		}
		return renderRecord(x, depth)
	default:
		panic(fmt.Sprintf("render: unknown value type %T", v))
	}
}

// renderArray renders an array / list per A.5: [e1, e2, e3, ...], at most 3
// elements shown, each rendered at depth+1.
func renderArray(elems []diag.Value, depth int) string {
	var b strings.Builder
	b.WriteByte('[')
	shown := len(elems)
	if shown > 3 {
		shown = 3
	}
	for i := 0; i < shown; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(renderValueAtDepth(elems[i], depth+1))
	}
	if len(elems) > 3 {
		b.WriteString(", ...")
	}
	b.WriteByte(']')
	return b.String()
}

// renderRecord renders a record per A.5: {k1: v1, k2: v2, k3: v3, ...} in
// document order, keys bare when identifier-shaped else quoted per A.2, at most
// 3 pairs shown.
func renderRecord(r diag.RecordVal, depth int) string {
	if len(r.Keys) != len(r.Vals) {
		panic(fmt.Sprintf("render: RecordVal has %d keys but %d values", len(r.Keys), len(r.Vals)))
	}
	var b strings.Builder
	b.WriteByte('{')
	shown := len(r.Keys)
	if shown > 3 {
		shown = 3
	}
	for i := 0; i < shown; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		if diag.IsIdentShaped(r.Keys[i]) {
			b.WriteString(r.Keys[i])
		} else {
			b.WriteByte('"')
			b.WriteString(diag.EscapeString(r.Keys[i]))
			b.WriteByte('"')
		}
		b.WriteString(": ")
		b.WriteString(renderValueAtDepth(r.Vals[i], depth+1))
	}
	if len(r.Keys) > 3 {
		b.WriteString(", ...")
	}
	b.WriteByte('}')
	return b.String()
}

// renderQuotedString renders a string per A.1: double-quoted, A.2-escaped, and
// A.4-truncated to 64 source code points with a "..." ellipsis before the
// closing quote.
func renderQuotedString(s string) string {
	runes := []rune(s)
	truncated := false
	if len(runes) > 64 {
		runes = runes[:64]
		truncated = true
	}
	content := diag.EscapeString(string(runes))
	if truncated {
		return `"` + content + `..."`
	}
	return `"` + content + `"`
}

// canonicalFloat renders a constructed (lexeme-less) float per A.3: the shortest
// round-tripping decimal, always float-marked (a value with no fractional part
// or exponent gains a trailing ".0").
func canonicalFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
