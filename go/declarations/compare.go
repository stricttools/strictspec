package declarations

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// recorder is the LiveReader the engine reads through: it caches every read
// and exposed value of one comparison, so a resource read twice (once by a
// derivation) reads the same, records every read for the plan identifier,
// and collects what the tool's reader got wrong.
type recorder struct {
	live    LiveReader
	types   map[string]ResourceType
	reads   map[string]readResult
	exposed map[string]exposedResult
	bugs    []string
}

type readResult struct {
	resourceType, name string
	r                  Resource
	ok                 bool
	err                error
}

type exposedResult struct {
	v   Value
	ok  bool
	err error
}

func (rc *recorder) List(resourceType string) ([]string, error) {
	return rc.live.List(resourceType)
}

func (rc *recorder) Read(resourceType, name string) (Resource, bool, error) {
	k := resourceType + "/" + name
	if res, ok := rc.reads[k]; ok {
		return res.r, res.ok, res.err
	}
	r, ok, err := rc.live.Read(resourceType, name)
	if err == nil && ok {
		rc.bugs = append(rc.bugs, checkLive(rc.types, resourceType, name, r)...)
	}
	if err != nil || !ok {
		r = Resource{}
	}
	rc.reads[k] = readResult{resourceType: resourceType, name: name, r: r, ok: ok, err: err}
	return r, ok, err
}

func (rc *recorder) Exposed(resourceType, name, value string) (Value, bool, error) {
	k := resourceType + "/" + name + "/" + value
	if res, ok := rc.exposed[k]; ok {
		return res.v, res.ok, res.err
	}
	v, ok, err := rc.live.Exposed(resourceType, name, value)
	if err == nil && ok {
		if cerr := checkValue(v, false); cerr != nil {
			rc.bugs = append(rc.bugs, fmt.Sprintf("the live reader returned for the %s of %s/%s %v, a bug in the tool", value, resourceType, name, cerr))
		}
	}
	rc.exposed[k] = exposedResult{v: v, ok: ok, err: err}
	return v, ok, err
}

// checkLive checks a resource a live reader returned.
func checkLive(types map[string]ResourceType, resourceType, name string, r Resource) []string {
	at := resourceType + "/" + name
	if r.Type != resourceType || r.Name != name {
		return []string{fmt.Sprintf("the live reader returned %s/%s for %s, a bug in the tool", r.Type, r.Name, at)}
	}
	var problems []string
	for _, f := range sortedKeys(r.Fields) {
		if err := checkValue(r.Fields[f], false); err != nil {
			problems = append(problems, fmt.Sprintf("the live reader returned for %s %s %v, a bug in the tool", at, f, err))
		}
	}
	rt := types[resourceType]
	if rt == nil {
		return problems
	}
	for _, c := range rt.Descriptor().Collections {
		for _, p := range checkItems(at+" (live)", c, r.Children[c.Name], false) {
			problems = append(problems, p)
		}
	}
	return problems
}

// slotState is one slot of a target during a comparison: resolved (the
// derivation's value or children), unresolved (its value does not exist
// yet), or skipped (its derivation failed and is reported unreadable). An
// unresolved slot is pending when the plan creates its source or its target
// (after names them): the apply derives it again once they are created.
type slotState struct {
	cause      cause
	derived    Derived
	unresolved bool
	pending    bool
	after      []string
	skipped    bool
}

// deferredSlot is a pending collection slot of the target at index target.
type deferredSlot struct {
	target int
	state  slotState
}

type comparison struct {
	set     Set
	types   map[string]ResourceType
	rc      *recorder
	diffs   []Difference
	managed []UnmanagedMatch
	unread  []Unreadable
	slots   map[int][]slotState
	// deferred are the pending collection slots, in comparison order.
	deferred []deferredSlot
	// owned are the live resources each live resource ("<type>/<name>")
	// owns that nothing declares, claims, or lists unmanaged, in the
	// account repository.
	owned map[string][]Resource
}

