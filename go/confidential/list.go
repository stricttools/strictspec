// Package confidential reads the confidential-term list and finds its terms
// in what a tool is about to publish.
//
// The list is one TOML file encrypted with age, outside every repository:
// <home>/Projects/CONTEXT/confidential/terms.toml.age, decrypted with the
// identity in <home>/.age-key.txt. It is decrypted in memory only; the package
// never writes the plaintext anywhere. A missing list is a list with no terms,
// which every caller states plainly; a list that exists and cannot be read or
// decrypted is an error.
//
// The list's format:
//
//	format_version = 1
//
//	[[terms]]
//	term = "Example"                      # a literal, matched ignoring case
//	added = 2026-10-10
//	reason = "one line"
//	scope = "everywhere"
//
//	[[terms]]
//	pattern = "/home/[a-z]+"              # a regular expression (Go syntax), matched ignoring case
//	added = 2026-10-10
//	reason = "one line"
//	scope = { except = ["some-repository"] }
//
// Each entry has one and only one of term and pattern. A scope of
// "everywhere" applies the entry to every repository; except names the
// repositories (by directory or origin name) the entry does not apply to.
//
// Matching finds every occurrence, as a substring, ignoring case; a pattern
// decides its own boundaries. Every occurrence is a hit with an id derived
// from the entry and the words around it (see Hit), so the same phrase gets
// the same id in a file, a commit message, a changelog, and a Release body.
// Hits are resolved by the repository's recorded resolutions (see store.go):
// a false-positive judgment by the agent, or the owner's approval to publish
// the hit anyway.
package confidential

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"filippo.io/age"
	tomledit "github.com/stricttools/go-toml-edit"
)

const (
	// ListRel is the list's path relative to the user's home directory.
	ListRel = "Projects/CONTEXT/confidential/terms.toml.age"
	// IdentityRel is the age identity's path relative to the user's home
	// directory.
	IdentityRel = ".age-key.txt"
	// FormatVersion is the list format this package reads.
	FormatVersion = 1
	// ScopeEverywhere is the scope that applies an entry to every repository.
	ScopeEverywhere = "everywhere"
)

// Location is where the list and the identity that decrypts it are.
type Location struct {
	List     string
	Identity string
}

// DefaultLocation is the list and identity under the user's home directory.
func DefaultLocation() (Location, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Location{}, fmt.Errorf("locating the confidential-term list: %w", err)
	}
	return LocationIn(home), nil
}

// LocationIn is the list and identity under home.
func LocationIn(home string) Location {
	return Location{
		List:     filepath.Join(home, filepath.FromSlash(ListRel)),
		Identity: filepath.Join(home, filepath.FromSlash(IdentityRel)),
	}
}

// Entry is one entry of the list.
type Entry struct {
	// Term is the literal the entry protects; empty for a pattern entry.
	Term string
	// Pattern is the regular expression the entry protects; empty for a
	// term entry.
	Pattern string
	// Added is the day the entry was added, as YYYY-MM-DD.
	Added  string
	Reason string
	// Except names the repositories the entry does not apply to; empty for
	// an entry whose scope is everywhere.
	Except []string

	re *regexp.Regexp
}

// key is the entry's identity inside a hit id: the lowered term, or the
// pattern as written.
func (e Entry) key() string {
	if e.Pattern != "" {
		return "pattern\x00" + e.Pattern
	}
	return "term\x00" + strings.ToLower(e.Term)
}

// Describe names the entry for a report the agent reads: the term, or the
// pattern. Never write it into a committed file.
func (e Entry) Describe() string {
	if e.Pattern != "" {
		return fmt.Sprintf("the pattern %q", e.Pattern)
	}
	return fmt.Sprintf("the term %q", e.Term)
}

// appliesTo reports whether the entry applies to a repository with the
// given names.
func (e Entry) appliesTo(repositoryNames []string) bool {
	for _, ex := range e.Except {
		for _, n := range repositoryNames {
			if strings.EqualFold(strings.TrimSpace(n), ex) {
				return false
			}
		}
	}
	return true
}

// List is the confidential-term list as read.
type List struct {
	path    string
	missing bool
	entries []Entry
}

// Path is the list's file.
func (l *List) Path() string { return l.path }

// Missing reports whether the list's file does not exist.
func (l *List) Missing() bool { return l.missing }

