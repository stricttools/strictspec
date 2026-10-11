package declarations

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var bareKeyRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlString renders s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlKey(k string) string {
	if bareKeyRE.MatchString(k) {
		return k
	}
	return tomlString(k)
}

// tomlValue renders a canonical value inline.
func tomlValue(v Value) (string, error) {
	switch x := v.(type) {
	case string:
		return tomlString(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case bool:
		return strconv.FormatBool(x), nil
	case []Value:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			s, err := tomlValue(e)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]Value:
		keys := sortedKeys(x)
		if len(keys) == 0 {
			return "{}", nil
		}
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, err := tomlValue(x[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, tomlKey(k)+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case Reference:
		return "{ " + tomlKey(referenceKey(x.ResourceType)) + " = " + tomlString(x.Name) + ", value = " + tomlString(x.Value) + " }", nil
	}
	return "", fmt.Errorf("%T is not a declaration value", v)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