// Compare reads every declared resource live, resolves references and
// derivations, and reports the differences, the live children unmanaged
// entries leave alone, and the reads that failed. In
// the account repository (the project's root is the claims' root) it also
// lists every live resource of every resource type and reports each one
// neither declared there, claimed, nor unmanaged; in a project it reports
// each declared resource the claims do not assign to the project.
func Compare(set Set, live LiveReader, claims Claims) (Report, error) {
	_, rep, err := run(set, live, claims)
	return rep, err
}

// run is Compare, also returning the comparison apply builds its changes
// from.
func run(set Set, live LiveReader, claims Claims) (*comparison, Report, error) {
	if err := checkTool(set.Tool); err != nil {
		return nil, Report{}, err
	}
	if err := checkNotImporting(set); err != nil {
		return nil, Report{}, err
	}
	account, err := samePath(set.Project.Root, claims.Root)
	if err != nil {
		return nil, Report{}, err
	}
	if err := checkClaimsPlacement(set, claims, account); err != nil {
		return nil, Report{}, err
	}
	c := &comparison{
		set:   set,
		types: typesByName(set.Tool),
		slots: map[int][]slotState{},
	}
	c.rc = &recorder{live: live, types: c.types, reads: map[string]readResult{}, exposed: map[string]exposedResult{}}
	if err := c.derive(); err != nil {
		return nil, Report{}, err
	}
	var dups []string
	for i := range set.Resources {
		dups = append(dups, c.compareResource(i)...)
	}
	if len(dups) > 0 {
		return nil, Report{}, refusal("the expected children", dups)
	}
	if account {
		c.unclaimed(claims)
	} else {
		c.misclaimed(claims)
	}
	if len(c.rc.bugs) > 0 {
		return nil, Report{}, refusal("the live state read", c.rc.bugs)
	}
	return c, c.report(), nil
}

// checkNotImporting refuses a set LoadForImport returned: it may hold slots
// whose target is not declared.
func checkNotImporting(set Set) error {
	if set.importing != nil {
		return fmt.Errorf("the set was loaded by LoadForImport for %s/%s and serves RenderImport only: load it with Load to compare or apply", set.importing.resourceType, set.importing.name)
	}
	return nil
}

// samePath reports whether the project's root is the account repository's,
// comparing the paths with every symlink resolved. A path that does not
// resolve is an error.
func samePath(projectRoot, accountRoot string) (bool, error) {
	if projectRoot == "" {
		return false, fmt.Errorf("the set's project has no root")
	}
	if accountRoot == "" {
		return false, fmt.Errorf("the claims carry no account repository path: load them with LoadClaims")
	}
	a, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return false, fmt.Errorf("the project's root %s: %w", projectRoot, err)
	}
	b, err := filepath.EvalSymlinks(accountRoot)
	if err != nil {
		return false, fmt.Errorf("the account repository's path %s: %w", accountRoot, err)
	}
	return a == b, nil
}

// checkClaimsPlacement refuses a claims.toml outside the account repository,
// and, in the account repository, a claim or unmanaged entry naming a
// resource the account repository declares itself.
func checkClaimsPlacement(set Set, claims Claims, account bool) error {
	if set.hasClaims && !account {
		return fmt.Errorf("%s/%s: claims.toml belongs only in the account repository (%s): move its entries into that repository's claims.toml and delete this one", dirRel(set.Tool), claimsFile, claims.Root)
	}
	if !account {
		return nil
	}
	var problems []string
	check := func(resourceType, name, place string) {
		if i, ok := set.index(resourceType, name); ok {
			problems = append(problems, fmt.Sprintf("%s/%s is declared by %s and listed in claims.toml as %s: a resource the account repository declares needs no other entry; remove %s", resourceType, name, set.Resources[i].File, place, place))
		}
	}
	for i, cl := range claims.Claims {
		check(cl.ResourceType, cl.Name, fmt.Sprintf("claim[%d]", i))
	}
	for i, u := range claims.Unmanaged {
		check(u.ResourceType, u.Name, fmt.Sprintf("unmanaged[%d]", i))
	}
	if len(problems) > 0 {
		return refusal(filepath.Join(claims.Root, dirRel(set.Tool), claimsFile), problems)
	}
	return nil
}

