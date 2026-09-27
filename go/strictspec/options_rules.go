package strictspec

// The options rules beyond shape (stricttools/docs/appendix-options.md,
// "The option registry", "Ranking rules", and "Validation"): the ranking
// parser, the registry rules, the per-namespace entry validator, and the
// settled / debt / waiting-on-the-tool classification.
//
// Every refusal is a catalogued STRICTSPEC_OPTIONS_* diagnostic
// (appendix-error-codes.md, section 21a) rendered from its pinned template, so
// the codes, paths, and messages are identical to the Python and TypeScript
// runtimes', which apply the same rules over the same shared test cases.

import (
	"regexp"
	"strings"

	"github.com/stricttools/strictspec/go/internal/diag"
)

// OptionsNonExistent is the reserved ideal value meaning "the right value is
// one the tool does not offer yet". A ranking never declares it.
const OptionsNonExistent = "non-existent"

// optionsScopeNone is the registry scope declaring that an option takes no
// scope.
const optionsScopeNone = "none"

var optionsValueName = regexp.MustCompile(`^[a-z0-9-]+$`)

// OptionsRanking is a parsed ranking string. Values lists the declared values
// in the order written; Level maps each to its rank, 0 being the strongest,
// with values of equal rank sharing a level.
type OptionsRanking struct {
	Values []string
	Level  map[string]int
}

// Declares reports whether v is one of the ranking's values.
func (r *OptionsRanking) Declares(v string) bool {
	_, ok := r.Level[v]
	return ok
}

func publicDiagnostics(ds []diag.Diagnostic) []Diagnostic {
	if len(ds) == 0 {
		return nil
	}
	return render_(ds).Diagnostics
}

func strVal(s string) diag.Slot { return diag.SlotValue{V: diag.StringVal(s)} }

// fieldPath is a new path: at followed by the record field name.
func fieldPath(at diag.Path, name string) diag.Path {
	steps := append(append([]diag.Step(nil), at.Steps...), diag.Key{Name: name})
	return diag.Path{Steps: steps}
}

func parseRanking(s string, at diag.Path) (*OptionsRanking, []diag.Diagnostic) {
	toks := strings.Split(s, " ")
	malformed := []diag.Diagnostic{{
		Code: "STRICTSPEC_OPTIONS_RANKING_MALFORMED", Path: at,
		Slots: map[string]diag.Slot{"ranking": strVal(s)},
	}}
	if len(toks)%2 == 0 {
		return nil, malformed
	}
	for i, t := range toks {
		isOp := t == ">" || t == "="
		if t == "" || (i%2 == 1) != isOp {
			return nil, malformed
		}
	}
	r := &OptionsRanking{Values: []string{}, Level: map[string]int{}}
	var ds []diag.Diagnostic
	refuse := func(code, v string) {
		ds = append(ds, diag.Diagnostic{Code: code, Path: at, Slots: map[string]diag.Slot{"value": strVal(v)}})
	}
	level := 0
	for i := 0; i < len(toks); i += 2 {
		if i > 0 && toks[i-1] == ">" {
			level++
		}
		v := toks[i]
		switch {
		case !optionsValueName.MatchString(v):
			refuse("STRICTSPEC_OPTIONS_RANKING_VALUE_NAME", v)
		case v == OptionsNonExistent:
			refuse("STRICTSPEC_OPTIONS_RANKING_RESERVED", v)
		case r.Declares(v):
			refuse("STRICTSPEC_OPTIONS_RANKING_DUPLICATE", v)
		default:
			r.Values = append(r.Values, v)
			r.Level[v] = level
		}
	}
	if ds != nil {
		return nil, ds
	}
	return r, nil
}

// ParseOptionsRanking parses a ranking string: value names separated by single
// spaces around `>` (stronger than) or `=` (equal rank), for example
// "npm = pypi = jsr > none". A string that is not that alternation is one
// STRICTSPEC_OPTIONS_RANKING_MALFORMED diagnostic; otherwise every value name
// outside the grammar, every use of the reserved non-existent, and every
// repeated value is refused. The diagnostics' path is "$". Non-empty
// diagnostics mean a nil ranking.
func ParseOptionsRanking(s string) (*OptionsRanking, []Diagnostic) {
	r, ds := parseRanking(s, diag.NewPath())
	return r, publicDiagnostics(ds)
}

// CheckedOption is a registry option that passed every registry rule, with its
// parsed ranking.
type CheckedOption struct {
	Declaration OptionDeclaration
	Ranking     *OptionsRanking
}

