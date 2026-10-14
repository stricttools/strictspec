// Package fixturetoml reads a test fixture's TOML file into plain Go values, so
// the fixture runners in the test suites can read their fixture descriptions
// with strictspec's own TOML parser instead of a third-party one.
//
// Tables become map[string]any, arrays []any, strings string, integers int64,
// floats float64, booleans bool, and date-times their lexeme as a string.
package fixturetoml

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/strdecode"
	"github.com/stricttools/strictspec/go/internal/tomldoc"
)

// Load parses the TOML file at path and returns its root table.
func Load(path string) (map[string]any, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, perr := tomldoc.Parse(src)
	if perr != nil {
		return nil, fmt.Errorf("%s: %s", path, perr.Message)
	}
	v, err := value(d.Root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: root is not a table", path)
	}
	return root, nil
}

func value(n doc.Node) (any, error) {
	switch n.Kind() {
	case doc.Record:
		out := map[string]any{}
		for _, e := range n.Entries() {
			v, err := value(e.Value)
			if err != nil {
				return nil, err
			}
			out[e.Key] = v
		}
		return out, nil
	case doc.Array:
		out := []any{}
		for _, item := range n.Items() {
			v, err := value(item)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case doc.String:
		return strdecode.TOML(n.Lexeme()), nil
	case doc.Integer:
		i, err := strconv.ParseInt(strings.ReplaceAll(n.Lexeme(), "_", ""), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("integer %q: %w", n.Lexeme(), err)
		}
		return i, nil
	case doc.Float:
		f, err := strconv.ParseFloat(strings.ReplaceAll(n.Lexeme(), "_", ""), 64)
		if err != nil {
			return nil, fmt.Errorf("float %q: %w", n.Lexeme(), err)
		}
		return f, nil
	case doc.Bool:
		return n.Lexeme() == "true", nil
	default:
		return n.Lexeme(), nil
	}
}
