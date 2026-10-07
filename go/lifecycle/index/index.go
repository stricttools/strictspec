// Package index reads, updates, and scans the machine-local confidential-name
// index: one file outside every repository, at
// <os.UserConfigDir()>/strictspec/confidential-names.toml, holding the names
// each confidential repository protects. Tools upsert a repository's entry
// from mutating commands only, remove it when the repository turns out public,
// and scan what a public repository commits or publishes against every name in
// it.
//
// An entry is keyed by its repository's lifecycle-and-license record, never by
// a remote: by the values of the record's open releasable-name identities (see
// SubjectsOf), so a repository without an origin remote has an entry, and
// finding it asks no network.
//
// The package performs no file write of its own: Upsert, Remove, Rename, and
// Apply write through the lifecycle.FileWriter the caller injects.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tomledit "github.com/stricttools/go-toml-edit"

	"github.com/stricttools/strictspec/go/lifecycle"
)

// FileName is the index's file name inside <os.UserConfigDir()>/strictspec.
const FileName = "confidential-names.toml"

// FormatVersion is the index format this package reads and writes.
const FormatVersion = 1

// DefaultPath is the index's machine-local path:
// <os.UserConfigDir()>/strictspec/confidential-names.toml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the confidential-name index: %w", err)
	}
	return filepath.Join(dir, "strictspec", FileName), nil
}

// Entry is one repository's entry: the subjects that key it and the names it
// protects.
type Entry struct {
	// Subjects are the values of the record's open releasable-name
	// identities when the entry was written (see SubjectsOf), sorted. No
	// subject keys more than one entry.
	Subjects []string
	Names    []string
}

// Index is the confidential-name index as read from its file.
type Index struct {
	path    string
	entries []Entry
}

type fileShape struct {
	FormatVersion int         `toml:"format_version,required"`
	Repositories  []repoShape `toml:"repositories"`
}

type repoShape struct {
	Subjects []string `toml:"subjects,required"`
	Names    []string `toml:"names,required"`
}

// Load reads the index at path. A missing file is an empty index (the file is
// created on the first Upsert). Unknown keys, a wrong format version, an entry
// without subjects or names, subjects not sorted, and a subject keying more
// than one entry are refused.
func Load(path string) (*Index, error) {
	x := &Index{path: path}
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return x, nil
		}
		return nil, err
	}
	shape, err := tomledit.Unmarshal[fileShape](src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if shape.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("%s: format_version is %d; this index reader reads %d", path, shape.FormatVersion, FormatVersion)
	}
	owner := map[string]int{}
	for i, r := range shape.Repositories {
		clean := cleanSubjects(r.Subjects)
		if len(clean) == 0 {
			return nil, fmt.Errorf("%s: entry %d has no subjects; every entry is keyed by the releasable names of its repository", path, i+1)
		}
		if !equalStrings(clean, r.Subjects) {
			return nil, fmt.Errorf("%s: entry %d has the subjects %q, which are not sorted, unique, and trimmed (they read %q so)", path, i+1, r.Subjects, clean)
		}
		for _, s := range clean {
			if j, ok := owner[s]; ok {
				return nil, fmt.Errorf("%s: the subject %q keys entries %d and %d; a subject keys one entry", path, s, j+1, i+1)
			}
			owner[s] = i
		}
		if len(r.Names) == 0 {
			return nil, fmt.Errorf("%s: entry %d (%s) has no names; an entry exists only for a confidential repository", path, i+1, strings.Join(clean, ", "))
		}
		x.entries = append(x.entries, Entry{Subjects: clean, Names: append([]string(nil), r.Names...)})
	}
	return x, nil
}

// Path is the file the index was read from and is written to.
func (x *Index) Path() string { return x.path }

// Entries returns the index's entries, ordered by their first subject.
func (x *Index) Entries() []Entry {
	out := make([]Entry, len(x.entries))
	for i, e := range x.entries {
		out[i] = copyEntry(e)
	}
	return out
}

// Held returns the entries keyed by any of subjects.
func (x *Index) Held(subjects []string) []Entry {
	want := setOf(cleanSubjects(subjects))
	var out []Entry
	for _, e := range x.entries {
		if shares(e.Subjects, want) {
			out = append(out, copyEntry(e))
		}
	}
	return out
}

// Names returns every name of every entry, deduplicated ignoring case and
// sorted.
func (x *Index) Names() []string {
	var all []string
	for _, e := range x.entries {
		all = append(all, e.Names...)
	}
	return cleanNames(all)
}

