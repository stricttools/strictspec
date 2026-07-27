// Command gencodes parses spec/appendix-error-codes.md (the single normative
// source for the error-code catalogue) and emits the generated Go catalogue
// table internal/codes/catalogue_gen.go. Hand-transcription of the catalogue is
// forbidden: this generator is the only writer of that file, and a freshness
// test regenerates and byte-compares it (drift = test failure).
//
// Invoke via `go generate ./internal/codes/...` or directly:
//
//	go run github.com/smm-h/strictspec/go/tools/gencodes [-spec PATH] [-out PATH]
//
// With no flags it locates the repo root (the ancestor containing
// spec/appendix-error-codes.md) and derives both paths.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const specRel = "spec/appendix-error-codes.md"
const outRel = "go/internal/codes/catalogue_gen.go"

func main() {
	specFlag := flag.String("spec", "", "path to appendix-error-codes.md (default: auto-locate)")
	outFlag := flag.String("out", "", "path to generated catalogue file (default: auto-locate)")
	flag.Parse()

	specPath := *specFlag
	outPath := *outFlag
	if specPath == "" || outPath == "" {
		root, err := findRepoRoot()
		if err != nil {
			fatal(err)
		}
		if specPath == "" {
			specPath = filepath.Join(root, specRel)
		}
		if outPath == "" {
			outPath = filepath.Join(root, outRel)
		}
	}

	src, err := os.ReadFile(specPath)
	if err != nil {
		fatal(fmt.Errorf("reading spec: %w", err))
	}
	entries, areas, err := parse(string(src))
	if err != nil {
		fatal(fmt.Errorf("parsing %s: %w", specPath, err))
	}
	code, err := render(entries, areas, specRel)
	if err != nil {
		fatal(fmt.Errorf("rendering generated code: %w", err))
	}
	if err := os.WriteFile(outPath, code, 0o644); err != nil {
		fatal(fmt.Errorf("writing %s: %w", outPath, err))
	}
	fmt.Printf("gencodes: wrote %d codes to %s\n", len(entries), outPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gencodes:", err)
	os.Exit(1)
}

// findRepoRoot walks up from the working directory to the first ancestor that
// contains spec/appendix-error-codes.md.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, specRel)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate %s in any ancestor of the working directory", specRel)
		}
		dir = parent
	}
}

type slotSpec struct {
	name     string
	typ      string // SlotType constant name
	elemType string // for list types; "" otherwise
}

type entry struct {
	code     string
	area     string
	template string
	slots    []slotSpec
}

var (
	// A code row: | `STRICTSPEC_...` | `template` | slots | notes |
	codeRowRe = regexp.MustCompile("^\\| `STRICTSPEC_")
	// An area row in section 3: | `AREA` | domain |
	areaRowRe      = regexp.MustCompile("^\\| `([A-Z]+)` \\|")
	areaHeadingRe  = regexp.MustCompile(`^## 3\. `)
	nextHeadingRe  = regexp.MustCompile(`^## `)
	placeholderRe  = regexp.MustCompile(`\{(\w+)\}`)
	slotEntryRe    = regexp.MustCompile(`^([A-Za-z_]\w*):\s*(.+)$`)
	backtickCodeRe = regexp.MustCompile("^`(STRICTSPEC_[A-Z0-9_]+)`$")
)

func parse(src string) ([]entry, []string, error) {
	lines := strings.Split(src, "\n")

	areas, err := parseAreas(lines)
	if err != nil {
		return nil, nil, err
	}
	areaSet := map[string]bool{}
	for _, a := range areas {
		areaSet[a] = true
	}

	var entries []entry
	seen := map[string]bool{}
	for i, line := range lines {
		if !codeRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 3 {
			return nil, nil, fmt.Errorf("line %d: expected at least 3 table cells, got %d: %q", i+1, len(cells), line)
		}
		code, err := parseCodeCell(cells[0])
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if seen[code] {
			return nil, nil, fmt.Errorf("line %d: duplicate code %s", i+1, code)
		}
		seen[code] = true

		template, err := parseTemplateCell(cells[1])
		if err != nil {
			return nil, nil, fmt.Errorf("line %d (%s): %w", i+1, code, err)
		}

		area, err := areaOf(code, areaSet)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}

		declared, err := parseSlotsCell(cells[2])
		if err != nil {
			return nil, nil, fmt.Errorf("line %d (%s): %w", i+1, code, err)
		}

		slots, err := resolveSlots(code, template, declared)
		if err != nil {
			return nil, nil, err
		}

		entries = append(entries, entry{code: code, area: area, template: template, slots: slots})
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no code rows found")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].code < entries[j].code })
	return entries, areas, nil
}