func sameMatchers(a, b map[string]Matcher) bool {
	if len(a) != len(b) {
		return false
	}
	for k, m := range a {
		o, ok := b[k]
		if !ok || m.isPrefix != o.isPrefix || m.prefix != o.prefix || (!m.isPrefix && !equal(m.exact, o.exact)) {
			return false
		}
	}
	return true
}

func sameSlot(a, b Slot) bool {
	return a.ResourceType == b.ResourceType && a.Name == b.Name && a.Field == b.Field && a.Collection == b.Collection && sameMatchers(a.Match, b.Match)
}

// derive runs every cause's Derive once and files each Derived under its
// target.
func (c *comparison) derive() error {
	bySource := map[int][]cause{}
	var sources []int
	for _, cs := range c.set.causes {
		if bySource[cs.source] == nil {
			sources = append(sources, cs.source)
		}
		bySource[cs.source] = append(bySource[cs.source], cs)
	}
	var bugs []string
	for _, si := range sources {
		r := c.set.Resources[si]
		causes := bySource[si]
		ds, err := c.types[r.Type].Derive(r, c.rc)
		file := func(st slotState) {
			ti, _ := c.set.index(st.cause.slot.ResourceType, st.cause.slot.Name)
			c.slots[ti] = append(c.slots[ti], st)
		}
		if err != nil {
			c.unreadable(r.Type, r.Name, err)
			for _, cs := range causes {
				file(slotState{cause: cs, skipped: true})
			}
			continue
		}
		used := make([]bool, len(ds))
		for _, cs := range causes {
			found := -1
			for j, d := range ds {
				if !used[j] && sameSlot(d.Slot, cs.slot) {
					found = j
					break
				}
			}
			if found < 0 {
				bugs = append(bugs, fmt.Sprintf("%s: Derive returned no value for %s of %s/%s, a bug in the tool", r.File, describeSlot(cs.slot), cs.slot.ResourceType, cs.slot.Name))
				continue
			}
			used[found] = true
			d := ds[found]
			if p := checkDerived(r, cs.slot, d, c.types); p != nil {
				bugs = append(bugs, p...)
				continue
			}
			st := slotState{cause: cs, derived: d, unresolved: d.Unresolved != ""}
			if st.unresolved {
				st.after = c.created(source(r), cs.slot.ResourceType+"/"+cs.slot.Name)
				st.pending = len(st.after) > 0
			}
			file(st)
		}
		for j, u := range used {
			if !u {
				bugs = append(bugs, fmt.Sprintf("%s: Derive returned a value for %s of %s/%s, which is not one of its slots, a bug in the tool", r.File, describeSlot(ds[j].Slot), ds[j].Slot.ResourceType, ds[j].Slot.Name))
			}
		}
	}
	if len(bugs) > 0 {
		return refusal("the derivations", bugs)
	}
	return nil
}

func checkDerived(r Resource, sl Slot, d Derived, types map[string]ResourceType) []string {
	if d.Unresolved != "" {
		return nil
	}
	at := fmt.Sprintf("%s: the derived %s of %s/%s", r.File, describeSlot(sl), sl.ResourceType, sl.Name)
	if sl.Field != "" {
		if d.Value == nil || d.Items != nil {
			return []string{at + " is not one value, a bug in the tool"}
		}
		if err := checkValue(d.Value, false); err != nil {
			return []string{fmt.Sprintf("%s holds %v, a bug in the tool", at, err)}
		}
		return nil
	}
	if d.Value != nil {
		return []string{at + " is a value where children belong, a bug in the tool"}
	}
	col, _ := findCollection(types[sl.ResourceType].Descriptor(), sl.Collection)
	problems := checkItems(at, col, d.Items, false)
	for i, it := range d.Items {
		if !matchesAll(it, sl.Match) {
			problems = append(problems, fmt.Sprintf("%s: %s[%d] does not match %s, a bug in the tool", at, sl.Collection, i, describeMatchers(sl.Match)))
		}
	}
	return problems
}

