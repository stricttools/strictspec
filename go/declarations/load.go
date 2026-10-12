package declarations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stricttools/strictspec/go/strictspec"
)

// Set is a loaded and checked declaration directory: the tool, the project,
// and its resources sorted by resource type and name.
type Set struct {
	Tool      Tool
	Project   Project
	Resources []Resource

	// files maps each file's path relative to the declaration directory to
	// the hex SHA-256 of its bytes.
	files     map[string]string
	hasClaims bool
	causes    []cause
	// yielded are the slots dropped because they yield to another
	// resource's slot covering the same field or child.
	yielded []cause
	// importing is the resource LoadForImport loaded the set for; such a set
	// serves RenderImport only.
	importing *importTarget
}

// importTarget is the resource being imported: a slot targeting it is not
// refused when it is not declared yet.
type importTarget struct {
	resourceType, name string
}

// cause is one slot and the index of the declared resource causing it.
type cause struct {
	slot   Slot
	source int
}

func (s Set) sourceOf(c cause) string { return source(s.Resources[c.source]) }

// source names a declaration file as <resource type>/<name>.
func source(r Resource) string { return r.Type + "/" + r.Name }

func (s Set) index(resourceType, name string) (int, bool) {
	for i, r := range s.Resources {
		if r.Type == resourceType && r.Name == name {
			return i, true
		}
	}
	return 0, false
}

func typesByName(tool Tool) map[string]ResourceType {
	m := map[string]ResourceType{}
	for _, rt := range tool.ResourceTypes {
		m[rt.Name()] = rt
	}
	return m
}

