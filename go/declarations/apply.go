package declarations

import (
	"fmt"
	"sort"
	"strings"
)

// Apply executes the differences verify reported under diffID. It refuses,
// in order: while the declaration directory has an uncommitted or untracked
// file; unless comparing again gives diffID; while a resource is misclaimed;
// while a live read fails. It then executes creates and updates in ascending
// Stage, then deletes in descending Stage, and never executes an unresolved
// or misclaimed difference. In the account repository each unclaimed
// resource is deleted whole, with the deletes; in a project apply never
// deletes a resource, only children (a live resource a project stops
// declaring is left alone, and the account repository reports it unclaimed).
// It returns the report computed after applying, where values this apply
// created resolve: the next stage.
func Apply(set Set, live LiveReader, claims Claims, w Writer, g Git, diffID string) (Report, error) {
	if err := checkTool(set.Tool); err != nil {
		return Report{}, err
	}
	if err := checkNotImporting(set); err != nil {
		return Report{}, err
	}
	dir := dirRel(set.Tool)
	out, err := g.Status(set.Project.Root, dir)
	if err != nil {
		return Report{}, fmt.Errorf("git status of %s: %w", dir, err)
	}
	var dirty []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			dirty = append(dirty, l)
		}
	}
	if len(dirty) > 0 {
		return Report{}, fmt.Errorf("%s has uncommitted or untracked files; apply runs only what is committed: commit them, then run verify again and pass its diff:\n  %s", dir, strings.Join(dirty, "\n  "))
	}
	c, rep, err := run(set, live, claims)
	if err != nil {
		return Report{}, err
	}
	if rep.DiffID != diffID {
		return Report{}, fmt.Errorf("the live state or the declarations changed since verify printed %s: run verify again and pass its new diff", diffID)
	}
	var problems []string
	for _, d := range rep.Differences {
		if d.Status == StatusMisclaimed {
			problems = append(problems, fmt.Sprintf("%s/%s (misclaimed: the claims assign it to another project or to none)", d.ResourceType, d.Name))
		}
	}
	if len(problems) > 0 {
		return Report{}, fmt.Errorf("apply refuses while a declared resource is misclaimed: claim each for %s in the account repository's claims.toml, or stop declaring it here, then run verify again:\n  %s", set.Project.Name, strings.Join(problems, "\n  "))
	}
	for _, u := range rep.Unreadable {
		what := u.ResourceType
		if u.Name != "" {
			what += "/" + u.Name
		}
		problems = append(problems, what+": "+u.Error)
	}
	if len(problems) > 0 {
		return Report{}, fmt.Errorf("apply refuses while a live read fails, rather than guess at what it would have read: run verify again once they read, and pass its diff:\n  %s", strings.Join(problems, "\n  "))
	}

	changes := c.changes(rep)
	for i, ch := range changes {
		if err := w.Apply(ch); err != nil {
			return Report{}, fmt.Errorf("apply stopped at %s: %w; %d change(s) were applied before it: run verify again", describeChange(ch), err, i)
		}
	}
	return Compare(set, live, claims)
}

func describeChange(ch Change) string {
	s := fmt.Sprintf("the %s of %s/%s", ch.Action, ch.ResourceType, ch.Name)
	if ch.Collection != "" {
		s += fmt.Sprintf(" %s %s", ch.Collection, valueJSON(Value(ch.Key)))
	}
	if ch.Field != "" {
		s += " field " + ch.Field
	}
	return s
}

