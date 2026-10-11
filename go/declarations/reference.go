package declarations

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/stricttools/strictspec/go/strictspec"
)

var referenceKeyRE = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// referenceKey is the TOML key a reference to a resource type is written
// under: the resource type's name with - as _.
func referenceKey(resourceType string) string { return strings.ReplaceAll(resourceType, "-", "_") }

// ParseReference binds a reference written
// { <resource type with - as _> = "<name>", value = "<exposed value>" }, for
// example { hetzner_server = "web1", value = "ipv4" }. A resource type's
// Decode calls it on a field its schema lets hold a reference; Load then
// checks the named resource is declared and its type exposes the value.
func ParseReference(v strictspec.Value) (Reference, error) {
	const form = `write a reference as { <resource type with - as _> = "<name>", value = "<exposed value>" }`
	if v.Kind() != strictspec.KindRecord {
		return Reference{}, errors.New("not a table: " + form)
	}
	entries := v.Entries()
	if len(entries) != 2 {
		return Reference{}, fmt.Errorf("a reference holds two keys, this one %d: %s", len(entries), form)
	}
	var r Reference
	for _, kv := range entries {
		s, ok := kv.Value.AsString()
		if !ok || s == "" {
			return Reference{}, fmt.Errorf("%s is not a non-empty string: %s", kv.Key, form)
		}
		if kv.Key == "value" {
			r.Value = s
			continue
		}
		if !referenceKeyRE.MatchString(kv.Key) {
			return Reference{}, fmt.Errorf("%q does not name a resource type: %s", kv.Key, form)
		}
		r.ResourceType = strings.ReplaceAll(kv.Key, "_", "-")
		r.Name = s
	}
	if r.Value == "" || r.ResourceType == "" {
		return Reference{}, errors.New("the reference lacks value or the resource type: " + form)
	}
	return r, nil
}