func typeNames(tool Tool) string {
	names := []string{}
	for _, rt := range tool.ResourceTypes {
		names = append(names, rt.Name())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func findCollection(d Descriptor, name string) (Collection, bool) {
	for _, c := range d.Collections {
		if c.Name == name {
			return c, true
		}
	}
	return Collection{}, false
}

func collectionNames(d Descriptor) string {
	names := []string{}
	for _, c := range d.Collections {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

func hashHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// compileType compiles a resource type's schema with the engine's type
// definitions added.
func compileType(rt ResourceType) (*strictspec.Program, error) {
	files, main := rt.Schema()
	all := map[string]string{}
	for k, v := range files {
		all[k] = v
	}
	if _, ok := all[typesFile]; ok {
		return nil, fmt.Errorf("declarations: the resource type %s supplies %s itself: the engine adds it", rt.Name(), typesFile)
	}
	all[typesFile] = schemaSource(typesFile)
	p, err := strictspec.CompileEmbedded(all, main)
	if err != nil {
		return nil, fmt.Errorf("declarations: the schema of the resource type %s does not compile: %w", rt.Name(), err)
	}
	return p, nil
}

func refusal(what string, problems []string) error {
	return fmt.Errorf("%s is refused:\n  - %s", what, strings.Join(problems, "\n  - "))
}

// Load reads and checks every file of the project's declaration directory.
func Load(tool Tool, project Project) (Set, error) {
	return load(tool, project, nil)
}

// LoadForImport is Load for importing the resource (resourceType, name) with
// one relaxation: a slot whose target is that resource is not refused when the
// resource is not declared yet, so RenderImport leaves out the fields and
// children the slot covers. Every other refusal stays. The set it returns
// serves RenderImport only: Compare and Apply refuse it.
func LoadForImport(tool Tool, project Project, resourceType, name string) (Set, error) {
	if err := checkTool(tool); err != nil {
		return Set{}, err
	}
	if typesByName(tool)[resourceType] == nil {
		return Set{}, fmt.Errorf("%s is not a resource type %s supplies (%s)", resourceType, tool.Name, typeNames(tool))
	}
	return load(tool, project, &importTarget{resourceType: resourceType, name: name})
}

func load(tool Tool, project Project, importing *importTarget) (Set, error) {
	if err := checkTool(tool); err != nil {
		return Set{}, err
	}
	types := typesByName(tool)
	dir := dirAbs(tool, project.Root)
	set := Set{Tool: tool, Project: project, files: map[string]string{}, importing: importing}
	top, err := os.ReadDir(dir)
	if err != nil {
		return Set{}, err
	}
	type document struct {
		rt  ResourceType
		rel string
		src []byte
	}
	var docs []document
	var problems []string
	// companionFiles are the files of resource type directories that are
	// not declarations, by their path relative to the declaration
	// directory: each must be a companion a declaration names.
	companionFiles := map[string]string{}
	for _, e := range top {
		n := e.Name()
		switch {
		case e.Type().IsRegular() && (n == manifestFile || n == projectFile || n == claimsFile):
			src, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				return Set{}, err
			}
			set.files[n] = hashHex(src)
			set.hasClaims = set.hasClaims || n == claimsFile
		case e.IsDir() && types[n] != nil:
			entries, err := os.ReadDir(filepath.Join(dir, n))
			if err != nil {
				return Set{}, err
			}
			for _, f := range entries {
				rel := n + "/" + f.Name()
				if f.Type().IsRegular() && !strings.HasSuffix(f.Name(), ".toml") && len(types[n].Descriptor().Companions) > 0 {
					companionFiles[rel] = filepath.Join(dir, n, f.Name())
					continue
				}
				if !f.Type().IsRegular() || !strings.HasSuffix(f.Name(), ".toml") || f.Name() == ".toml" {
					problems = append(problems, fmt.Sprintf("%s: a resource type directory holds only <name>.toml files, one per resource", rel))
					continue
				}
				src, err := os.ReadFile(filepath.Join(dir, n, f.Name()))
				if err != nil {
					return Set{}, err
				}
				set.files[rel] = hashHex(src)
				docs = append(docs, document{rt: types[n], rel: rel, src: src})
			}
		case e.IsDir():
			problems = append(problems, fmt.Sprintf("%s/ is not a resource type %s supplies (%s)", n, tool.Name, typeNames(tool)))
		default:
			problems = append(problems, fmt.Sprintf("%s: the top of the declaration directory holds only manifest.toml, project.toml, claims.toml, and resource type directories (%s)", n, typeNames(tool)))
		}
	}
	programs := map[string]*strictspec.Program{}
	var valid []document
	for _, d := range docs {
		p := programs[d.rt.Name()]
		if p == nil {
			if p, err = compileType(d.rt); err != nil {
				return Set{}, err
			}
			programs[d.rt.Name()] = p
		}
		if res := p.Validate(d.src, "toml"); !res.Valid {
			problems = append(problems, renderDiagnostics(d.rel, res.Diagnostics))
			continue
		}
		valid = append(valid, d)
	}
	if len(problems) > 0 {
		return Set{}, refusal(dirRel(tool), problems)
	}
	for _, d := range valid {
		v, err := strictspec.LoadValue(d.src, "toml")
		if err != nil {
			return Set{}, fmt.Errorf("%s: %w", d.rel, err)
		}
		desc := d.rt.Descriptor()
		unmanaged, ps := parseUnmanaged(d.rel, v, desc)
		problems = append(problems, ps...)
		stem := strings.TrimSuffix(d.rel[strings.Index(d.rel, "/")+1:], ".toml")
		r, err := d.rt.Decode(stem, v)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if r.Type != d.rt.Name() {
			problems = append(problems, fmt.Sprintf("%s: the resource type %s decoded it as a %q: a bug in the tool", d.rel, d.rt.Name(), r.Type))
			continue
		}
		if r.Name != stem {
			problems = append(problems, fmt.Sprintf("%s: the resource's name is %q: rename the file to %s/%s.toml", d.rel, r.Name, r.Type, r.Name))
			continue
		}
		r.File = d.rel
		r.Unmanaged = unmanaged
		for _, c := range desc.Companions {
			if _, set := r.Fields[c.Field]; set {
				problems = append(problems, fmt.Sprintf("%s: Decode set the companion field %s, which the engine sets from its file: a bug in the tool", d.rel, c.Field))
				continue
			}
			fv, _ := v.Field(c.FileKey)
			named, _ := fv.AsString()
			want := stem + c.Suffix
			if named != want {
				problems = append(problems, fmt.Sprintf("%s: %s is %q: write %s = %q, the file beside it holding its %s", d.rel, c.FileKey, named, c.FileKey, want, c.Field))
				continue
			}
			rel := r.Type + "/" + want
			path, ok := companionFiles[rel]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s names %s, which does not exist: write the %s there", d.rel, rel, c.Field))
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return Set{}, err
			}
			set.files[rel] = hashHex(content)
			r.Fields[c.Field] = string(content)
		}
		problems = append(problems, checkResource(r, desc, true)...)
		set.Resources = append(set.Resources, r)
	}
	for _, rel := range sortedKeys(companionFiles) {
		if _, named := set.files[rel]; !named {
			problems = append(problems, fmt.Sprintf("%s: a resource type directory holds <name>.toml files, one per resource, and the files they name; no declaration names this one", rel))
		}
	}
	if len(problems) > 0 {
		return Set{}, refusal(dirRel(tool), problems)
	}
	sort.Slice(set.Resources, func(i, j int) bool {
		a, b := set.Resources[i], set.Resources[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Name < b.Name
	})
	if problems = set.check(types); len(problems) > 0 {
		return Set{}, refusal(dirRel(tool), problems)
	}
	return set, nil
}

// parseUnmanaged binds the unmanaged array, running the match-value consumer
// check and checking each entry against the descriptor.
func parseUnmanaged(rel string, v strictspec.Value, desc Descriptor) ([]UnmanagedEntry, []string) {
	u, ok := v.Field("unmanaged")
	if !ok || u.Kind() != strictspec.KindArray {
		return nil, []string{fmt.Sprintf("%s: no unmanaged array: the resource type's schema must require one of UnmanagedEntry (from %s), a bug in the tool", rel, typesFile)}
	}
	out := []UnmanagedEntry{}
	var problems []string
	for i, e := range u.Items() {
		at := fmt.Sprintf("%s: unmanaged[%d]", rel, i)
		cv, _ := e.Field("collection")
		col, _ := cv.AsString()
		rv, _ := e.Field("reason")
		reason, _ := rv.AsString()
		entry := UnmanagedEntry{Collection: col, Match: map[string]Value{}, Reason: reason}
		c, found := findCollection(desc, col)
		if !found {
			problems = append(problems, fmt.Sprintf("%s.collection is %q: the resource type's collections are %s", at, col, collectionNames(desc)))
		}
		mv, _ := e.Field("match")
		kvs := mv.Entries()
		if len(kvs) == 0 {
			problems = append(problems, fmt.Sprintf("%s.match is empty: it holds at least one field to match", at))
		}
		for _, kv := range kvs {
			val, ok := matchValue(kv.Value)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s.match.%s is not a string, an integer, or a boolean: write one of those", at, kv.Key))
				continue
			}
			if found && !contains(c.Key, kv.Key) && !contains(c.Fields, kv.Key) {
				problems = append(problems, fmt.Sprintf("%s.match.%s is not a field of %s: its fields are %s", at, kv.Key, col, strings.Join(append(append([]string{}, c.Key...), c.Fields...), ", ")))
				continue
			}
			entry.Match[kv.Key] = val
		}
		out = append(out, entry)
	}
	return out, problems
}

// matchValue is the consumer check of declarations.types.toml's MatchValue.
func matchValue(v strictspec.Value) (Value, bool) {
	switch v.Kind() {
	case strictspec.KindString:
		s, _ := v.AsString()
		return s, true
	case strictspec.KindInteger:
		n, ok := v.Int()
		return n, ok
	case strictspec.KindBool:
		b, _ := v.Bool()
		return b, true
	}
	return nil, false
}

// checkResource checks a resource a tool built (by Decode, a live read, or a
// derivation) against its descriptor. refs says whether references may
// appear.
func checkResource(r Resource, desc Descriptor, refs bool) []string {
	at := r.File
	if at == "" {
		at = source(r)
	}
	var problems []string
	for _, f := range sortedKeys(r.Fields) {
		if !contains(desc.Fields, f) && !contains(desc.CreateOnly, f) {
			problems = append(problems, fmt.Sprintf("%s: the field %q is not in the resource type's descriptor, a bug in the tool", at, f))
			continue
		}
		if err := checkValue(r.Fields[f], refs); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s holds %v, a bug in the tool", at, f, err))
		}
	}
	for _, name := range sortedKeys(r.Children) {
		c, ok := findCollection(desc, name)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: the collection %q is not in the resource type's descriptor, a bug in the tool", at, name))
			continue
		}
		problems = append(problems, checkItems(at, c, r.Children[name], refs)...)
	}
	return problems
}

