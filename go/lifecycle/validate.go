package lifecycle

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Validate checks the whole record: the structural checks Load runs, plus the
// checks that need the date and the repository's declarations. declared is
// every releasable and member name the repository's declarations hold (empty
// for a repository with none, such as a minimal record).
//
//   - The subject of an open or pending entry must be declared, or be the
//     subject of a retired lifecycle period. A closed period and a registry
//     name may name any subject: they record the past.
func (r *Record) Validate(on time.Time, declared []string) error {
	problems := r.structuralProblems()
	known := map[string]bool{}
	for _, d := range declared {
		known[d] = true
	}
	for _, l := range r.lifecycle {
		if l.Status == StatusRetired {
			known[l.Subject] = true
		}
	}
	undeclared := map[string][]string{}
	note := func(subject, where string) {
		if !known[subject] {
			undeclared[subject] = append(undeclared[subject], where)
		}
	}
	for _, l := range r.lifecycle {
		if l.Open() {
			note(l.Subject, "an open lifecycle period")
		}
	}
	for _, l := range r.licenses {
		if l.Open() {
			note(l.Subject, "an open license period")
		}
	}
	for _, id := range r.identities {
		switch {
		case id.Pending():
			note(id.Subject, "a pending "+string(id.Facet)+" identity")
		case id.Open():
			note(id.Subject, "an open "+string(id.Facet)+" identity")
		}
	}
	for _, subject := range sortedKeys(undeclared) {
		problems = append(problems, fmt.Sprintf(
			"subject %q has %s but is neither declared as a releasable or member nor retired; declare it, close its periods, or record a retired lifecycle period for it",
			subject, strings.Join(dedupe(undeclared[subject]), " and ")))
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// structuralProblems are the checks that need neither the date nor the
// declarations: period bounds, overlaps, open and pending entries, and
// uniqueness.
func (r *Record) structuralProblems() []string {
	var problems []string
	type keyed struct {
		key    string
		period Period
	}
	checkPeriods := func(table string, entries []keyed) {
		groups := map[string][]Period{}
		var order []string
		for _, e := range entries {
			if !e.period.Open() && e.period.Until.Before(e.period.From) {
				problems = append(problems, fmt.Sprintf(
					"%s: %s has a period whose until (%s) is before its from (%s); until must be on or after from",
					table, e.key, formatDate(e.period.Until), formatDate(e.period.From)))
				continue
			}
			if _, ok := groups[e.key]; !ok {
				order = append(order, e.key)
			}
			groups[e.key] = append(groups[e.key], e.period)
		}
		for _, key := range order {
			ps := groups[key]
			var open []string
			for _, p := range ps {
				if p.Open() {
					open = append(open, p.describe())
				}
			}
			if len(open) > 1 {
				problems = append(problems, fmt.Sprintf(
					"%s: %s has %d open periods (%s); at most one may be open, so close the earlier ones",
					table, key, len(open), strings.Join(open, "; ")))
			}
			for i := 0; i < len(ps); i++ {
				for j := i + 1; j < len(ps); j++ {
					if ps[i].overlaps(ps[j]) {
						problems = append(problems, fmt.Sprintf(
							"%s: %s has overlapping periods (%s) and (%s); periods of one subject must not share a day",
							table, key, ps[i].describe(), ps[j].describe()))
					}
				}
			}
		}
	}

	var lifecycle []keyed
	for _, l := range r.lifecycle {
		lifecycle = append(lifecycle, keyed{"subject " + quote(l.Subject), l.Period})
	}
	checkPeriods("lifecycle", lifecycle)

	var licenses []keyed
	for _, l := range r.licenses {
		licenses = append(licenses, keyed{"subject " + quote(l.Subject), l.Period})
	}
	checkPeriods("licenses", licenses)

	var identities []keyed
	pending := map[string]int{}
	var pendingOrder []string
	for _, id := range r.identities {
		key := "subject " + quote(id.Subject) + " facet " + quote(string(id.Facet))
		if id.Facet.RegistryScoped() {
			key += " registry " + quote(id.Registry)
			if id.Registry == "" {
				problems = append(problems, fmt.Sprintf(
					"identities: subject %q facet %q value %q names no registry; a %s is a name in one registry, so the identity names it (npm, pypi, or go), one identity per registry",
					id.Subject, id.Facet, id.Value, id.Facet))
			}
		}
		hasFrom := !id.From.IsZero()
		switch {
		case id.Pending() && hasFrom:
			problems = append(problems, fmt.Sprintf(
				"identities: %s value %q carries both from and effective_version; a pending entry carries only effective_version",
				key, id.Value))
		case !id.Pending() && !hasFrom:
			problems = append(problems, fmt.Sprintf(
				"identities: %s value %q carries neither from nor effective_version; give it the date its period began, or the version whose release begins it",
				key, id.Value))
		case id.Pending():
			if !id.Until.IsZero() {
				problems = append(problems, fmt.Sprintf(
					"identities: %s value %q is pending and carries until; a pending entry has no until",
					key, id.Value))
			}
			if pending[key] == 0 {
				pendingOrder = append(pendingOrder, key)
			}
			pending[key]++
		default:
			identities = append(identities, keyed{key, id.Period})
		}
		for _, p := range id.TagPatterns {
			if strings.ContainsAny(p, "[]\\") {
				problems = append(problems, fmt.Sprintf(
					"identities: %s value %q has the tag pattern %q; tag patterns use only * and ?, never brackets or backslashes",
					key, id.Value, p))
			}
		}
	}
	checkPeriods("identities", identities)
	for _, key := range pendingOrder {
		if pending[key] > 1 {
			problems = append(problems, fmt.Sprintf(
				"identities: %s has %d pending entries; at most one may be pending, so remove the others",
				key, pending[key]))
		}
	}

	seenNames := map[string]bool{}
	for _, n := range r.registryNames {
		key := n.Registry + " " + n.Name
		if seenNames[key] {
			problems = append(problems, fmt.Sprintf(
				"registry_names: %s name %q is recorded more than once; keep one entry",
				n.Registry, n.Name))
		}
		seenNames[key] = true
	}
	seenTags := map[string]bool{}
	for _, t := range r.unversionedTags {
		if seenTags[t.Tag] {
			problems = append(problems, fmt.Sprintf(
				"unversioned_tags: tag %q is recorded more than once; keep one entry", t.Tag))
		}
		seenTags[t.Tag] = true
	}
	return problems
}

func quote(s string) string { return fmt.Sprintf("%q", s) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
