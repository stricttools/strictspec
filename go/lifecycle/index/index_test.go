package index_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/strictspec/go/lifecycle"
	"github.com/stricttools/strictspec/go/lifecycle/index"
)

type recordingWriter struct {
	files map[string][]byte
	dirs  []string
}

func newRecordingWriter() *recordingWriter {
	return &recordingWriter{files: map[string][]byte{}}
}

func (w *recordingWriter) WriteFile(path string, data []byte) error {
	w.files[path] = append([]byte(nil), data...)
	return nil
}

func (w *recordingWriter) MkdirAll(path string) error {
	w.dirs = append(w.dirs, path)
	return nil
}

type diskWriter struct{}

func (diskWriter) WriteFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
func (diskWriter) MkdirAll(path string) error               { return os.MkdirAll(path, 0o755) }

func indexPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config", "strictspec", index.FileName)
}

func load(t *testing.T, path string) *index.Index {
	t.Helper()
	x, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func parse(t *testing.T, src string) *lifecycle.Record {
	t.Helper()
	r, err := lifecycle.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return r
}

// confidentialRecord holds two releasables: portal, proprietary and renamed
// from oldportal, and client, public.
const confidentialRecord = `format_version = 1
codenames = ["Bluebird"]

[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-01-01
reason = "closed"

[[licenses]]
subject = "client"
license = "MIT"
from = 2026-01-01
reason = "open"

[[identities]]
subject = "portal"
facet = "releasable-name"
value = "oldportal"
registry = ""
tag_patterns = ["oldportal-v*"]
from = 2025-01-01
until = 2026-01-01
reason = "first name"

[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["portal-v*"]
from = 2026-01-01
reason = "renamed"

[[identities]]
subject = "client"
facet = "releasable-name"
value = "client"
registry = ""
tag_patterns = ["client-v*"]
from = 2026-01-01
reason = "created"
`

func entries(t *testing.T, path string) []index.Entry {
	t.Helper()
	return load(t, path).Entries()
}

func TestDefaultPathIsUnderTheUserConfigDirectory(t *testing.T) {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config directory on this machine")
	}
	got, err := index.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "strictspec", "confidential-names.toml") {
		t.Fatalf("default path %s", got)
	}
}

func TestAMissingIndexIsEmpty(t *testing.T) {
	x := load(t, indexPath(t))
	if len(x.Entries()) != 0 || len(x.Scan("portal")) != 0 {
		t.Fatal("a missing index holds names")
	}
}

func TestSubjectsAreTheOpenReleasableNameIdentities(t *testing.T) {
	got := index.SubjectsOf(parse(t, confidentialRecord))
	if want := []string{"client", "portal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects %v, want %v", got, want)
	}
	if got := index.SubjectsOf(&lifecycle.Record{}); len(got) != 0 {
		t.Fatalf("an absent record has subjects %v", got)
	}
}

// A confidential repository is keyed by its record alone: no origin remote,
// no network.
func TestPlanKeysAConfidentialRecordByItsSubjects(t *testing.T) {
	u, err := index.Plan(parse(t, confidentialRecord), day(t, "2026-06-01"), "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(u.Subjects, []string{"client", "portal"}) || !u.Confidential() {
		t.Fatalf("update %+v", u)
	}
	if !reflect.DeepEqual(u.Names, []string{"Bluebird"}) {
		t.Fatalf("names %v, want the codename alone (releasable names and other identity values are not confidential, and a public releasable leaves the repository's names out)", u.Names)
	}
	path := indexPath(t)
	x := load(t, path)
	if err := x.Apply(diskWriter{}, u); err != nil {
		t.Fatal(err)
	}
	got := entries(t, path)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Subjects, []string{"client", "portal"}) || !reflect.DeepEqual(got[0].Names, u.Names) {
		t.Fatalf("entries %+v", got)
	}
}

func TestPlanRefusesAConfidentialRecordWithoutAReleasableName(t *testing.T) {
	rec := parse(t, `format_version = 1

[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-01-01
reason = "closed"
`)
	_, err := index.Plan(rec, day(t, "2026-06-01"), "portal")
	if err == nil || !strings.Contains(err.Error(), "releasable-name") {
		t.Fatalf("want a refusal naming the releasable-name identity, got %v", err)
	}
}

