package declarations

import (
	"fmt"
	"sort"
	"strings"
)

// RenderImport renders a new declaration file from a live resource: a header
// saying it was imported from live state and must be reviewed,
// format_version = 1, the descriptor's fields in its order, unmanaged = [],
// and the children sorted by key. Create-only fields, fields the descriptor
// does not name, and the fields and children a slot of a resource declared in
// the set covers are left out. The tool refuses an existing target file
// before reading anything, and writes the bytes.
func RenderImport(set Set, live Resource) ([]byte, error) {
	if err := checkTool(set.Tool); err != nil {
		return nil, err
	}
	types := typesByName(set.Tool)
	rt := types[live.Type]
	if rt == nil {
		return nil, fmt.Errorf("%s is not a resource type %s supplies (%s)", live.Type, set.Tool.Name, typeNames(set.Tool))
	}
	if problems := checkLive(types, live.Type, live.Name, live); len(problems) > 0 {
		return nil, refusal("the live resource "+source(live), problems)
	}
	desc := rt.Descriptor()
	var slots []Slot
	for _, c := range set.causes {
		if c.slot.ResourceType == live.Type && c.slot.Name == live.Name {
			slots = append(slots, c.slot)
		}
	}
	derivedField := func(f string) bool {
		for _, sl := range slots {
			if sl.Field == f {
				return true
			}
		}
		return false
	}
	derivedItem := func(col string, it Item) bool {
		for _, sl := range slots {
			if sl.Collection == col && matchesAll(it, sl.Match) {
				return true
			}
		}
		return false
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Imported from live state by %s: review every value before committing it.\n", set.Tool.Name)
	b.WriteString("format_version = 1\n")
	line := func(k string, v Value) error {
		s, err := tomlValue(v)
		if err != nil {
			return err
		}
		b.WriteString(tomlKey(k) + " = " + s + "\n")
		return nil
	}
	for _, f := range desc.Fields {
		v, ok := live.Fields[f]
		if !ok || derivedField(f) {
			continue
		}
		if err := line(f, v); err != nil {
			return nil, err
		}
	}
	tables := map[string][]Item{}
	for _, col := range desc.Collections {
		var items []Item
		for _, it := range live.Children[col.Name] {
			if !derivedItem(col.Name, it) {
				items = append(items, it)
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return keyJSON(col, items[i]) < keyJSON(col, items[j]) })
		if len(items) == 0 {
			b.WriteString(tomlKey(col.Name) + " = []\n")
			continue
		}
		tables[col.Name] = items
	}
	b.WriteString("unmanaged = []\n")
	for _, col := range desc.Collections {
		for _, it := range tables[col.Name] {
			b.WriteString("\n[[" + tomlKey(col.Name) + "]]\n")
			for _, f := range append(append([]string{}, col.Key...), col.Fields...) {
				if v, ok := it.Fields[f]; ok {
					if err := line(f, v); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return []byte(b.String()), nil
}