// parseAreas extracts the closed area set from the section-3 table.
func parseAreas(lines []string) ([]string, error) {
	var areas []string
	inSection := false
	for _, line := range lines {
		if areaHeadingRe.MatchString(line) {
			inSection = true
			continue
		}
		if inSection && nextHeadingRe.MatchString(line) {
			break
		}
		if !inSection {
			continue
		}
		if m := areaRowRe.FindStringSubmatch(line); m != nil {
			areas = append(areas, m[1])
		}
	}
	if len(areas) == 0 {
		return nil, fmt.Errorf("could not parse the closed area set from section 3")
	}
	sort.Strings(areas)
	return areas, nil
}

// splitCells splits a markdown table row on UNESCAPED pipes and returns the
// interior cells (dropping the empty leading/trailing border cells). Each cell
// is trimmed and has escaped pipes (\|) unescaped.
func splitCells(row string) []string {
	var cells []string
	var cur strings.Builder
	runes := []rune(row)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) && runes[i+1] == '|' {
			cur.WriteRune('|')
			i++
			continue
		}
		if r == '|' {
			cells = append(cells, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	cells = append(cells, cur.String())
	// Drop the empty border cells produced by the leading and trailing '|'.
	if len(cells) >= 2 {
		cells = cells[1 : len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func parseCodeCell(cell string) (string, error) {
	m := backtickCodeRe.FindStringSubmatch(cell)
	if m == nil {
		return "", fmt.Errorf("malformed code cell %q", cell)
	}
	return m[1], nil
}

func parseTemplateCell(cell string) (string, error) {
	if len(cell) < 2 || cell[0] != '`' || cell[len(cell)-1] != '`' {
		return "", fmt.Errorf("template cell not backtick-delimited: %q", cell)
	}
	return cell[1 : len(cell)-1], nil
}

// areaOf derives the area from the code (the second underscore-delimited token)
// and validates it against the closed area set.
func areaOf(code string, areaSet map[string]bool) (string, error) {
	parts := strings.SplitN(code, "_", 3)
	if len(parts) < 3 || parts[0] != "STRICTSPEC" {
		return "", fmt.Errorf("code %q is not STRICTSPEC_<AREA>_<NAME>", code)
	}
	area := parts[1]
	if !areaSet[area] {
		return "", fmt.Errorf("code %q has area %q outside the closed area set", code, area)
	}
	return area, nil
}

// parseSlotsCell parses the "Slots" column into a name->type map. An em-dash or
// empty cell means no declared slots.
func parseSlotsCell(cell string) (map[string]slotSpec, error) {
	out := map[string]slotSpec{}
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "—" || cell == "-" {
		return out, nil
	}
	for _, part := range strings.Split(cell, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		m := slotEntryRe.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("malformed slot declaration %q", part)
		}
		name := m[1]
		typeText := strings.TrimSpace(m[2])
		spec, err := parseSlotType(name, typeText)
		if err != nil {
			return nil, err
		}
		out[name] = spec
	}
	return out, nil
}

// parseSlotType maps an appendix slot-type token to a SlotType constant name.
func parseSlotType(name, typeText string) (slotSpec, error) {
	// Unescape markdown-escaped angle brackets (list\<string>).
	typeText = strings.ReplaceAll(typeText, `\<`, "<")
	typeText = strings.ReplaceAll(typeText, `\>`, ">")
	typeText = strings.TrimSpace(typeText)

	if strings.HasPrefix(typeText, "list<") && strings.HasSuffix(typeText, ">") {
		elem := typeText[len("list<") : len(typeText)-1]
		elemConst, err := scalarSlotType(elem)
		if err != nil {
			return slotSpec{}, fmt.Errorf("slot %q: %w", name, err)
		}
		return slotSpec{name: name, typ: "SlotTypeList", elemType: elemConst}, nil
	}
	c, err := scalarSlotType(typeText)
	if err != nil {
		return slotSpec{}, fmt.Errorf("slot %q: %w", name, err)
	}
	return slotSpec{name: name, typ: c}, nil
}

func scalarSlotType(token string) (string, error) {
	switch token {
	case "string":
		return "SlotTypeString", nil
	case "int":
		return "SlotTypeInt", nil
	case "code":
		return "SlotTypeCode", nil
	case "identifier":
		return "SlotTypeIdentifier", nil
	case "version":
		return "SlotTypeVersion", nil
	case "path":
		return "SlotTypePath", nil
	case "value":
		return "SlotTypeValue", nil
	default:
		return "", fmt.Errorf("unknown slot type %q", token)
	}
}

// resolveSlots builds the ordered slot list from the template placeholders,
// assigning each a type from the declared Slots column, with two auto-typed
// exceptions the appendix relies on: {path} (auto-injected, always path) and
// {suggestion} (computed did-you-mean, string). Every declared slot must be a
// template placeholder, and every non-auto placeholder must be declared.
func resolveSlots(code, template string, declared map[string]slotSpec) ([]slotSpec, error) {
	placeholders := placeholderRe.FindAllStringSubmatch(template, -1)
	seen := map[string]bool{}
	var order []string
	for _, m := range placeholders {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		order = append(order, name)
	}
	// Every declared slot must correspond to a placeholder.
	for name := range declared {
		if !seen[name] {
			return nil, fmt.Errorf("code %s declares slot %q that the template does not reference", code, name)
		}
	}
	var slots []slotSpec
	for _, name := range order {
		if spec, ok := declared[name]; ok {
			slots = append(slots, spec)
			continue
		}
		switch name {
		case "path":
			slots = append(slots, slotSpec{name: "path", typ: "SlotTypePath"})
		case "suggestion":
			slots = append(slots, slotSpec{name: "suggestion", typ: "SlotTypeString"})
		default:
			return nil, fmt.Errorf("code %s: template placeholder {%s} has no declared slot type", code, name)
		}
	}
	return slots, nil
}

func render(entries []entry, areas []string, specPathRel string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by tools/gencodes from %s. DO NOT EDIT.\n\n", specPathRel)
	b.WriteString("package codes\n\n")
	fmt.Fprintf(&b, "// Areas is the closed area set (appendix-error-codes.md section 3).\n")
	b.WriteString("var Areas = []string{\n")
	for _, a := range areas {
		fmt.Fprintf(&b, "\t%q,\n", a)
	}
	b.WriteString("}\n\n")
	b.WriteString("var catalogue = map[string]Entry{\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\t%q: {\n", e.code)
		fmt.Fprintf(&b, "\t\tCode:     %q,\n", e.code)
		fmt.Fprintf(&b, "\t\tArea:     %q,\n", e.area)
		fmt.Fprintf(&b, "\t\tTemplate: %s,\n", strconv.Quote(e.template))
		if len(e.slots) == 0 {
			b.WriteString("\t\tSlots:    nil,\n")
		} else {
			b.WriteString("\t\tSlots: []SlotSpec{\n")
			for _, s := range e.slots {
				if s.elemType != "" {
					fmt.Fprintf(&b, "\t\t\t{Name: %q, Type: %s, ElemType: %s},\n", s.name, s.typ, s.elemType)
				} else {
					fmt.Fprintf(&b, "\t\t\t{Name: %q, Type: %s},\n", s.name, s.typ)
				}
			}
			b.WriteString("\t\t},\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w\n---\n%s", err, b.String())
	}
	return formatted, nil
}
