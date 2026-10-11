package declarations

import (
	"fmt"
	"sort"
	"strings"
)

// Pending stands for a value an earlier change of the same plan creates: a
// reference to a provider-assigned value of a resource the plan creates (a
// new server's ipv4), or a derived field whose derivation needs a resource
// the plan creates (the nameservers a new zone is assigned). The plan holds
// it where the value goes; Execute replaces it with the value once the
// resources it waits on are created, and a dry run shows its Placeholder.
type Pending struct {
	text  string
	after []string // "<type>/<name>" of each resource whose creation it waits on
	ref   *Reference
	cause *cause
}

// Placeholder is the pending value in words, between angle brackets, such as
// "<ipv4 of new hetzner-server web-1>".
func (p Pending) Placeholder() string { return "<" + p.text + ">" }

// After names, as "<resource type>/<name>", each resource the plan creates
// that the value waits on.
func (p Pending) After() []string { return append([]string(nil), p.after...) }

// Deferred is set on a change whose children a derivation gives only once
// earlier changes of the same plan create what it reads (a new mail
// domain's records, which the mail provider names once the domain exists).
// Execute derives them then and passes the writer one change per child to
// create, update, or delete so the slot's children match the derivation;
// the deferred change itself never reaches the writer.
type Deferred struct {
	// Source is the declaration file (<type>/<name>) causing the children.
	Source string
	// Slot is what the source causes in the change's resource.
	Slot Slot
	// Why is the derivation's reason it cannot give them yet.
	Why string
	// After names, as "<resource type>/<name>", each resource the plan
	// creates that the derivation waits on.
	After []string
	// Covered are the live children the slot covers as read for the plan:
	// the apply updates or deletes each to match the derivation.
	Covered []Item

	cause cause
}

// Placeholders is v with every Pending replaced by its Placeholder, for a
// dry run that records each write with the values it does not know yet.
func Placeholders(v Value) Value {
	switch x := v.(type) {
	case Pending:
		return x.Placeholder()
	case []Value:
		out := make([]Value, len(x))
		for i, e := range x {
			out[i] = Placeholders(e)
		}
		return out
	case map[string]Value:
		out := make(map[string]Value, len(x))
		for k, e := range x {
			out[k] = Placeholders(e)
		}
		return out
	}
	return v
}

// WithPlaceholders is the change with every Pending in its resource, item,
// and key replaced by its Placeholder.
func (ch Change) WithPlaceholders() Change {
	out := ch
	out.Key = placeholderMap(ch.Key)
	out.Item = Item{Fields: placeholderMap(ch.Item.Fields)}
	out.Resource = ch.Resource
	out.Resource.Fields = placeholderMap(ch.Resource.Fields)
	if ch.Resource.Children != nil {
		out.Resource.Children = map[string][]Item{}
		for col, items := range ch.Resource.Children {
			list := []Item{}
			for _, it := range items {
				list = append(list, Item{Fields: placeholderMap(it.Fields)})
			}
			out.Resource.Children[col] = list
		}
	}
	return out
}

func placeholderMap(m map[string]Value) map[string]Value {
	if m == nil {
		return nil
	}
	out := make(map[string]Value, len(m))
	for k, v := range m {
		out[k] = Placeholders(v)
	}
	return out
}

func walkPending(v Value, f func(Pending)) {
	switch x := v.(type) {
	case Pending:
		f(x)
	case []Value:
		for _, e := range x {
			walkPending(e, f)
		}
	case map[string]Value:
		for _, e := range x {
			walkPending(e, f)
		}
	}
}

func hasPending(v Value) bool {
	found := false
	walkPending(v, func(Pending) { found = true })
	return found
}