// Upsert writes the entry of the repository whose subjects are subjects (see
// SubjectsOf) with names, through w, when anything changed. Every entry keyed
// by one of subjects is the repository's: it is replaced, except that its
// subjects outside subjects keep their own entry with the names it held, since
// a releasable another repository took over (an extract) is that
// repository's to rewrite. An empty name list is refused: a repository with
// no confidential names is removed with Remove.
func (x *Index) Upsert(w lifecycle.FileWriter, subjects, names []string) error {
	key := cleanSubjects(subjects)
	if len(key) == 0 {
		return fmt.Errorf("upserting names with no subjects; the index keys a repository by the values of its open releasable-name identities")
	}
	clean := cleanNames(names)
	if len(clean) == 0 {
		return fmt.Errorf("upserting %s with no names; a repository with no confidential names is removed from the index, not upserted", strings.Join(key, ", "))
	}
	next := append(x.without(key), Entry{Subjects: key, Names: clean})
	return x.write(w, next)
}

// Remove drops the entries keyed by subjects, through w, when the index holds
// any; their other subjects keep their own entry with the names it held.
func (x *Index) Remove(w lifecycle.FileWriter, subjects []string) error {
	key := cleanSubjects(subjects)
	if len(key) == 0 {
		return nil
	}
	return x.write(w, x.without(key))
}

// Rename moves the subject from to the name to, through w, as a renamed
// releasable's repository does: the entry keyed by from is keyed by to
// instead, merged (subjects and names alike) with the entry to keys already.
// An index without from is left as it is.
func (x *Index) Rename(w lifecycle.FileWriter, from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return fmt.Errorf("renaming a subject of the index needs the old and the new name")
	}
	var moved *Entry
	var next []Entry
	for _, e := range x.Entries() {
		if slices.Contains(e.Subjects, from) {
			e.Subjects = slices.DeleteFunc(e.Subjects, func(s string) bool { return s == from })
			e.Subjects = append(e.Subjects, to)
			moved = &e
			continue
		}
		next = append(next, e)
	}
	if moved == nil {
		return nil
	}
	merged := *moved
	var rest []Entry
	for _, e := range next {
		if slices.Contains(e.Subjects, to) {
			merged.Subjects = append(merged.Subjects, e.Subjects...)
			merged.Names = append(merged.Names, e.Names...)
			continue
		}
		rest = append(rest, e)
	}
	merged.Subjects = cleanSubjects(merged.Subjects)
	merged.Names = cleanNames(merged.Names)
	return x.write(w, append(rest, merged))
}

// Scan matches text against every name in the index (see ScanTerms).
func (x *Index) Scan(text string) []Match {
	return ScanTerms(text, x.Names())
}

// without is the entries with every subject of key taken out; an entry left
// with no subject is dropped.
func (x *Index) without(key []string) []Entry {
	drop := setOf(key)
	var out []Entry
	for _, e := range x.Entries() {
		if !shares(e.Subjects, drop) {
			out = append(out, e)
			continue
		}
		rest := slices.DeleteFunc(e.Subjects, func(s string) bool { return drop[s] })
		if len(rest) > 0 {
			out = append(out, Entry{Subjects: rest, Names: e.Names})
		}
	}
	return out
}

// write writes entries through w when they differ from the index's own.
func (x *Index) write(w lifecycle.FileWriter, entries []Entry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Subjects[0] < entries[j].Subjects[0] })
	if equalEntries(entries, x.entries) {
		return nil
	}
	src, err := render(entries)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(filepath.Dir(x.path)); err != nil {
		return err
	}
	if err := w.WriteFile(x.path, src); err != nil {
		return err
	}
	x.entries = entries
	return nil
}

func render(entries []Entry) ([]byte, error) {
	d, err := tomledit.Parse([]byte(fmt.Sprintf("format_version = %d\n", FormatVersion)))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if err := d.NewArrayTable("repositories"); err != nil {
			return nil, err
		}
		if err := d.Set("repositories[-1].subjects", e.Subjects); err != nil {
			return nil, err
		}
		if err := d.Set("repositories[-1].names", e.Names); err != nil {
			return nil, err
		}
	}
	return d.Bytes(), nil
}

// cleanNames trims names, drops empty ones and ones equal ignoring case to an
// earlier one in sorted order, and sorts the rest.
func cleanNames(names []string) []string {
	trimmed := make([]string, 0, len(names))
	for _, n := range names {
		if t := strings.TrimSpace(n); t != "" {
			trimmed = append(trimmed, t)
		}
	}
	sort.Strings(trimmed)
	seen := map[string]bool{}
	out := []string{}
	for _, n := range trimmed {
		k := strings.ToLower(n)
		if !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// cleanSubjects trims subjects, drops empty and repeated ones, and sorts the
// rest. Subjects are releasable names, compared as written.
func cleanSubjects(subjects []string) []string {
	out := []string{}
	for _, s := range subjects {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func setOf(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

func shares(subjects []string, set map[string]bool) bool {
	for _, s := range subjects {
		if set[s] {
			return true
		}
	}
	return false
}

func copyEntry(e Entry) Entry {
	return Entry{Subjects: append([]string(nil), e.Subjects...), Names: append([]string(nil), e.Names...)}
}

func equalEntries(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalStrings(a[i].Subjects, b[i].Subjects) || !equalStrings(a[i].Names, b[i].Names) {
			return false
		}
	}
	return true
}
