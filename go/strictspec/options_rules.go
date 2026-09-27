package strictspec

// The options rules beyond shape (stricttools/docs/appendix-options.md,
// "The option registry", "Ranking rules", and "Validation"): the ranking
// parser, the registry checks, the per-namespace entry validator, and the
// settled / debt / waiting-on-the-tool classification.
//
// These are unexported on purpose. Every refusal here needs a catalogued
// STRICTSPEC_* code with a pinned message template naming the file, the entry,
// and the fix, and appendix-error-codes.md has no area for them yet (its area
// set is closed). Until that catalogue exists the rules report refusals as
// structured values (rule, file, index, value, detail) and are not part of the
// public runtime surface. The same rules, over the same shared test cases,
// exist in the Python and TypeScript runtimes.

import (
	"regexp"
	"strconv"
	"strings"
)

// optionsNonExistent is the reserved ideal value meaning "the right value is
// one the tool does not offer yet". It is never declared.
const optionsNonExistent = "non-existent"

// optionsScopeNone is the registry scope declaring that an option takes no
// scope.
const optionsScopeNone = "none"

var optionsValueName = regexp.MustCompile(`^[a-z0-9-]+$`)

// optionsRule names one refusal.
type optionsRule string

const (
	ruleRankingMalformed          optionsRule = "ranking-malformed"
	ruleRankingInvalidValueName   optionsRule = "ranking-invalid-value-name"
	ruleRankingReservedValue      optionsRule = "ranking-reserved-value"
	ruleRankingDuplicateValue     optionsRule = "ranking-duplicate-value"
	ruleRegistryDefaultUndeclared optionsRule = "registry-default-undeclared"
	ruleRegistrySubjectInvalid    optionsRule = "registry-subject-invalid"
	ruleEntryUnknownOption        optionsRule = "entry-unknown-option"
	ruleEntryWrongSubject         optionsRule = "entry-wrong-subject"
	ruleEntryScopeNotAccepted     optionsRule = "entry-scope-not-accepted"
	ruleEntryUndeclaredCurrent    optionsRule = "entry-undeclared-current"
	ruleEntryUndeclaredIdeal      optionsRule = "entry-undeclared-ideal"
	ruleEntryRedundant            optionsRule = "entry-redundant"
	ruleEntryCurrentAboveIdeal    optionsRule = "entry-current-above-ideal"
	ruleEntryDuplicate            optionsRule = "entry-duplicate"
)

// optionsRefusal is one refusal. For registry refusals File is empty and Index
// is the [[option]] position; for entry refusals File and Index locate the
// entry. Value is the offending token or value; Detail carries what the fix
// needs (the right subject file, the first occurrence of a duplicate, the
// ranking string).
type optionsRefusal struct {
	Rule   optionsRule
	File   string
	Index  int
	Value  string
	Detail string
}

// optionsRanking is a parsed ranking string. Level 0 is the strongest; values
// of equal rank share a level.
type optionsRanking struct {
	Values []string
	Level  map[string]int
}

func (r optionsRanking) declares(v string) bool {
	_, ok := r.Level[v]
	return ok
}

// parseOptionsRanking parses a ranking string: value names separated by single
// spaces around `>` (stronger than) or `=` (equal rank), for example
// "npm = pypi = jsr > none". A string that is not that alternation is one
// ranking-malformed refusal; otherwise every value name outside the grammar,
// every use of the reserved non-existent, and every repeated value is refused.
func parseOptionsRanking(s string) (optionsRanking, []optionsRefusal) {
	r := optionsRanking{Level: map[string]int{}}
	toks := strings.Split(s, " ")
	malformed := []optionsRefusal{{Rule: ruleRankingMalformed, Value: s}}
	if len(toks)%2 == 0 {
		return optionsRanking{}, malformed
	}
	for i, t := range toks {
		isOp := t == ">" || t == "="
		if t == "" || (i%2 == 1) != isOp {
			return optionsRanking{}, malformed
		}
	}
	var refusals []optionsRefusal
	level := 0
	for i := 0; i < len(toks); i += 2 {
		if i > 0 && toks[i-1] == ">" {
			level++
		}
		v := toks[i]
		switch {
		case !optionsValueName.MatchString(v):
			refusals = append(refusals, optionsRefusal{Rule: ruleRankingInvalidValueName, Value: v})
		case v == optionsNonExistent:
			refusals = append(refusals, optionsRefusal{Rule: ruleRankingReservedValue, Value: v})
		case r.declares(v):
			refusals = append(refusals, optionsRefusal{Rule: ruleRankingDuplicateValue, Value: v})
		default:
			r.Values = append(r.Values, v)
			r.Level[v] = level
		}
	}
	if refusals != nil {
		return optionsRanking{}, refusals
	}
	return r, nil
}

// checkedOption is a registry option whose ranking parsed and whose default
// and subject are valid.
type checkedOption struct {
	Decl    OptionDeclaration
	Ranking optionsRanking
}