// needs names the resources whose creation a change waits on: those of the
// pending values in what the change writes (a resource create's fields, an
// updated field, a child and its key), and a deferred change's.
func (ch Change) needs() []string {
	set := map[string]bool{}
	add := func(v Value) {
		walkPending(v, func(p Pending) {
			for _, a := range p.after {
				set[a] = true
			}
		})
	}
	switch {
	case ch.Deferred != nil:
		for _, a := range ch.Deferred.After {
			set[a] = true
		}
	case ch.Collection != "":
		add(Value(ch.Key))
		add(Value(ch.Item.Fields))
	case ch.Field != "":
		add(ch.Resource.Fields[ch.Field])
	default:
		add(Value(ch.Resource.Fields))
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func isCreateOf(ch Change) (string, bool) {
	if ch.Action == ActionCreate && ch.Collection == "" && ch.Field == "" {
		return ch.ResourceType + "/" + ch.Name, true
	}
	return "", false
}

// orderByNeeds moves each change after the creations it waits on, keeping
// the given order otherwise: each step takes the earliest change whose
// creations are taken. It refuses a change waiting on a creation the plan
// does not make, and changes waiting on each other.
func orderByNeeds(changes []Change) ([]Change, error) {
	creates := map[string]bool{}
	for _, ch := range changes {
		if k, ok := isCreateOf(ch); ok {
			creates[k] = true
		}
	}
	needs := make([][]string, len(changes))
	var problems []string
	for i, ch := range changes {
		needs[i] = ch.needs()
		// A child or field of a resource the plan creates waits on that
		// creation too, wherever the creation's own needs moved it.
		if own := ch.ResourceType + "/" + ch.Name; creates[own] && ch.Action != ActionDelete && !contains(needs[i], own) {
			if _, isCreate := isCreateOf(ch); !isCreate {
				needs[i] = append(needs[i], own)
			}
		}
		for _, n := range needs[i] {
			if !creates[n] {
				problems = append(problems, fmt.Sprintf("%s waits on the creation of %s, which the plan does not make", describeChange(ch), n))
			}
		}
	}
	if len(problems) > 0 {
		return nil, refusal("the plan", problems)
	}
	done := map[string]bool{}
	taken := make([]bool, len(changes))
	out := make([]Change, 0, len(changes))
	for len(out) < len(changes) {
		next := -1
		for i := range changes {
			if taken[i] {
				continue
			}
			ready := true
			for _, n := range needs[i] {
				if !done[n] {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next < 0 {
			for i, ch := range changes {
				if !taken[i] {
					problems = append(problems, fmt.Sprintf("%s waits on the creation of %s", describeChange(ch), strings.Join(needs[i], ", ")))
				}
			}
			return nil, refusal("the plan, whose changes wait on each other,", problems)
		}
		taken[next] = true
		out = append(out, changes[next])
		if k, ok := isCreateOf(changes[next]); ok {
			done[k] = true
		}
	}
	return out, nil
}

// realizer resolves a plan's pending values while Execute runs it, reading
// through the tool's live reader itself, not the plan's cached reads: the
// values exist only once the changes before have run.
type realizer struct {
	p       Plan
	types   map[string]ResourceType
	created map[string]bool
}

// value resolves every Pending in v whose creations ran; ok is false when one
// waits on a creation that has not run yet.
func (z *realizer) value(v Value) (Value, bool, error) {
	switch x := v.(type) {
	case Pending:
		for _, a := range x.after {
			if !z.created[a] {
				return nil, false, nil
			}
		}
		val, err := z.resolve(x)
		return val, err == nil, err
	case []Value:
		out := make([]Value, len(x))
		for i, e := range x {
			r, ok, err := z.value(e)
			if err != nil || !ok {
				return nil, ok, err
			}
			out[i] = r
		}
		return out, true, nil
	case map[string]Value:
		out := make(map[string]Value, len(x))
		for k, e := range x {
			r, ok, err := z.value(e)
			if err != nil || !ok {
				return nil, ok, err
			}
			out[k] = r
		}
		return out, true, nil
	}
	return v, true, nil
}

func (z *realizer) resolve(p Pending) (Value, error) {
	if p.ref != nil {
		v, ok, err := z.p.live.Exposed(p.ref.ResourceType, p.ref.Name, p.ref.Value)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s does not exist after its creation", p.Placeholder())
		}
		return v, nil
	}
	d, err := z.derive(*p.cause)
	if err != nil {
		return nil, err
	}
	return d.Value, nil
}

// derive runs the cause's derivation again, for its slot.
func (z *realizer) derive(cs cause) (Derived, error) {
	r := z.p.set.Resources[cs.source]
	ds, err := z.types[r.Type].Derive(r, z.p.live)
	if err != nil {
		return Derived{}, err
	}
	for _, d := range ds {
		if sameSlot(d.Slot, cs.slot) {
			if d.Unresolved != "" {
				return Derived{}, fmt.Errorf("the derivation of %s still waits once the plan created what it waits on: %s", source(r), d.Unresolved)
			}
			if p := checkDerived(r, cs.slot, d, z.types); p != nil {
				return Derived{}, refusal("the derivation", p)
			}
			return d, nil
		}
	}
	return Derived{}, fmt.Errorf("%s: Derive returned no value for %s of %s/%s, a bug in the tool", r.File, describeSlot(cs.slot), cs.slot.ResourceType, cs.slot.Name)
}

// realize is the change as the writer gets it: its pending values resolved
// (a field or child of the expected resource whose value waits on a creation
// that has not run yet is left out, as an unresolved one is), or, for a
// deferred change, one change per child to create, update, or delete.
func (z *realizer) realize(ch Change) ([]Change, error) {
	if ch.Deferred != nil {
		return z.expand(ch)
	}
	out := ch
	var err error
	if out.Key, err = z.must(ch.Key, ch); err != nil {
		return nil, err
	}
	if out.Item.Fields, err = z.must(ch.Item.Fields, ch); err != nil {
		return nil, err
	}
	if out.Resource, err = z.resource(ch.Resource); err != nil {
		return nil, err
	}
	if ch.Field != "" && ch.Collection == "" {
		if _, ok := out.Resource.Fields[ch.Field]; !ok {
			return nil, fmt.Errorf("%s: its value waits on a creation that has not run, a bug in the plan's order", describeChange(ch))
		}
	}
	return []Change{out}, nil
}

// must resolves a map every pending value of which must resolve now.
func (z *realizer) must(m map[string]Value, ch Change) (map[string]Value, error) {
	if m == nil {
		return nil, nil
	}
	v, ok, err := z.value(Value(m))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s: its value waits on a creation that has not run, a bug in the plan's order", describeChange(ch))
	}
	return v.(map[string]Value), nil
}

func (z *realizer) resource(r Resource) (Resource, error) {
	out := r
	out.Fields = map[string]Value{}
	for f, v := range r.Fields {
		val, ok, err := z.value(v)
		if err != nil {
			return Resource{}, err
		}
		if ok {
			out.Fields[f] = val
		}
	}
	if r.Children != nil {
		out.Children = map[string][]Item{}
		for col, items := range r.Children {
			list := []Item{}
			for _, it := range items {
				val, ok, err := z.value(Value(it.Fields))
				if err != nil {
					return Resource{}, err
				}
				if ok {
					list = append(list, Item{Fields: val.(map[string]Value)})
				}
			}
			out.Children[col] = list
		}
	}
	return out, nil
}

// expand derives a deferred change's children now and returns the changes
// making the slot's live children match them: creates and updates, then
// deletes.
func (z *realizer) expand(ch Change) ([]Change, error) {
	d := ch.Deferred
	derived, err := z.derive(d.cause)
	if err != nil {
		return nil, err
	}
	col, _ := findCollection(z.types[ch.ResourceType].Descriptor(), ch.Collection)
	lr, exists, err := z.p.live.Read(ch.ResourceType, ch.Name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%s/%s is not live after its creation, so the children %s derives cannot be written", ch.ResourceType, ch.Name, d.Source)
	}
	base, err := z.resource(ch.Resource)
	if err != nil {
		return nil, err
	}
	base.Children[ch.Collection] = append(append([]Item{}, base.Children[ch.Collection]...), derived.Items...)
	live := map[string]Item{}
	for _, li := range lr.Children[ch.Collection] {
		if matchesAll(li, d.Slot.Match) {
			live[keyJSON(col, li)] = li
		}
	}
	var upserts, deletes []Change
	kept := map[string]bool{}
	for _, it := range derived.Items {
		k := keyJSON(col, it)
		kept[k] = true
		next := Change{ResourceType: ch.ResourceType, Name: ch.Name, Collection: ch.Collection, Key: itemKey(col, it), Resource: base, Item: it, Stage: ch.Stage}
		li, ok := live[k]
		if !ok {
			next.Action = ActionCreate
			upserts = append(upserts, next)
			continue
		}
		for _, f := range col.Fields {
			ev, ehas := it.Fields[f]
			lv, lhas := li.Fields[f]
			if ehas != lhas || (ehas && !equal(ev, lv)) {
				u := next
				u.Action, u.Field = ActionUpdate, f
				upserts = append(upserts, u)
			}
		}
	}
	for _, li := range lr.Children[ch.Collection] {
		k := keyJSON(col, li)
		if _, covered := live[k]; !covered || kept[k] {
			continue
		}
		deletes = append(deletes, Change{Action: ActionDelete, ResourceType: ch.ResourceType, Name: ch.Name, Collection: ch.Collection, Key: itemKey(col, li), Resource: base, Item: li, Stage: ch.Stage})
	}
	return append(upserts, deletes...), nil
}