// changes turns the executable differences into changes, in execution
// order.
func (c *comparison) changes(rep Report) []Change {
	resources := map[int]Resource{}
	expectedResource := func(i int) Resource {
		if r, ok := resources[i]; ok {
			return r
		}
		r := c.set.Resources[i]
		desc := c.types[r.Type].Descriptor()
		fields, children, _ := c.expected(i)
		// The comparison read the live resource already: this read is its
		// cached result, and a failed one refused apply before changes run.
		lr, _, _ := c.rc.Read(r.Type, r.Name)
		out := Resource{Type: r.Type, Name: r.Name, Fields: map[string]Value{}, Children: map[string][]Item{}, Unmanaged: r.Unmanaged, File: r.File}
		for f, e := range fields {
			if !e.unresolved {
				out.Fields[f] = e.value
			}
		}
		for col, items := range children {
			cd, _ := findCollection(desc, col)
			out.Children[col] = []Item{}
			for _, e := range items {
				if it, ok := resolvedItem(cd, e, lr.Children[col]); ok {
					out.Children[col] = append(out.Children[col], it)
				}
			}
		}
		resources[i] = out
		return out
	}
	var upserts, deletes []Change
	for _, d := range rep.Differences {
		var action Action
		switch d.Status {
		case StatusMissing:
			action = ActionCreate
		case StatusChanged:
			action = ActionUpdate
		case StatusUnexpected:
			action = ActionDelete
		case StatusUnclaimed:
			// Only the account repository's comparison reports unclaimed
			// resources: each is deleted whole. The resource is declared
			// nowhere, so the change carries only its type and name.
			rt := c.types[d.ResourceType]
			deletes = append(deletes, Change{Action: ActionDelete, ResourceType: d.ResourceType, Name: d.Name, Resource: Resource{Type: d.ResourceType, Name: d.Name, Fields: map[string]Value{}, Children: map[string][]Item{}}, Stage: rt.Descriptor().Stage})
			continue
		default:
			continue
		}
		i, _ := c.set.index(d.ResourceType, d.Name)
		rt := c.types[d.ResourceType]
		ch := Change{Action: action, ResourceType: d.ResourceType, Name: d.Name, Collection: d.Collection, Key: d.Key, Field: d.Field, Resource: expectedResource(i), Stage: rt.Descriptor().Stage}
		if d.Collection != "" {
			col, _ := findCollection(rt.Descriptor(), d.Collection)
			ch.Item = itemFor(col, d, ch.Resource)
		}
		if action == ActionDelete {
			deletes = append(deletes, ch)
		} else {
			upserts = append(upserts, ch)
		}
	}
	sort.SliceStable(upserts, func(i, j int) bool { return upserts[i].Stage < upserts[j].Stage })
	sort.SliceStable(deletes, func(i, j int) bool { return deletes[i].Stage > deletes[j].Stage })
	return append(upserts, deletes...)
}

// resolvedItem is the expected child a change carries: the expected item
// itself when nothing in it is unresolved; when its key is resolved and it is
// live, the expected item with each unresolved field replaced by the live
// child's value (left out when the live child has none), so a writer never
// writes an unresolved value; otherwise none (false).
func resolvedItem(col Collection, e expectedItem, live []Item) (Item, bool) {
	if len(e.unresolved) == 0 {
		return e.item, true
	}
	if e.keyUnresolved(col) {
		return Item{}, false
	}
	k := keyJSON(col, e.item)
	for _, li := range live {
		if keyJSON(col, li) != k {
			continue
		}
		it := Item{Fields: map[string]Value{}}
		for f, v := range e.item.Fields {
			if !e.unresolved[f] {
				it.Fields[f] = v
			}
		}
		for f := range e.unresolved {
			if v, ok := li.Fields[f]; ok {
				it.Fields[f] = v
			}
		}
		return it, true
	}
	return Item{}, false
}

// itemFor is the change's item: the live child for a delete, else the
// expected child with the difference's key.
func itemFor(col Collection, d Difference, expected Resource) Item {
	if d.Status == StatusUnexpected {
		it := Item{Fields: map[string]Value{}}
		for k, v := range d.Key {
			it.Fields[k] = v
		}
		if live, ok := d.Live.(map[string]Value); ok {
			for k, v := range live {
				it.Fields[k] = v
			}
		}
		return it
	}
	want := valueJSON(Value(d.Key))
	for _, it := range expected.Children[col.Name] {
		if keyJSON(col, it) == want {
			return it
		}
	}
	return Item{}
}
