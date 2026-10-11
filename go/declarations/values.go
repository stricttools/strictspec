package declarations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// checkValue refuses anything that is not canonical data (string, int64,
// bool, []Value, map[string]Value), or a Reference where refs allows one.
func checkValue(v Value, refs bool) error {
	switch x := v.(type) {
	case string, int64, bool:
		return nil
	case Reference:
		if !refs {
			return fmt.Errorf("a reference (%s) where only live data belongs", describeReference(x))
		}
		return nil
	case []Value:
		for _, e := range x {
			if err := checkValue(e, refs); err != nil {
				return err
			}
		}
		return nil
	case map[string]Value:
		for _, e := range x {
			if err := checkValue(e, refs); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("a %T, which is not a declaration value (string, int64, bool, []Value, map[string]Value, or Reference)", v)
}

func describeReference(r Reference) string {
	return fmt.Sprintf("%s/%s %s", r.ResourceType, r.Name, r.Value)
}

// jsonData turns a value into data encoding/json renders canonically: a
// Reference becomes its TOML form's object.
func jsonData(v Value) any {
	switch x := v.(type) {
	case Reference:
		return map[string]any{referenceKey(x.ResourceType): x.Name, "value": x.Value}
	case []Value:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonData(e)
		}
		return out
	case map[string]Value:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonData(e)
		}
		return out
	}
	return v
}

// canonicalJSON encodes data with sorted keys and no insignificant
// whitespace.
func canonicalJSON(data any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		panic("declarations: canonical JSON: " + err.Error())
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func valueJSON(v Value) string { return canonicalJSON(jsonData(v)) }

func equal(a, b Value) bool { return valueJSON(a) == valueJSON(b) }

// hasReference reports whether v holds a Reference anywhere.
func hasReference(v Value) bool {
	found := false
	walkReferences(v, func(Reference) { found = true })
	return found
}

func walkReferences(v Value, f func(Reference)) {
	switch x := v.(type) {
	case Reference:
		f(x)
	case []Value:
		for _, e := range x {
			walkReferences(e, f)
		}
	case map[string]Value:
		for _, k := range sortedKeys(x) {
			walkReferences(x[k], f)
		}
	}
}

func (m Matcher) matches(v Value, present bool) bool {
	if !present {
		return false
	}
	if m.isPrefix {
		s, ok := v.(string)
		return ok && strings.HasPrefix(s, m.prefix)
	}
	return equal(m.exact, v)
}

func (m Matcher) String() string {
	if m.isPrefix {
		return "prefix " + valueJSON(m.prefix)
	}
	return valueJSON(m.exact)
}

// compatible reports whether some value matches both matchers: equal exact
// values, prefixes one of which extends the other, or an exact string
// starting with the prefix.
func (m Matcher) compatible(o Matcher) bool {
	switch {
	case m.isPrefix && o.isPrefix:
		return strings.HasPrefix(m.prefix, o.prefix) || strings.HasPrefix(o.prefix, m.prefix)
	case m.isPrefix:
		return o.matches(m.prefix, true) || m.matches(o.exact, true)
	case o.isPrefix:
		return o.matches(m.exact, true)
	}
	return equal(m.exact, o.exact)
}

// matchesAll reports whether the item's fields match every matcher.
func matchesAll(it Item, match map[string]Matcher) bool {
	for k, m := range match {
		v, ok := it.Fields[k]
		if !m.matches(v, ok) {
			return false
		}
	}
	return true
}

// compatibleMatchers reports whether one child could match both: every field
// both constrain is compatible.
func compatibleMatchers(a, b map[string]Matcher) bool {
	for k, m := range a {
		if o, ok := b[k]; ok && !m.compatible(o) {
			return false
		}
	}
	return true
}

func exactMatchers(match map[string]Value) map[string]Matcher {
	out := make(map[string]Matcher, len(match))
	for k, v := range match {
		out[k] = Exact(v)
	}
	return out
}

func describeMatchers(match map[string]Matcher) string {
	parts := []string{}
	for _, k := range sortedKeys(match) {
		parts = append(parts, k+" "+match[k].String())
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// itemKey is the item's key fields as a map.
func itemKey(c Collection, it Item) map[string]Value {
	k := make(map[string]Value, len(c.Key))
	for _, f := range c.Key {
		k[f] = it.Fields[f]
	}
	return k
}

func keyJSON(c Collection, it Item) string { return valueJSON(Value(itemKey(c, it))) }

// nonKeyFields is the item's fields other than its key, as an object.
func nonKeyFields(c Collection, it Item) map[string]Value {
	out := map[string]Value{}
	for f, v := range it.Fields {
		if !contains(c.Key, f) {
			out[f] = v
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
