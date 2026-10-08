package index

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stricttools/strictspec/go/lifecycle"
)

// SubjectsOf is the key of rec's repository in the index: the values of its
// open releasable-name identities, sorted. A closed identity no longer names
// the repository's releasable (a rename closes it; an extract moves it to
// another repository), and a pending one does not name it yet.
func SubjectsOf(rec *lifecycle.Record) []string {
	var out []string
	for _, id := range rec.Identities() {
		if id.Facet == lifecycle.FacetReleasableName && !id.Pending() && id.Open() {
			out = append(out, id.Value)
		}
	}
	return cleanSubjects(out)
}

// Update is the change a record makes to its repository's entry: the entry
// keyed by Subjects holds Names while the repository is confidential and has
// names to protect, and is removed otherwise (Names empty). A confidential
// repository can have none: a public-client declaration takes its registry
// names and its own names out.
type Update struct {
	Subjects []string
	Names    []string

	confidential bool
}

// Confidential reports whether the record makes the repository confidential,
// whether or not it has names to protect. Plan sets it.
func (u Update) Confidential() bool { return u.confidential }

// Plan decides the update rec makes on the date of on. repositoryNames are
// the repository's own names (see RepositoryNames), which the record protects
// when no releasable carries a non-proprietary license. A confidential record
// without an open releasable-name identity is refused: the index keys an entry
// by them.
func Plan(rec *lifecycle.Record, on time.Time, repositoryNames ...string) (Update, error) {
	u := Update{Subjects: SubjectsOf(rec)}
	if !rec.Confidential(on) {
		return u, nil
	}
	if len(u.Subjects) == 0 {
		return Update{}, fmt.Errorf("the repository is confidential, and %s holds no open releasable-name identity, by which the confidential-name index keys a repository's names; record one for each releasable (rlsbl transition identity --facet releasable-name), then run this again", lifecycle.RecordFile)
	}
	names, err := rec.ConfidentialNames(on, repositoryNames...)
	if err != nil {
		return Update{}, err
	}
	u.Names = names
	u.confidential = true
	return u, nil
}

// Apply writes the update through w: the names upserted under the subjects of
// a confidential repository, or the entry removed for a public one and for a
// confidential one with no names to protect.
func (x *Index) Apply(w lifecycle.FileWriter, u Update) error {
	if len(u.Names) > 0 {
		return x.Upsert(w, u.Subjects, u.Names)
	}
	return x.Remove(w, u.Subjects)
}

// RepositoryNames are the names of the repository whose work tree is root: the
// name of its root directory, and, when origin (its origin remote's URL) is
// not empty, the last path segment of the origin.
func RepositoryNames(root, origin string) ([]string, error) {
	names := []string{filepath.Base(filepath.Clean(root))}
	if strings.TrimSpace(origin) != "" {
		norm, err := NormalizeOrigin(origin)
		if err != nil {
			return nil, fmt.Errorf("reading the origin remote %q: %w", origin, err)
		}
		names = append(names, path.Base(strings.TrimPrefix(norm, "file://")))
	}
	return slices.Compact(names), nil
}