// CheckedOptionsRegistry is a registry that passed every registry rule. Only
// ValidateOptionsRegistry makes one.
type CheckedOptionsRegistry struct {
	options map[string]CheckedOption
	names   []string
}

// Option returns the checked option named name (the tool's own name for it,
// without the tool prefix).
func (r *CheckedOptionsRegistry) Option(name string) (CheckedOption, bool) {
	o, ok := r.options[name]
	return o, ok
}

// Names returns the registry's option names in declaration order.
func (r *CheckedOptionsRegistry) Names() []string {
	return append([]string(nil), r.names...)
}

// validOptionsSubject reports whether a registry subject names a subject file
// the entry loader reads: a value-name-grammar stem that is not the options
// directory's own manifest.
func validOptionsSubject(s string) bool {
	return optionsValueName.MatchString(s) && s+".toml" != optionsManifestFile
}

// ValidateOptionsRegistry applies the registry rules to a registry: each
// option's values parse as a ranking, its default is a declared value, and its
// subject is a valid subject file stem. Option names are unique by the
// built-in schema; a registry handed over directly with a repeated name draws
// the same STRICTSPEC_INTRA_UNIQUE_BY diagnostic the shape reader reports.
// Paths locate the refused field in the registry document. Non-empty
// diagnostics mean a nil registry.
func ValidateOptionsRegistry(reg *OptionsRegistry) (*CheckedOptionsRegistry, []Diagnostic) {
	out := &CheckedOptionsRegistry{options: map[string]CheckedOption{}}
	var ds []diag.Diagnostic
	for i, o := range reg.Options {
		at := diag.NewPath(diag.Key{Name: "option"}, diag.Index{N: i})
		if _, dup := out.options[o.Name]; dup {
			ds = append(ds, diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_UNIQUE_BY",
				Path: diag.NewPath(diag.Key{Name: "option"}),
				Slots: map[string]diag.Slot{
					"value":         strVal(o.Name),
					"field":         diag.SlotString{S: "name"},
					"normalization": diag.SlotString{S: "none"},
				},
			})
		}
		rk, rds := parseRanking(o.Values, fieldPath(at, "values"))
		ds = append(ds, rds...)
		if rk != nil && !rk.Declares(o.Default) {
			ds = append(ds, diag.Diagnostic{
				Code: "STRICTSPEC_OPTIONS_DEFAULT_UNDECLARED", Path: fieldPath(at, "default"),
				Slots: map[string]diag.Slot{"value": strVal(o.Default), "ranking": strVal(o.Values)},
			})
		}
		if !validOptionsSubject(o.Subject) {
			ds = append(ds, diag.Diagnostic{
				Code: "STRICTSPEC_OPTIONS_SUBJECT_INVALID", Path: fieldPath(at, "subject"),
				Slots: map[string]diag.Slot{"value": strVal(o.Subject)},
			})
		}
		if _, dup := out.options[o.Name]; !dup {
			out.options[o.Name] = CheckedOption{Declaration: o, Ranking: rk}
			out.names = append(out.names, o.Name)
		}
	}
	if ds != nil {
		return nil, publicDiagnostics(ds)
	}
	return out, nil
}

// OptionsClass is the ranking classification of an accepted entry.
type OptionsClass string

const (
	// OptionsSettled: current and ideal have equal rank.
	OptionsSettled OptionsClass = "settled"
	// OptionsDebt: current ranks below ideal.
	OptionsDebt OptionsClass = "debt"
	// OptionsWaitingOnTool: ideal is non-existent, a value the tool does not
	// offer yet. Such entries sit outside the ranking.
	OptionsWaitingOnTool OptionsClass = "waiting-on-tool"
)

// ClassifiedOptionsEntry is an accepted entry of the validated namespace with
// its classification.
type ClassifiedOptionsEntry struct {
	Entry OptionsEntry
	Class OptionsClass
}

// optionsFile is the repository-relative path of a subject document.
func optionsFile(name string) string { return OptionsDir + "/" + name }

func entryPath(e OptionsEntry) diag.Path {
	return diag.NewPath(diag.Key{Name: "entry"}, diag.Index{N: e.Index})
}