func TestPlanOfAPublicRecordRemoves(t *testing.T) {
	path := indexPath(t)
	x := load(t, path)
	if err := x.Upsert(diskWriter{}, []string{"portal"}, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	u, err := index.Plan(parse(t, confidentialRecord), day(t, "2025-06-01"), "")
	if err != nil {
		t.Fatal(err)
	}
	if u.Confidential() {
		t.Fatalf("a public record planned names %v", u.Names)
	}
	if err := x.Apply(diskWriter{}, u); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, path); len(got) != 0 {
		t.Fatalf("entries after a public plan %+v", got)
	}
	// A public record without subjects has nothing to remove.
	w := newRecordingWriter()
	u, err = index.Plan(&lifecycle.Record{}, day(t, "2026-06-01"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Apply(w, u); err != nil || len(w.files) != 0 {
		t.Fatalf("an absent record wrote %v (%v)", w.files, err)
	}
}

func TestUpsertGoesThroughTheWriterOnly(t *testing.T) {
	path := indexPath(t)
	x := load(t, path)
	w := newRecordingWriter()
	if err := x.Upsert(w, []string{"portal", " "}, []string{"portal", " Portal ", "Bluebird", ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the library created the index directory itself (%v)", err)
	}
	if len(w.dirs) != 1 || w.dirs[0] != filepath.Dir(path) {
		t.Fatalf("directories %v", w.dirs)
	}
	got := string(w.files[path])
	if !strings.HasPrefix(got, "format_version = 1\n") || !strings.Contains(got, `subjects = ["portal"]`) {
		t.Fatalf("index file:\n%s", got)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, w.files[path], 0o644); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, path); len(got) != 1 || !reflect.DeepEqual(got[0].Names, []string{"Bluebird", "Portal"}) {
		t.Fatalf("read back %+v", got)
	}
}

func TestUpsertReplacesTheEntrySharingASubjectAndRemoveDropsIt(t *testing.T) {
	path := indexPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	x := load(t, path)
	if err := x.Upsert(diskWriter{}, []string{"portal"}, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(diskWriter{}, []string{"gadget"}, []string{"gadget"}); err != nil {
		t.Fatal(err)
	}
	// The repository gained a releasable: its entry is replaced.
	if err := x.Upsert(diskWriter{}, []string{"portal", "server"}, []string{"portal", "server"}); err != nil {
		t.Fatal(err)
	}
	got := entries(t, path)
	if len(got) != 2 || !reflect.DeepEqual(got[0].Subjects, []string{"gadget"}) || !reflect.DeepEqual(got[1].Subjects, []string{"portal", "server"}) {
		t.Fatalf("entries %+v", got)
	}

	// An unchanged upsert writes nothing.
	w := newRecordingWriter()
	if err := load(t, path).Upsert(w, []string{"server", "portal"}, []string{"server", "portal"}); err != nil {
		t.Fatal(err)
	}
	if len(w.files) != 0 {
		t.Fatalf("an unchanged upsert wrote %v", w.files)
	}

	if err := load(t, path).Remove(diskWriter{}, []string{"portal", "server"}); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, path); len(got) != 1 || got[0].Subjects[0] != "gadget" {
		t.Fatalf("entries after removal %+v", got)
	}
	// Removing subjects the index does not hold writes nothing.
	w = newRecordingWriter()
	if err := load(t, path).Remove(w, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if len(w.files) != 0 {
		t.Fatalf("removing absent subjects wrote %v", w.files)
	}
}

// An extract splits one repository's releasables into two repositories: each
// takes its own subjects, and the names of the subjects it no longer holds
// stay keyed by them until the repository holding them writes its entry.
func TestAnEntryNamingSubjectsARepositoryNoLongerHoldsKeepsTheirNames(t *testing.T) {
	path := indexPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := load(t, path).Upsert(diskWriter{}, []string{"portal", "server"}, []string{"portal", "server"}); err != nil {
		t.Fatal(err)
	}
	if err := load(t, path).Upsert(diskWriter{}, []string{"portal"}, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	got := entries(t, path)
	if len(got) != 2 || !reflect.DeepEqual(got[0].Subjects, []string{"portal"}) || !reflect.DeepEqual(got[1].Subjects, []string{"server"}) ||
		!reflect.DeepEqual(got[1].Names, []string{"portal", "server"}) {
		t.Fatalf("entries %+v", got)
	}
	if err := load(t, path).Upsert(diskWriter{}, []string{"server"}, []string{"server"}); err != nil {
		t.Fatal(err)
	}
	got = entries(t, path)
	if len(got) != 2 || !reflect.DeepEqual(got[1].Names, []string{"server"}) {
		t.Fatalf("entries %+v", got)
	}
}

func TestRenameMovesASubjectToItsNewName(t *testing.T) {
	path := indexPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := load(t, path).Upsert(diskWriter{}, []string{"oldportal"}, []string{"oldportal"}); err != nil {
		t.Fatal(err)
	}
	// The rename's commit wrote an entry under the new name already.
	if err := load(t, path).Upsert(diskWriter{}, []string{"portal"}, []string{"oldportal", "portal"}); err != nil {
		t.Fatal(err)
	}
	if err := load(t, path).Rename(diskWriter{}, "oldportal", "portal"); err != nil {
		t.Fatal(err)
	}
	got := entries(t, path)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Subjects, []string{"portal"}) || !reflect.DeepEqual(got[0].Names, []string{"oldportal", "portal"}) {
		t.Fatalf("entries %+v", got)
	}
	// A name the index does not hold writes nothing.
	w := newRecordingWriter()
	if err := load(t, path).Rename(w, "gadget", "widget"); err != nil || len(w.files) != 0 {
		t.Fatalf("renaming an absent subject wrote %v (%v)", w.files, err)
	}
}

func TestUpsertWithNoNamesOrNoSubjectsIsRefused(t *testing.T) {
	x := load(t, indexPath(t))
	err := x.Upsert(newRecordingWriter(), []string{"portal"}, []string{" "})
	if err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("want a refusal naming removal, got %v", err)
	}
	err = x.Upsert(newRecordingWriter(), nil, []string{"portal"})
	if err == nil || !strings.Contains(err.Error(), "subjects") {
		t.Fatalf("want a refusal naming the subjects, got %v", err)
	}
}

func TestLoadRefusals(t *testing.T) {
	cases := map[string]string{
		"unknown key":      "format_version = 1\nowner = \"x\"\n",
		"wrong format":     "format_version = 2\n",
		"missing format":   "[[repositories]]\nsubjects = [\"r\"]\nnames = [\"r\"]\n",
		"origin key":       "format_version = 1\n[[repositories]]\norigin = \"github.com/o/r\"\nnames = [\"r\"]\n",
		"no subjects":      "format_version = 1\n[[repositories]]\nsubjects = []\nnames = [\"r\"]\n",
		"empty subject":    "format_version = 1\n[[repositories]]\nsubjects = [\" \"]\nnames = [\"r\"]\n",
		"unsorted":         "format_version = 1\n[[repositories]]\nsubjects = [\"s\", \"r\"]\nnames = [\"r\"]\n",
		"shared subject":   "format_version = 1\n[[repositories]]\nsubjects = [\"r\"]\nnames = [\"r\"]\n[[repositories]]\nsubjects = [\"r\", \"s\"]\nnames = [\"s\"]\n",
		"repeated subject": "format_version = 1\n[[repositories]]\nsubjects = [\"r\", \"r\"]\nnames = [\"r\"]\n",
		"no names":         "format_version = 1\n[[repositories]]\nsubjects = [\"r\"]\nnames = []\n",
	}
	for name, src := range cases {
		path := indexPath(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := index.Load(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestIndexScanUsesEveryRepositorysNames(t *testing.T) {
	x := load(t, indexPath(t))
	if err := x.Upsert(newRecordingWriter(), []string{"portal"}, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(newRecordingWriter(), []string{"gadget"}, []string{"gadget", "Portal"}); err != nil {
		t.Fatal(err)
	}
	got := x.Scan("the gadget talks to\nPORTAL today")
	want := []index.Match{{Term: "gadget", Line: 1, Column: 5}, {Term: "Portal", Line: 2, Column: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches %+v, want %+v", got, want)
	}
}

func TestHeldNamesTheEntriesSharingASubject(t *testing.T) {
	x := load(t, indexPath(t))
	if err := x.Upsert(newRecordingWriter(), []string{"portal"}, []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(newRecordingWriter(), []string{"gadget"}, []string{"gadget"}); err != nil {
		t.Fatal(err)
	}
	got := x.Held([]string{"portal", "server"})
	if len(got) != 1 || got[0].Subjects[0] != "portal" {
		t.Fatalf("held %+v", got)
	}
}

func TestRepositoryNames(t *testing.T) {
	got, err := index.RepositoryNames("/work/portal-checkout", "git@github.com:owner/portal.git")
	if err != nil || !reflect.DeepEqual(got, []string{"portal-checkout", "portal"}) {
		t.Fatalf("names %v, %v", got, err)
	}
	got, err = index.RepositoryNames("/work/portal", "")
	if err != nil || !reflect.DeepEqual(got, []string{"portal"}) {
		t.Fatalf("names without an origin %v, %v", got, err)
	}
	if _, err := index.RepositoryNames("/work/portal", "ftp://example.com/portal"); err == nil {
		t.Fatal("an unreadable origin was accepted")
	}
}

func TestNormalizeOrigin(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/portal.git":    "github.com/owner/portal",
		"https://GitHub.com/owner/portal/":       "github.com/owner/portal",
		"http://github.com/owner/portal":         "github.com/owner/portal",
		"ssh://git@github.com:22/owner/portal":   "github.com/owner/portal",
		"git@github.com:owner/portal.git":        "github.com/owner/portal",
		"github.com:owner/portal.git":            "github.com/owner/portal",
		"gp:owner/portal.git":                    "gp/owner/portal",
		"git+ssh://git@github.com/owner/portal":  "github.com/owner/portal",
		"file:///srv/remotes/portal.git":         "file:///srv/remotes/portal",
		"/srv/remotes/portal.git/":               "file:///srv/remotes/portal",
		"  https://github.com/owner/portal.git ": "github.com/owner/portal",
	}
	for in, want := range cases {
		got, err := index.NormalizeOrigin(in)
		if err != nil || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "portal", "../portal", "ftp://example.com/portal", "https://github.com/"} {
		if got, err := index.NormalizeOrigin(bad); err == nil {
			t.Errorf("NormalizeOrigin(%q) = %q, accepted", bad, got)
		}
	}
}