// checkedRegistry is a registry that passed every registry rule.
type checkedRegistry struct {
	Options map[string]checkedOption
}

// validOptionsSubject reports whether a registry subject names a subject file
// the entry loader reads: a value-name-grammar stem that is not the options
// directory's own manifest.
func validOptionsSubject(s string) bool {
	return optionsValueName.MatchString(s) && s+".toml" != optionsManifestFile
}

// checkOptionsRegistry applies the registry rules to a shape-valid registry:
// each option's values parse as a ranking, its default is a declared value, and
// its subject is a valid subject file stem. (Option names are unique by the
// built-in schema's unique-by constraint.) Any refusal means a nil registry.
func checkOptionsRegistry(reg *OptionsRegistry) (*checkedRegistry, []optionsRefusal) {
	out := &checkedRegistry{Options: map[string]checkedOption{}}
	var refusals []optionsRefusal
	for i, o := range reg.Options {
		rk, rr := parseOptionsRanking(o.Values)
		for _, r := range rr {
			r.Index = i
			r.Detail = o.Values
			refusals = append(refusals, r)
		}
		if rr == nil && !rk.declares(o.Default) {
			refusals = append(refusals, optionsRefusal{
				Rule: ruleRegistryDefaultUndeclared, Index: i, Value: o.Default, Detail: o.Values,
			})
		}
		if !validOptionsSubject(o.Subject) {
			refusals = append(refusals, optionsRefusal{
				Rule: ruleRegistrySubjectInvalid, Index: i, Value: o.Subject,
			})
		}
		out.Options[o.Name] = checkedOption{Decl: o, Ranking: rk}
	}
	if refusals != nil {
		return nil, refusals
	}
	return out, nil
}

// optionsClass is the ranking classification of an accepted entry.
type optionsClass string

const (
	classSettled       optionsClass = "settled"
	classDebt          optionsClass = "debt"
	classWaitingOnTool optionsClass = "waiting-on-tool"
)

// classifiedEntry is an accepted entry of the validated namespace with its
// classification.
type classifiedEntry struct {
	Entry OptionsEntry
	Class optionsClass
}

func entryLocation(e OptionsEntry) string {
	return e.File + " entry " + strconv.Itoa(e.Index)
}

// validateOptionsNamespace judges the entries of one tool's namespace
// (`<tool>:*`) against that tool's checked registry, in the order given. Entries
// of other namespaces are not judged. It returns the accepted entries,
// classified, and every refusal; an entry with any refusal is not classified.
func validateOptionsNamespace(tool string, reg *checkedRegistry, entries []OptionsEntry) ([]classifiedEntry, []optionsRefusal) {
	prefix := tool + ":"
	type key struct {
		id       string
		hasScope bool
		scope    string
	}
	first := map[key]OptionsEntry{}
	var accepted []classifiedEntry
	var refusals []optionsRefusal
	for _, e := range entries {
		if !strings.HasPrefix(e.ID, prefix) {
			continue
		}
		refuse := func(rule optionsRule, value, detail string) {
			refusals = append(refusals, optionsRefusal{Rule: rule, File: e.File, Index: e.Index, Value: value, Detail: detail})
		}
		before := len(refusals)
		k := key{e.ID, e.HasScope, e.Scope}
		if f, dup := first[k]; dup {
			refuse(ruleEntryDuplicate, e.ID, entryLocation(f))
		} else {
			first[k] = e
		}
		opt, known := reg.Options[strings.TrimPrefix(e.ID, prefix)]
		if !known {
			refuse(ruleEntryUnknownOption, e.ID, "")
			continue
		}
		if want := opt.Decl.Subject + ".toml"; e.File != want {
			refuse(ruleEntryWrongSubject, e.File, want)
		}
		if e.HasScope && opt.Decl.Scope == optionsScopeNone {
			refuse(ruleEntryScopeNotAccepted, e.Scope, "")
		}
		rk := opt.Ranking
		currentOK := rk.declares(e.Current)
		if !currentOK {
			refuse(ruleEntryUndeclaredCurrent, e.Current, opt.Decl.Values)
		}
		waiting := e.Ideal == optionsNonExistent
		idealOK := waiting || rk.declares(e.Ideal)
		if !idealOK {
			refuse(ruleEntryUndeclaredIdeal, e.Ideal, opt.Decl.Values)
		}
		if currentOK && idealOK {
			if e.Current == opt.Decl.Default && e.Ideal == opt.Decl.Default {
				refuse(ruleEntryRedundant, e.Current, "")
			} else if !waiting && rk.Level[e.Current] < rk.Level[e.Ideal] {
				refuse(ruleEntryCurrentAboveIdeal, e.Current, e.Ideal)
			}
		}
		if len(refusals) > before {
			continue
		}
		class := classDebt
		switch {
		case waiting:
			class = classWaitingOnTool
		case rk.Level[e.Current] == rk.Level[e.Ideal]:
			class = classSettled
		}
		accepted = append(accepted, classifiedEntry{Entry: e, Class: class})
	}
	return accepted, refusals
}
