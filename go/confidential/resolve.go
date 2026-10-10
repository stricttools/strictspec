package confidential

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Resolved is a hit and the resolution that resolves it.
type Resolved struct {
	Hit        Hit
	Resolution Resolution
}

// Outcome is every hit of a scan, split by whether a resolution resolves it.
type Outcome struct {
	Unresolved []Hit
	Resolved   []Resolved
}

// Resolve splits hits by whether one of rs names the hit's id.
func Resolve(hits []Hit, rs []Resolution) Outcome {
	byID := map[string]Resolution{}
	for _, r := range rs {
		byID[r.Hit] = r
	}
	var o Outcome
	for _, h := range hits {
		if r, ok := byID[h.ID]; ok {
			o.Resolved = append(o.Resolved, Resolved{Hit: h, Resolution: r})
			continue
		}
		o.Unresolved = append(o.Unresolved, h)
	}
	return o
}

// Clear reports whether no hit is unresolved.
func (o Outcome) Clear() bool { return len(o.Unresolved) == 0 }

// Used is every resolution that resolved a hit, once each, in hit-id order.
func (o Outcome) Used() []Resolution {
	seen := map[string]bool{}
	var out []Resolution
	for _, r := range o.Resolved {
		if !seen[r.Resolution.Hit] {
			seen[r.Resolution.Hit] = true
			out = append(out, r.Resolution)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hit < out[j].Hit })
	return out
}

// Find returns the hits of the scan with id, resolved or not.
func (o Outcome) Find(id string) []Hit {
	var out []Hit
	for _, h := range o.Unresolved {
		if h.ID == id {
			out = append(out, h)
		}
	}
	for _, r := range o.Resolved {
		if r.Hit.ID == id {
			out = append(out, r.Hit)
		}
	}
	return out
}

// ReportUnresolved lists every unresolved hit, grouped by id, each id with
// the entry that matched and every occurrence with its location and the text
// around it.
func (o Outcome) ReportUnresolved() string {
	var b strings.Builder
	for _, r := range o.UnresolvedReports() {
		b.WriteString("  - " + r + "\n")
	}
	return b.String()
}

// UnresolvedReports are the unresolved hits, one report per id: the id, the
// entry that matched, and every occurrence with its location and the text
// around it, one per line.
func (o Outcome) UnresolvedReports() []string {
	var ids []string
	byID := map[string][]Hit{}
	for _, h := range o.Unresolved {
		if _, ok := byID[h.ID]; !ok {
			ids = append(ids, h.ID)
		}
		byID[h.ID] = append(byID[h.ID], h)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		hs := byID[id]
		var b strings.Builder
		fmt.Fprintf(&b, "hit %s (%s):", id, hs[0].Entry.Describe())
		for _, h := range hs {
			fmt.Fprintf(&b, "\n      %s, line %d, column %d: %s", h.Where, h.Line, h.Column, h.Context)
		}
		out = append(out, b.String())
	}
	return out
}

// ReportResolved lists every resolved hit's id with its resolution: what the
// agent passed over as a false positive (with its certainty and reason), and
// what the owner approved (with the owner's reason).
func (o Outcome) ReportResolved() string {
	var b strings.Builder
	for _, r := range o.Used() {
		switch r.Decision {
		case FalsePositive:
			fmt.Fprintf(&b, "  - hit %s at %s: passed over as a false positive (%d%% certain): %s\n", r.Hit, r.Location, r.Certainty, r.Reason)
		default:
			fmt.Fprintf(&b, "  - hit %s at %s: published with the owner's approval: %s\n", r.Hit, r.Location, r.Reason)
		}
	}
	return b.String()
}

// Refusal is the error a publishing step refuses with while a hit is
// unresolved, or nil: it names what was refused, lists every unresolved hit,
// and ends with fix, the commands that resolve a hit.
func (o Outcome) Refusal(what, fix string) error {
	if o.Clear() {
		return nil
	}
	return errors.New(fmt.Sprintf("%s carries confidential-term hits nothing resolves:\n%s%s", what, o.ReportUnresolved(), fix))
}