// Entries returns the list's entries in file order.
func (l *List) Entries() []Entry {
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Status is one plain sentence about the list: that it does not exist, so
// there are no terms, or how many entries it holds.
func (l *List) Status() string {
	if l.missing {
		return fmt.Sprintf("the confidential-term list %s does not exist, so there are no confidential terms to scan for", l.path)
	}
	return fmt.Sprintf("the confidential-term list %s holds %d entr%s", l.path, len(l.entries), plural(len(l.entries), "y", "ies"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Load reads and decrypts the list at loc.List with the identity at
// loc.Identity. A missing list is an empty list with Missing set, and needs
// no identity. Every other failure is an error: an unreadable identity, a
// file age cannot decrypt, and a list the format refuses.
func Load(loc Location) (*List, error) {
	f, err := os.Open(loc.List)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &List{path: loc.List, missing: true}, nil
		}
		return nil, fmt.Errorf("opening the confidential-term list: %w", err)
	}
	defer f.Close()
	idSrc, err := os.ReadFile(loc.Identity)
	if err != nil {
		return nil, fmt.Errorf("the confidential-term list %s exists, and the age identity that decrypts it cannot be read: %w", loc.List, err)
	}
	ids, err := age.ParseIdentities(bytes.NewReader(idSrc))
	if err != nil {
		return nil, fmt.Errorf("reading the age identity %s: %w", loc.Identity, err)
	}
	r, err := age.Decrypt(f, ids...)
	if err != nil {
		return nil, fmt.Errorf("decrypting the confidential-term list %s with the identity %s: %w", loc.List, loc.Identity, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decrypting the confidential-term list %s: %w", loc.List, err)
	}
	entries, err := Parse(plain)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", loc.List, err)
	}
	return &List{path: loc.List, entries: entries}, nil
}

// FromEntries is a list holding entries, read from nowhere: for a caller that
// holds the entries already, and for tests. Each entry is checked as Parse
// checks it.
func FromEntries(path string, entries []Entry) (*List, error) {
	out := make([]Entry, 0, len(entries))
	for i, e := range entries {
		checked, problems := check(i, e)
		if len(problems) > 0 {
			return nil, errors.New(strings.Join(problems, "; "))
		}
		out = append(out, checked)
	}
	return &List{path: path, entries: out}, nil
}

type rawList struct {
	FormatVersion int64      `toml:"format_version,required"`
	Terms         []rawEntry `toml:"terms"`
}

type rawEntry struct {
	Term    *string            `toml:"term"`
	Pattern *string            `toml:"pattern"`
	Added   tomledit.LocalDate `toml:"added,required"`
	Reason  string             `toml:"reason,required"`
	Scope   any                `toml:"scope,required"`
}

// Parse reads the decrypted list. Unknown keys, a wrong format version, an
// entry with both or neither of term and pattern, an empty term, a pattern Go
// cannot compile or that matches the empty string, an empty reason, and a
// scope other than "everywhere" or { except = [...] } with at least one
// repository are refused, every problem at once.
func Parse(plaintext []byte) ([]Entry, error) {
	raw, err := tomledit.Unmarshal[rawList](plaintext)
	if err != nil {
		return nil, err
	}
	if raw.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("format_version is %d; this reader reads %d", raw.FormatVersion, FormatVersion)
	}
	var problems []string
	var out []Entry
	for i, r := range raw.Terms {
		e := Entry{
			Added:  fmt.Sprintf("%04d-%02d-%02d", r.Added.Year, r.Added.Month, r.Added.Day),
			Reason: r.Reason,
		}
		if r.Term != nil {
			e.Term = *r.Term
		}
		if r.Pattern != nil {
			e.Pattern = *r.Pattern
		}
		if r.Term != nil && r.Pattern != nil {
			problems = append(problems, fmt.Sprintf("entry %d has both term and pattern; give one", i+1))
			continue
		}
		if r.Term == nil && r.Pattern == nil {
			problems = append(problems, fmt.Sprintf("entry %d has neither term nor pattern; give one", i+1))
			continue
		}
		except, scopeProblems := parseScope(i, r.Scope)
		problems = append(problems, scopeProblems...)
		e.Except = except
		checked, entryProblems := check(i, e)
		problems = append(problems, entryProblems...)
		if len(scopeProblems) == 0 && len(entryProblems) == 0 {
			out = append(out, checked)
		}
	}
	if len(problems) > 0 {
		return nil, errors.New("the confidential-term list is refused:\n  " + strings.Join(problems, "\n  "))
	}
	return out, nil
}

func parseScope(i int, scope any) ([]string, []string) {
	switch s := scope.(type) {
	case string:
		if s == ScopeEverywhere {
			return nil, nil
		}
	case map[string]any:
		list, ok := s["except"].([]any)
		if len(s) == 1 && ok && len(list) > 0 {
			var except []string
			for _, x := range list {
				name, ok := x.(string)
				if !ok || strings.TrimSpace(name) == "" {
					return nil, []string{fmt.Sprintf("entry %d: every repository scope.except names is a non-empty string", i+1)}
				}
				except = append(except, strings.TrimSpace(name))
			}
			sort.Strings(except)
			return except, nil
		}
	}
	return nil, []string{fmt.Sprintf("entry %d: scope is \"everywhere\" or { except = [\"<repository>\", ...] } naming at least one repository", i+1)}
}

// check validates one entry and compiles its matcher.
func check(i int, e Entry) (Entry, []string) {
	var problems []string
	where := fmt.Sprintf("entry %d", i+1)
	if strings.TrimSpace(e.Reason) == "" || strings.Contains(e.Reason, "\n") {
		problems = append(problems, where+": reason is one non-empty line")
	}
	switch {
	case e.Term != "" && e.Pattern != "":
		problems = append(problems, where+": has both term and pattern; give one")
	case e.Pattern != "":
		re, err := regexp.Compile("(?i)" + e.Pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: the pattern does not compile: %v", where, err))
		} else if re.MatchString("") {
			problems = append(problems, where+": the pattern matches the empty string, so it would match everywhere")
		} else {
			e.re = re
		}
	case strings.TrimSpace(e.Term) == "":
		problems = append(problems, where+": term is empty")
	default:
		e.re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(e.Term))
	}
	return e, problems
}

// For is the matcher of the entries that apply to a repository known by
// repositoryNames (see RepositoryNames): every entry except those whose
// scope exempts one of the names.
func (l *List) For(repositoryNames []string) *Matcher {
	m := &Matcher{list: l}
	for _, e := range l.entries {
		if e.appliesTo(repositoryNames) {
			m.entries = append(m.entries, e)
		}
	}
	return m
}
