// Package index reads, updates, and scans the machine-local confidential-name
// index: one file outside every repository, at
// <os.UserConfigDir()>/strictspec/confidential-names.toml, holding the names
// each confidential repository protects. Tools upsert a repository's entry
// from mutating commands only, remove it when the repository turns out public,
// and scan what a public repository commits or publishes against every name in
// it.
//
// The package performs no file write of its own: Upsert and Remove write
// through the lifecycle.FileWriter the caller injects.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Entry is one repository's entry: its normalized origin and the names it
// protects.
type Entry struct {
	Origin string
	Names  []string
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
	Origin string   `toml:"origin,required"`
	Names  []string `toml:"names,required"`
}

// Load reads the index at path. A missing file is an empty index (the file is
// created on the first Upsert). Unknown keys, a wrong format version, a
// repeated or unnormalized origin, and an entry without names are refused.
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
	seen := map[string]bool{}
	for _, r := range shape.Repositories {
		norm, err := renormalizeStored(r.Origin)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if norm != r.Origin {
			return nil, fmt.Errorf("%s: origin %q is not normalized (it reads %q normalized); the index holds normalized origins only", path, r.Origin, norm)
		}
		if seen[r.Origin] {
			return nil, fmt.Errorf("%s: origin %q has more than one entry", path, r.Origin)
		}
		seen[r.Origin] = true
		if len(r.Names) == 0 {
			return nil, fmt.Errorf("%s: origin %q has no names; an entry exists only for a confidential repository", path, r.Origin)
		}
		x.entries = append(x.entries, Entry{Origin: r.Origin, Names: append([]string(nil), r.Names...)})
	}
	return x, nil
}

// Path is the file the index was read from and is written to.
func (x *Index) Path() string { return x.path }

// Entries returns the index's entries in origin order.
func (x *Index) Entries() []Entry {
	out := make([]Entry, len(x.entries))
	for i, e := range x.entries {
		out[i] = Entry{Origin: e.Origin, Names: append([]string(nil), e.Names...)}
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

// Upsert sets the names of origin's entry (origin is normalized first) and
// writes the index through w when anything changed. An empty name list is
// refused: a repository with no confidential names is removed with Remove.
func (x *Index) Upsert(w lifecycle.FileWriter, origin string, names []string) error {
	norm, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	clean := cleanNames(names)
	if len(clean) == 0 {
		return fmt.Errorf("upserting %s with no names; a repository with no confidential names is removed from the index, not upserted", norm)
	}
	for i, e := range x.entries {
		if e.Origin == norm {
			if equalStrings(e.Names, clean) {
				return nil
			}
			next := x.Entries()
			next[i].Names = clean
			return x.write(w, next)
		}
	}
	next := append(x.Entries(), Entry{Origin: norm, Names: clean})
	return x.write(w, next)
}

// Remove drops origin's entry (origin is normalized first) and writes the
// index through w when it had one.
func (x *Index) Remove(w lifecycle.FileWriter, origin string) error {
	norm, err := NormalizeOrigin(origin)
	if err != nil {
		return err
	}
	var next []Entry
	found := false
	for _, e := range x.Entries() {
		if e.Origin == norm {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return nil
	}
	return x.write(w, next)
}

// Scan matches text against every name in the index (see ScanTerms).
func (x *Index) Scan(text string) []Match {
	return ScanTerms(text, x.Names())
}

func (x *Index) write(w lifecycle.FileWriter, entries []Entry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Origin < entries[j].Origin })
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
		if err := d.Set("repositories[-1].origin", e.Origin); err != nil {
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