// ValidateOptionsNamespace judges the entries of one tool's namespace
// (`<tool>:*`) against that tool's checked registry, in the order given; tool
// is the tool's name. Entries of other namespaces are not judged. It returns
// the accepted entries, classified, and a diagnostic for every refusal; an
// entry with any refusal is not classified. Each diagnostic's path locates the
// entry within its subject document, and its message names that document.
func ValidateOptionsNamespace(tool string, reg *CheckedOptionsRegistry, entries []OptionsEntry) ([]ClassifiedOptionsEntry, []Diagnostic) {
	prefix := tool + ":"
	type key struct {
		id       string
		hasScope bool
		scope    string
	}
	candidates := make([]string, 0, len(reg.names))
	for _, n := range reg.names {
		candidates = append(candidates, prefix+n)
	}
	first := map[key]OptionsEntry{}
	accepted := []ClassifiedOptionsEntry{}
	var ds []diag.Diagnostic
	for _, e := range entries {
		if !strings.HasPrefix(e.ID, prefix) {
			continue
		}
		before := len(ds)
		at := entryPath(e)
		refuse := func(code string, path diag.Path, slots map[string]diag.Slot) {
			slots["file"] = diag.SlotString{S: optionsFile(e.File)}
			if code != "STRICTSPEC_OPTIONS_UNKNOWN_OPTION" {
				slots["id"] = strVal(e.ID)
			}
			ds = append(ds, diag.Diagnostic{Code: code, Path: path, Slots: slots})
		}
		k := key{e.ID, e.HasScope, e.Scope}
		if f, dup := first[k]; dup {
			refuse("STRICTSPEC_OPTIONS_DUPLICATE_ENTRY", at, map[string]diag.Slot{
				"first":      diag.SlotPath{P: entryPath(f)},
				"first_file": diag.SlotString{S: optionsFile(f.File)},
			})
		} else {
			first[k] = e
		}
		opt, known := reg.options[strings.TrimPrefix(e.ID, prefix)]
		if !known {
			refuse("STRICTSPEC_OPTIONS_UNKNOWN_OPTION", fieldPath(at, "id"), map[string]diag.Slot{
				"id":         strVal(e.ID),
				"tool":       diag.SlotString{S: tool},
				"suggestion": diag.SlotSuggestion{Unknown: e.ID, Candidates: candidates},
			})
		} else {
			decl, rk := opt.Declaration, opt.Ranking
			if want := decl.Subject + ".toml"; e.File != want {
				refuse("STRICTSPEC_OPTIONS_WRONG_SUBJECT", at, map[string]diag.Slot{
					"subject": diag.SlotString{S: optionsFile(want)},
				})
			}
			if e.HasScope && decl.Scope == optionsScopeNone {
				refuse("STRICTSPEC_OPTIONS_SCOPE_NOT_ACCEPTED", fieldPath(at, "scope"), map[string]diag.Slot{
					"value": strVal(e.Scope),
				})
			}
			currentOK := rk.Declares(e.Current)
			if !currentOK {
				refuse("STRICTSPEC_OPTIONS_UNDECLARED_CURRENT", fieldPath(at, "current"), map[string]diag.Slot{
					"value": strVal(e.Current), "ranking": strVal(decl.Values),
				})
			}
			waiting := e.Ideal == OptionsNonExistent
			idealOK := waiting || rk.Declares(e.Ideal)
			if !idealOK {
				refuse("STRICTSPEC_OPTIONS_UNDECLARED_IDEAL", fieldPath(at, "ideal"), map[string]diag.Slot{
					"value": strVal(e.Ideal), "ranking": strVal(decl.Values),
				})
			}
			if currentOK && idealOK {
				if e.Current == decl.Default && e.Ideal == decl.Default {
					refuse("STRICTSPEC_OPTIONS_REDUNDANT", at, map[string]diag.Slot{"value": strVal(e.Current)})
				} else if !waiting && rk.Level[e.Current] < rk.Level[e.Ideal] {
					refuse("STRICTSPEC_OPTIONS_CURRENT_ABOVE_IDEAL", at, map[string]diag.Slot{
						"current": strVal(e.Current), "ideal": strVal(e.Ideal), "ranking": strVal(decl.Values),
					})
				}
			}
		}
		if e.Reason == "" {
			refuse("STRICTSPEC_OPTIONS_EMPTY_REASON", fieldPath(at, "reason"), map[string]diag.Slot{})
		}
		if len(ds) > before {
			continue
		}
		rk := opt.Ranking
		class := OptionsDebt
		switch {
		case e.Ideal == OptionsNonExistent:
			class = OptionsWaitingOnTool
		case rk.Level[e.Current] == rk.Level[e.Ideal]:
			class = OptionsSettled
		}
		accepted = append(accepted, ClassifiedOptionsEntry{Entry: e, Class: class})
	}
	return accepted, publicDiagnostics(ds)
}
