package index_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	x, err := index.Load(indexPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Entries()) != 0 || len(x.Scan("portal")) != 0 {
		t.Fatal("a missing index holds names")
	}
}

func TestUpsertGoesThroughTheWriterOnly(t *testing.T) {
	path := indexPath(t)
	x, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w := newRecordingWriter()
	if err := x.Upsert(w, "git@github.com:Owner/Portal.git", []string{"portal", " Portal ", "Bluebird", ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the library created the index directory itself (%v)", err)
	}
	if len(w.dirs) != 1 || w.dirs[0] != filepath.Dir(path) {
		t.Fatalf("directories %v", w.dirs)
	}
	want := "format_version = 1\n"
	got := string(w.files[path])
	if !strings.HasPrefix(got, want) || !strings.Contains(got, `origin = "github.com/Owner/Portal"`) {
		t.Fatalf("index file:\n%s", got)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, w.files[path], 0o644); err != nil {
		t.Fatal(err)
	}
	reread, err := index.Load(path)
	if err != nil {
		t.Fatalf("the written index does not read back: %v", err)
	}
	if got := reread.Entries(); len(got) != 1 || !reflect.DeepEqual(got[0].Names, []string{"Bluebird", "Portal"}) {
		t.Fatalf("read back %+v", got)
	}
	entries := x.Entries()
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].Names, []string{"Bluebird", "Portal"}) {
		t.Fatalf("entries %+v", entries)
	}
}

func TestUpsertReplacesAndRemoveDrops(t *testing.T) {
	path := indexPath(t)
	x, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(diskWriter{}, "https://github.com/owner/portal", []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(diskWriter{}, "https://github.com/owner/gadget.git", []string{"gadget"}); err != nil {
		t.Fatal(err)
	}
	// The same remote spelled another way replaces the entry.
	if err := x.Upsert(diskWriter{}, "ssh://git@github.com/owner/portal.git", []string{"portal", "Bluebird"}); err != nil {
		t.Fatal(err)
	}
	again, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := again.Entries()
	if len(entries) != 2 || entries[0].Origin != "github.com/owner/gadget" || entries[1].Origin != "github.com/owner/portal" ||
		!reflect.DeepEqual(entries[1].Names, []string{"Bluebird", "portal"}) {
		t.Fatalf("entries %+v", entries)
	}

	// An unchanged upsert writes nothing.
	w := newRecordingWriter()
	if err := again.Upsert(w, "https://github.com/owner/portal", []string{"Bluebird", "portal"}); err != nil {
		t.Fatal(err)
	}
	if len(w.files) != 0 {
		t.Fatalf("an unchanged upsert wrote %v", w.files)
	}

	if err := again.Remove(diskWriter{}, "git@github.com:owner/portal.git"); err != nil {
		t.Fatal(err)
	}
	third, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := third.Entries(); len(got) != 1 || got[0].Origin != "github.com/owner/gadget" {
		t.Fatalf("entries after removal %+v", got)
	}
	// Removing an origin the index does not hold writes nothing.
	w = newRecordingWriter()
	if err := third.Remove(w, "https://github.com/owner/portal"); err != nil {
		t.Fatal(err)
	}
	if len(w.files) != 0 {
		t.Fatalf("removing an absent origin wrote %v", w.files)
	}
}

func TestUpsertWithNoNamesIsRefused(t *testing.T) {
	x, err := index.Load(indexPath(t))
	if err != nil {
		t.Fatal(err)
	}
	err = x.Upsert(newRecordingWriter(), "https://github.com/owner/portal", []string{" "})
	if err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("want a refusal naming removal, got %v", err)
	}
}

func TestLoadRefusals(t *testing.T) {
	cases := map[string]string{
		"unknown key":         "format_version = 1\nowner = \"x\"\n",
		"wrong format":        "format_version = 2\n",
		"missing format":      "[[repositories]]\norigin = \"github.com/o/r\"\nnames = [\"r\"]\n",
		"unnormalized origin": "format_version = 1\n[[repositories]]\norigin = \"https://github.com/o/r.git\"\nnames = [\"r\"]\n",
		"repeated origin":     "format_version = 1\n[[repositories]]\norigin = \"github.com/o/r\"\nnames = [\"r\"]\n[[repositories]]\norigin = \"github.com/o/r\"\nnames = [\"s\"]\n",
		"no names":            "format_version = 1\n[[repositories]]\norigin = \"github.com/o/r\"\nnames = []\n",
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
	path := indexPath(t)
	x, err := index.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(newRecordingWriter(), "https://github.com/owner/portal", []string{"portal"}); err != nil {
		t.Fatal(err)
	}
	if err := x.Upsert(newRecordingWriter(), "https://github.com/owner/gadget", []string{"gadget", "Portal"}); err != nil {
		t.Fatal(err)
	}
	got := x.Scan("the gadget talks to\nPORTAL today")
	want := []index.Match{{Term: "gadget", Line: 1, Column: 5}, {Term: "Portal", Line: 2, Column: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches %+v, want %+v", got, want)
	}
}

func TestNormalizeOrigin(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/portal.git":    "github.com/owner/portal",
		"https://GitHub.com/owner/portal/":       "github.com/owner/portal",
		"http://github.com/owner/portal":         "github.com/owner/portal",
		"ssh://git@github.com:22/owner/portal":   "github.com/owner/portal",
		"git@github.com:owner/portal.git":        "github.com/owner/portal",
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