// created names those of the declared resources (each "<type>/<name>") that
// are not live, which the plan creates. A resource whose read fails is not
// named: the failed read is reported where the resource is compared.
func (c *comparison) created(resources ...string) []string {
	var out []string
	for _, k := range resources {
		t, n, _ := strings.Cut(k, "/")
		if _, declared := c.set.index(t, n); !declared {
			continue
		}
		if _, ok, err := c.rc.Read(t, n); err == nil && !ok && !contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (c *comparison) unreadable(resourceType, name string, err error) {
	for _, u := range c.unread {
		if u.ResourceType == resourceType && u.Name == name && u.Error == err.Error() {
			return
		}
	}
	c.unread = append(c.unread, Unreadable{ResourceType: resourceType, Name: name, Error: err.Error()})
}

// resolve replaces every reference in v with its exposed value, or with a
// Pending when the referenced resource is not live (the plan creates it).
// unresolved is true when one does not exist yet and the plan does not
// create it (v is then returned as written); a failed read is an error.
func (c *comparison) resolve(v Value) (Value, bool, error) {
	switch x := v.(type) {
	case Reference:
		val, ok, err := c.rc.Exposed(x.ResourceType, x.Name, x.Value)
		if err != nil {
			c.unreadable(x.ResourceType, x.Name, err)
			return nil, false, err
		}
		if !ok {
			if after := c.created(x.ResourceType + "/" + x.Name); len(after) > 0 {
				ref := x
				return Pending{text: fmt.Sprintf("%s of new %s %s", x.Value, x.ResourceType, x.Name), after: after, ref: &ref}, false, nil
			}
			return v, true, nil
		}
		return val, false, nil
	case []Value:
		out := make([]Value, len(x))
		unresolved := false
		for i, e := range x {
			r, u, err := c.resolve(e)
			if err != nil {
				return nil, false, err
			}
			out[i], unresolved = r, unresolved || u
		}
		if unresolved {
			return v, true, nil
		}
		return out, false, nil
	case map[string]Value:
		out := make(map[string]Value, len(x))
		unresolved := false
		for _, k := range sortedKeys(x) {
			r, u, err := c.resolve(x[k])
			if err != nil {
				return nil, false, err
			}
			out[k], unresolved = r, unresolved || u
		}
		if unresolved {
			return v, true, nil
		}
		return out, false, nil
	}
	return v, false, nil
}

// expectedField is one top-level field's expected value. declared is what
// the report shows: the value, the reference as written, or why a derived
// value does not exist yet.
type expectedField struct {
	value      Value
	unresolved bool
	source     string
}

// expectedItem is one expected child; unresolved names its fields (key
// fields included) that wait on a value that does not exist yet.
type expectedItem struct {
	item       Item
	unresolved map[string]bool
	source     string
}

func (e expectedItem) keyUnresolved(col Collection) bool {
	for _, k := range keyFields(col, e.item) {
		if e.unresolved[k] {
			return true
		}
	}
	return false
}

// expected builds the resource's expected fields and children: the hand
// declaration with references resolved, and every resolved slot's value.
// ok is false when a read failed (reported unreadable).
func (c *comparison) expected(i int) (map[string]expectedField, map[string][]expectedItem, bool) {
	r := c.set.Resources[i]
	fields := map[string]expectedField{}
	for _, f := range sortedKeys(r.Fields) {
		v, u, err := c.resolve(r.Fields[f])
		if err != nil {
			return nil, nil, false
		}
		fields[f] = expectedField{value: v, unresolved: u, source: source(r)}
	}
	children := map[string][]expectedItem{}
	for _, col := range sortedKeys(r.Children) {
		for _, it := range r.Children[col] {
			e := expectedItem{item: Item{Fields: map[string]Value{}}, unresolved: map[string]bool{}, source: source(r)}
			for _, f := range sortedKeys(it.Fields) {
				v, u, err := c.resolve(it.Fields[f])
				if err != nil {
					return nil, nil, false
				}
				e.item.Fields[f] = v
				if u {
					e.unresolved[f] = true
				}
			}
			children[col] = append(children[col], e)
		}
	}
	for _, st := range c.slots[i] {
		sl := st.cause.slot
		src := c.set.sourceOf(st.cause)
		switch {
		case st.skipped:
		case sl.Field != "" && st.pending:
			// The field is written by its own resource's create or update,
			// so it waits on its source only.
			cs := st.cause
			srcRes := c.set.Resources[cs.source]
			var after []string
			for _, a := range st.after {
				if a != sl.ResourceType+"/"+sl.Name {
					after = append(after, a)
				}
			}
			if len(after) == 0 {
				fields[sl.Field] = expectedField{value: st.derived.Unresolved, unresolved: true, source: src}
				break
			}
			fields[sl.Field] = expectedField{value: Pending{text: fmt.Sprintf("%s %s %s derives", sl.Field, srcRes.Type, srcRes.Name), after: after, cause: &cs}, source: src}
		case sl.Field != "" && st.unresolved:
			fields[sl.Field] = expectedField{value: st.derived.Unresolved, unresolved: true, source: src}
		case sl.Field != "":
			fields[sl.Field] = expectedField{value: st.derived.Value, source: src}
		case !st.unresolved:
			for _, it := range st.derived.Items {
				children[sl.Collection] = append(children[sl.Collection], expectedItem{item: it, unresolved: map[string]bool{}, source: src})
			}
		}
	}
	return fields, children, true
}

func (c *comparison) add(d Difference) { c.diffs = append(c.diffs, d) }

// sameKeys names every two expected children of one collection with equal
// keys (as resolved): each would be compared with one live child, giving
// contradictory differences.
func sameKeys(r Resource, desc Descriptor, children map[string][]expectedItem) []string {
	var problems []string
	for _, col := range desc.Collections {
		seen := map[string]expectedItem{}
		for _, e := range children[col.Name] {
			if e.keyUnresolved(col) {
				continue
			}
			k := keyJSON(col, e.item)
			if o, dup := seen[k]; dup {
				problems = append(problems, fmt.Sprintf("%s and %s both expect the %s %s of %s/%s: a child has one owning file and is declared once; change or remove one of them", o.source, e.source, col.Name, k, r.Type, r.Name))
				continue
			}
			seen[k] = e
		}
	}
	return problems
}

// compareResource compares one declared resource with its live state; it
// returns the expected children sharing a key, which refuse the comparison.
func (c *comparison) compareResource(i int) []string {
	r := c.set.Resources[i]
	rt := c.types[r.Type]
	desc := rt.Descriptor()
	lr, exists, err := c.rc.Read(r.Type, r.Name)
	if err != nil {
		c.unreadable(r.Type, r.Name, err)
		return nil
	}
	fields, children, ok := c.expected(i)
	if !ok {
		return nil
	}
	if dups := sameKeys(r, desc, children); len(dups) > 0 {
		return dups
	}
	// A field whose derivation failed (reported unreadable) is not compared,
	// as the children of a failed derivation are left alone; an unresolved
	// one gives its unresolved difference only.
	failed := map[string]bool{}
	for _, st := range c.slots[i] {
		if st.unresolved && st.cause.slot.Collection != "" {
			status := StatusUnresolved
			if st.pending {
				status = StatusMissing
				c.deferred = append(c.deferred, deferredSlot{target: i, state: st})
			}
			c.add(Difference{Status: status, ResourceType: r.Type, Name: r.Name, Collection: st.cause.slot.Collection, Declared: st.derived.Unresolved, Source: c.set.sourceOf(st.cause)})
		}
		if st.skipped && st.cause.slot.Field != "" {
			failed[st.cause.slot.Field] = true
		}
	}
	if !exists {
		c.missingResource(r, desc, fields, children)
		return nil
	}
	for _, f := range desc.Fields {
		if failed[f] {
			continue
		}
		e, has := fields[f]
		lv, lhas := lr.Fields[f]
		d := Difference{ResourceType: r.Type, Name: r.Name, Field: f, Source: source(r)}
		if has {
			d.Declared, d.Source = e.value, e.source
		}
		if lhas {
			d.Live = lv
		}
		switch {
		case has && e.unresolved:
			d.Status = StatusUnresolved
		case has != lhas || (has && !equal(e.value, lv)):
			d.Status = StatusChanged
		default:
			continue
		}
		c.add(d)
	}
	for _, col := range desc.Collections {
		c.compareCollection(r, col, children[col.Name], lr.Children[col.Name], c.slots[i])
	}
	return nil
}

func (c *comparison) missingResource(r Resource, desc Descriptor, fields map[string]expectedField, children map[string][]expectedItem) {
	declared := map[string]Value{}
	waiting := false
	for _, f := range sortedKeys(fields) {
		declared[f] = fields[f].value
		waiting = waiting || fields[f].unresolved
	}
	status := StatusMissing
	if waiting {
		status = StatusUnresolved
	}
	c.add(Difference{Status: status, ResourceType: r.Type, Name: r.Name, Declared: declared, Source: source(r)})
	for _, col := range desc.Collections {
		for _, e := range children[col.Name] {
			st := status
			if len(e.unresolved) > 0 {
				st = StatusUnresolved
			}
			c.add(Difference{Status: st, ResourceType: r.Type, Name: r.Name, Collection: col.Name, Key: itemKey(col, e.item), Declared: nonKeyFields(col, e.item), Source: e.source})
		}
	}
}

func (c *comparison) compareCollection(r Resource, col Collection, expected []expectedItem, live []Item, slots []slotState) {
	liveByKey := map[string]Item{}
	for _, it := range live {
		liveByKey[keyJSON(col, it)] = it
	}
	matched := map[string]bool{}
	for _, e := range expected {
		d := Difference{ResourceType: r.Type, Name: r.Name, Collection: col.Name, Key: itemKey(col, e.item), Source: e.source}
		if e.keyUnresolved(col) {
			d.Status, d.Declared = StatusUnresolved, nonKeyFields(col, e.item)
			c.add(d)
			continue
		}
		k := keyJSON(col, e.item)
		li, ok := liveByKey[k]
		if !ok {
			d.Status, d.Declared = StatusMissing, nonKeyFields(col, e.item)
			if len(e.unresolved) > 0 {
				d.Status = StatusUnresolved
			}
			c.add(d)
			continue
		}
		matched[k] = true
		for _, f := range col.Fields {
			ev, ehas := e.item.Fields[f]
			lv, lhas := li.Fields[f]
			fd := d
			fd.Field = f
			if ehas {
				fd.Declared = ev
			}
			if lhas {
				fd.Live = lv
			}
			switch {
			case e.unresolved[f]:
				fd.Status = StatusUnresolved
			case ehas != lhas || (ehas && !equal(ev, lv)):
				fd.Status = StatusChanged
			default:
				continue
			}
			c.add(fd)
		}
	}
	for _, li := range live {
		if matched[keyJSON(col, li)] {
			continue
		}
		d := Difference{Status: StatusUnexpected, ResourceType: r.Type, Name: r.Name, Collection: col.Name, Key: itemKey(col, li), Live: nonKeyFields(col, li), Source: source(r)}
		covered := false
		for _, st := range slots {
			sl := st.cause.slot
			if sl.Collection == col.Name && matchesAll(li, sl.Match) {
				covered = true
				if !st.unresolved && !st.skipped {
					d.Source = c.set.sourceOf(st.cause)
					c.add(d)
				}
				break
			}
		}
		if covered {
			continue
		}
		left := false
		for _, u := range r.Unmanaged {
			if u.Collection == col.Name && matchesAll(li, exactMatchers(u.Match)) {
				c.managed = append(c.managed, UnmanagedMatch{ResourceType: r.Type, Name: r.Name, Collection: col.Name, Key: itemKey(col, li), Reason: u.Reason})
				left = true
				break
			}
		}
		if !left {
			c.add(d)
		}
	}
}

// unclaimed lists every live resource of every resource type, in the account
// repository, and reads each one neither declared there, claimed, nor
// unmanaged: it is unclaimed, unless its owner (Owning) is live, when it goes
// with its owner (in owned, under the owner, when the owner is unclaimed).
func (c *comparison) unclaimed(claims Claims) {
	claimed := map[string]bool{}
	for _, cl := range claims.Claims {
		claimed[cl.ResourceType+"/"+cl.Name] = true
	}
	unmanaged := map[string]string{}
	for _, u := range claims.Unmanaged {
		unmanaged[u.ResourceType+"/"+u.Name] = u.Reason
	}
	type candidate struct {
		resourceType, name string
		owner              string
		live               Resource
	}
	var candidates []candidate
	listed := map[string]bool{}
	for _, rt := range c.set.Tool.ResourceTypes {
		names, err := c.rc.List(rt.Name())
		if err != nil {
			c.unreadable(rt.Name(), "", err)
			continue
		}
		for _, n := range names {
			k := rt.Name() + "/" + n
			listed[k] = true
			if _, declared := c.set.index(rt.Name(), n); declared || claimed[k] {
				continue
			}
			if reason, ok := unmanaged[k]; ok {
				c.managed = append(c.managed, UnmanagedMatch{ResourceType: rt.Name(), Name: n, Reason: reason})
				continue
			}
			r, ok, err := c.rc.Read(rt.Name(), n)
			if err != nil {
				c.unreadable(rt.Name(), n, err)
				continue
			}
			if !ok {
				// Listed, then gone before it was read.
				continue
			}
			cand := candidate{resourceType: rt.Name(), name: n, live: r}
			if o, ok := rt.(Owning); ok {
				if ot, on, owned := o.Owner(r); owned {
					cand.owner = ot + "/" + on
				}
			}
			candidates = append(candidates, cand)
		}
	}
	c.owned = map[string][]Resource{}
	for _, cand := range candidates {
		if cand.owner != "" && listed[cand.owner] {
			c.owned[cand.owner] = append(c.owned[cand.owner], cand.live)
			continue
		}
		c.add(Difference{Status: StatusUnclaimed, ResourceType: cand.resourceType, Name: cand.name})
	}
}

// misclaimed reports each declared resource the claims do not assign to the
// project.
func (c *comparison) misclaimed(claims Claims) {
	owner := map[string]string{}
	for _, cl := range claims.Claims {
		owner[cl.ResourceType+"/"+cl.Name] = cl.Project
	}
	for _, r := range c.set.Resources {
		if owner[source(r)] != c.set.Project.Name {
			c.add(Difference{Status: StatusMisclaimed, ResourceType: r.Type, Name: r.Name, Source: source(r)})
		}
	}
}

func keyString(k map[string]Value) string {
	if k == nil {
		return ""
	}
	return valueJSON(Value(k))
}

func (c *comparison) report() Report {
	sort.SliceStable(c.diffs, func(i, j int) bool {
		a, b := c.diffs[i], c.diffs[j]
		ka := []string{a.ResourceType, a.Name, a.Collection, keyString(a.Key), a.Field, string(a.Status), optionalJSON(a.Declared), optionalJSON(a.Live), a.Source}
		kb := []string{b.ResourceType, b.Name, b.Collection, keyString(b.Key), b.Field, string(b.Status), optionalJSON(b.Declared), optionalJSON(b.Live), b.Source}
		return lessStrings(ka, kb)
	})
	sort.SliceStable(c.managed, func(i, j int) bool {
		a, b := c.managed[i], c.managed[j]
		return lessStrings([]string{a.ResourceType, a.Name, a.Collection, keyString(a.Key)}, []string{b.ResourceType, b.Name, b.Collection, keyString(b.Key)})
	})
	sort.SliceStable(c.unread, func(i, j int) bool {
		a, b := c.unread[i], c.unread[j]
		return lessStrings([]string{a.ResourceType, a.Name, a.Error}, []string{b.ResourceType, b.Name, b.Error})
	})
	rep := Report{
		Tool:        c.set.Tool.Name,
		Project:     c.set.Project.Name,
		Root:        c.set.Project.Root,
		Directory:   dirRel(c.set.Tool),
		Differences: append([]Difference{}, c.diffs...),
		Unmanaged:   append([]UnmanagedMatch{}, c.managed...),
		Unreadable:  append([]Unreadable{}, c.unread...),
	}
	rep.Matching = len(rep.Differences) == 0 && len(rep.Unreadable) == 0
	return rep
}

func optionalJSON(v Value) string {
	if v == nil {
		return ""
	}
	return valueJSON(v)
}

func lessStrings(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// planID is the hex SHA-256 of the canonical JSON of every declaration
// file's hash, every live resource read (exposed values excluded), sorted by
// resource type then name, and the differences.
func (c *comparison) planID(rep Report) string {
	decl := map[string]any{}
	for p, h := range c.set.files {
		decl[p] = h
	}
	reads := make([]readResult, 0, len(c.rc.reads))
	for _, res := range c.rc.reads {
		reads = append(reads, res)
	}
	sort.Slice(reads, func(i, j int) bool {
		return lessStrings([]string{reads[i].resourceType, reads[i].name}, []string{reads[j].resourceType, reads[j].name})
	})
	live := []any{}
	for _, res := range reads {
		e := map[string]any{"resource_type": res.resourceType, "name": res.name, "exists": res.ok}
		if res.err != nil {
			e["error"] = res.err.Error()
		}
		if res.ok {
			e["fields"] = jsonData(Value(res.r.Fields))
			children := map[string]any{}
			for col, items := range res.r.Children {
				list := []any{}
				for _, it := range items {
					list = append(list, jsonData(Value(it.Fields)))
				}
				children[col] = list
			}
			e["children"] = children
		}
		live = append(live, e)
	}
	diffs := []any{}
	for _, d := range rep.Differences {
		diffs = append(diffs, d.jsonData())
	}
	return hashHex([]byte(canonicalJSON(map[string]any{"declarations": decl, "live": live, "differences": diffs})))
}

func (d Difference) jsonData() map[string]any {
	m := map[string]any{"status": string(d.Status), "resource_type": d.ResourceType, "name": d.Name}
	if d.Collection != "" {
		m["collection"] = d.Collection
	}
	if d.Key != nil {
		m["key"] = jsonData(Value(d.Key))
	}
	if d.Field != "" {
		m["field"] = d.Field
	}
	if d.Declared != nil {
		m["declared"] = jsonData(d.Declared)
	}
	if d.Live != nil {
		m["live"] = jsonData(d.Live)
	}
	if d.Source != "" {
		m["source"] = d.Source
	}
	return m
}

func (u UnmanagedMatch) jsonData() map[string]any {
	m := map[string]any{"resource_type": u.ResourceType, "name": u.Name, "reason": u.Reason}
	if u.Collection != "" {
		m["collection"] = u.Collection
	}
	if u.Key != nil {
		m["key"] = jsonData(Value(u.Key))
	}
	return m
}

func (u Unreadable) jsonData() map[string]any {
	m := map[string]any{"resource_type": u.ResourceType, "error": u.Error}
	if u.Name != "" {
		m["name"] = u.Name
	}
	return m
}

// MarshalJSON renders the report as a declaration-report document.
func (r Report) MarshalJSON() ([]byte, error) {
	diffs := []any{}
	for _, d := range r.Differences {
		diffs = append(diffs, d.jsonData())
	}
	managed := []any{}
	for _, u := range r.Unmanaged {
		managed = append(managed, u.jsonData())
	}
	unread := []any{}
	for _, u := range r.Unreadable {
		unread = append(unread, u.jsonData())
	}
	return []byte(canonicalJSON(map[string]any{
		"format_version": 1,
		"tool":           r.Tool,
		"project":        r.Project,
		"root":           r.Root,
		"directory":      r.Directory,
		"matching":       r.Matching,
		"differences":    diffs,
		"unmanaged":      managed,
		"unreadable":     unread,
	})), nil
}

// ExitCode is a verify's exit code for the report: ExitFailed when a live
// read failed, else ExitDifferences when there is a difference, else
// ExitMatching.
func (r Report) ExitCode() int {
	switch {
	case len(r.Unreadable) > 0:
		return ExitFailed
	case len(r.Differences) > 0:
		return ExitDifferences
	}
	return ExitMatching
}