func checkItems(at string, c Collection, items []Item, refs bool) []string {
	var problems []string
	seen := map[string]int{}
	for i, it := range items {
		bad := false
		for _, k := range keyFields(c, it) {
			if _, ok := it.Fields[k]; !ok {
				problems = append(problems, fmt.Sprintf("%s: %s[%d] has no %s, a key field, a bug in the tool", at, c.Name, i, k))
				bad = true
			}
		}
		for _, f := range sortedKeys(it.Fields) {
			if err := checkValue(it.Fields[f], refs); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %s[%d].%s holds %v, a bug in the tool", at, c.Name, i, f, err))
				bad = true
			}
		}
		if bad {
			continue
		}
		k := keyJSON(c, it)
		if j, dup := seen[k]; dup {
			problems = append(problems, fmt.Sprintf("%s: declares the %s %s twice (%s[%d] and %s[%d]): a child is declared once", at, c.Name, k, c.Name, j, c.Name, i))
			continue
		}
		seen[k] = i
	}
	return problems
}

// check runs the checks across the set's files: references, slots, and
// unmanaged entries.
func (s *Set) check(types map[string]ResourceType) []string {
	var problems []string
	for _, r := range s.Resources {
		visit := func(v Value) {
			walkReferences(v, func(ref Reference) {
				tt := types[ref.ResourceType]
				switch _, declared := s.index(ref.ResourceType, ref.Name); {
				case tt == nil:
					problems = append(problems, fmt.Sprintf("%s: refers to %s/%s, but %s is not a resource type %s supplies (%s)", r.File, ref.ResourceType, ref.Name, ref.ResourceType, s.Tool.Name, typeNames(s.Tool)))
				case !declared:
					problems = append(problems, fmt.Sprintf("%s: refers to %s/%s, which is not declared: write %s/%s.toml", r.File, ref.ResourceType, ref.Name, ref.ResourceType, ref.Name))
				case !contains(tt.Descriptor().Exposes, ref.Value):
					problems = append(problems, fmt.Sprintf("%s: refers to the %s of %s/%s, which a %s does not expose (it exposes %s)", r.File, ref.Value, ref.ResourceType, ref.Name, ref.ResourceType, strings.Join(tt.Descriptor().Exposes, ", ")))
				}
			})
		}
		for _, f := range sortedKeys(r.Fields) {
			visit(r.Fields[f])
		}
		for _, c := range sortedKeys(r.Children) {
			for _, it := range r.Children[c] {
				for _, f := range sortedKeys(it.Fields) {
					visit(it.Fields[f])
				}
			}
		}
	}

	for i, r := range s.Resources {
		for _, sl := range types[r.Type].Slots(r) {
			if p := checkSlot(r, sl, types); p != "" {
				problems = append(problems, p)
				continue
			}
			if _, ok := s.index(sl.ResourceType, sl.Name); !ok && !s.isImporting(sl.ResourceType, sl.Name) {
				problems = append(problems, fmt.Sprintf("%s causes %s of %s/%s, which is not declared: write %s/%s.toml", r.File, describeSlot(sl), sl.ResourceType, sl.Name, sl.ResourceType, sl.Name))
				continue
			}
			s.causes = append(s.causes, cause{slot: sl, source: i})
		}
	}

	// A slot yielding to the resource type of another slot covering the
	// same field or child is dropped.
	drop := make([]bool, len(s.causes))
	for i, a := range s.causes {
		for j, b := range s.causes {
			if i == j || a.slot.ResourceType != b.slot.ResourceType || a.slot.Name != b.slot.Name || !overlap(a.slot, b.slot) {
				continue
			}
			if contains(a.slot.YieldsTo, s.Resources[b.source].Type) && !contains(b.slot.YieldsTo, s.Resources[a.source].Type) {
				drop[i] = true
			}
		}
	}
	kept := s.causes[:0:0]
	for i, c := range s.causes {
		if drop[i] {
			s.yielded = append(s.yielded, c)
			continue
		}
		kept = append(kept, c)
	}
	s.causes = kept

	for i, a := range s.causes {
		for _, b := range s.causes[i+1:] {
			if a.slot.ResourceType != b.slot.ResourceType || a.slot.Name != b.slot.Name {
				continue
			}
			if overlap(a.slot, b.slot) {
				problems = append(problems, fmt.Sprintf("%s and %s both derive %s of %s/%s: every field and child has one owning file; remove one of them", s.sourceOf(a), s.sourceOf(b), describeSlot(a.slot), a.slot.ResourceType, a.slot.Name))
			}
		}
	}

	for _, c := range s.causes {
		ti, ok := s.index(c.slot.ResourceType, c.slot.Name)
		if !ok { // the undeclared resource being imported: nothing hand-declared
			continue
		}
		t := s.Resources[ti]
		if c.slot.Field != "" {
			if _, ok := t.Fields[c.slot.Field]; ok {
				problems = append(problems, fmt.Sprintf("%s: %s is derived from %s; remove it from %s", t.File, c.slot.Field, s.sourceOf(c), t.File))
			}
			continue
		}
		for j, it := range t.Children[c.slot.Collection] {
			if matchesAll(it, c.slot.Match) {
				problems = append(problems, fmt.Sprintf("%s: %s[%d] is derived from %s (it matches %s); remove it from %s", t.File, c.slot.Collection, j, s.sourceOf(c), describeMatchers(c.slot.Match), t.File))
			}
		}
	}

	for ri, r := range s.Resources {
		for ui, u := range r.Unmanaged {
			um := exactMatchers(u.Match)
			for j, it := range r.Children[u.Collection] {
				if matchesAll(it, um) {
					problems = append(problems, fmt.Sprintf("%s: unmanaged[%d] matches %s[%d], which the file declares: a child is declared and unmanaged, never both; remove one of them", r.File, ui, u.Collection, j))
				}
			}
			for _, c := range s.causes {
				ti, ok := s.index(c.slot.ResourceType, c.slot.Name)
				if ok && ti == ri && c.slot.Collection == u.Collection && compatibleMatchers(um, c.slot.Match) {
					problems = append(problems, fmt.Sprintf("%s: unmanaged[%d] matches the %s children %s derives (%s): a child is derived and unmanaged, never both; remove the entry", r.File, ui, u.Collection, s.sourceOf(c), describeMatchers(c.slot.Match)))
				}
			}
		}
		for _, f := range types[r.Type].Descriptor().DerivableFields {
			if _, ok := r.Fields[f]; ok {
				continue
			}
			derived := false
			for _, c := range s.causes {
				ti, ok := s.index(c.slot.ResourceType, c.slot.Name)
				derived = derived || (ok && ti == ri && c.slot.Field == f)
			}
			if !derived {
				problems = append(problems, fmt.Sprintf("%s: %s is neither declared nor derived: declare it in %s, or declare the resource whose derivation supplies it", r.File, f, r.File))
			}
		}
	}
	return problems
}

// isImporting reports whether (resourceType, name) is the resource the set
// was loaded to import.
func (s *Set) isImporting(resourceType, name string) bool {
	return s.importing != nil && s.importing.resourceType == resourceType && s.importing.name == name
}

// checkSlot refuses a slot its resource type got wrong: a bug in the tool.
func checkSlot(r Resource, sl Slot, types map[string]ResourceType) string {
	bug := func(what string) string {
		return fmt.Sprintf("%s: the resource type %s returned a slot that %s, a bug in the tool", r.File, r.Type, what)
	}
	tt := types[sl.ResourceType]
	if tt == nil {
		return bug(fmt.Sprintf("targets %q, a resource type the tool does not supply", sl.ResourceType))
	}
	if (sl.Field == "") == (sl.Collection == "") {
		return bug("names both or neither of a field and a collection")
	}
	td := tt.Descriptor()
	if sl.Field != "" {
		if !contains(td.DerivableFields, sl.Field) {
			return bug(fmt.Sprintf("names the field %s, which is not one of %s's DerivableFields", sl.Field, sl.ResourceType))
		}
		return ""
	}
	c, ok := findCollection(td, sl.Collection)
	if !ok {
		return bug(fmt.Sprintf("names the collection %s, which %s does not have", sl.Collection, sl.ResourceType))
	}
	for _, k := range sortedKeys(sl.Match) {
		if !contains(c.Key, k) {
			return bug(fmt.Sprintf("matches %s, which is not a key field of %s (%s): a slot matches key fields only, so a hand-declared child sharing a derived child's key is one the slot covers, and two slots no child matches both of never derive one key", k, sl.Collection, strings.Join(c.Key, ", ")))
		}
		if m := sl.Match[k]; !m.isPrefix {
			if err := checkValue(m.exact, false); err != nil {
				return bug(fmt.Sprintf("matches %s against %v", k, err))
			}
		}
	}
	return ""
}

func describeSlot(sl Slot) string {
	if sl.Field != "" {
		return "the field " + sl.Field
	}
	return fmt.Sprintf("the %s children matching %s", sl.Collection, describeMatchers(sl.Match))
}

// overlap reports whether two slots on one target cover a common field or
// child.
func overlap(a, b Slot) bool {
	if a.Field != "" || b.Field != "" {
		return a.Field == b.Field
	}
	return a.Collection == b.Collection && compatibleMatchers(a.Match, b.Match)
}
