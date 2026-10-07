package lifecycle_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stricttools/strictspec/go/lifecycle"
)

// day parses a YYYY-MM-DD date for a test.
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

// recordingWriter records every write and performs none.
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

// diskWriter performs the writes, for round-trip tests.
type diskWriter struct{}

func (diskWriter) WriteFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
func (diskWriter) MkdirAll(path string) error               { return os.MkdirAll(path, 0o755) }

// filesUnder lists every path under root, root excluded.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if p != root {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// refusal asserts err is a *lifecycle.Refusal for rule and returns it.
func refusal(t *testing.T, err error, rule lifecycle.Rule) *lifecycle.Refusal {
	t.Helper()
	var r *lifecycle.Refusal
	if !errors.As(err, &r) {
		t.Fatalf("want a %s refusal, got %v", rule, err)
	}
	if r.Rule != rule {
		t.Fatalf("want rule %s, got %s (%v)", rule, r.Rule, err)
	}
	if r.Fix == "" {
		t.Fatalf("refusal names no fix: %v", err)
	}
	if !strings.Contains(err.Error(), string(rule)) {
		t.Fatalf("refusal text does not name its rule: %v", err)
	}
	return r
}

// The record most rule tests share: portal is proprietary from 2026-10-07,
// widget is MIT throughout, gadget is retired.
const sharedRecord = `format_version = 1
codenames = ["Bluebird"]
distinctive_terms = ["hyperlattice"]

[[lifecycle]]
subject = "portal"
status = "active"
from = 2026-01-01
reason = "first release"

[[lifecycle]]
subject = "widget"
status = "active"
from = 2026-01-01
until = 2026-11-01
reason = "first release"

[[lifecycle]]
subject = "widget"
status = "on-hold"
from = 2026-11-01
reason = "paused"

[[lifecycle]]
subject = "gadget"
status = "retired"
from = 2026-03-01
reason = "replaced by widget"

[[licenses]]
subject = "portal"
license = "MIT"
from = 2026-01-01
until = 2026-10-07
reason = "initial license"

[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-10-07
reason = "server logic"

[[licenses]]
subject = "widget"
license = "MIT"
from = 2026-01-01
reason = "initial license"

[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["portal@v*"]
from = 2026-01-01
reason = "first name"

[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-server"
registry = "npm"
tag_patterns = ["portal@v*"]
from = 2026-01-01
reason = "first name"

[[identities]]
subject = "portal"
facet = "tag-format"
value = "portal@v{version}"
registry = ""
tag_patterns = ["portal@v*"]
from = 2026-01-01
reason = "first format"

[[identities]]
subject = "widget"
facet = "releasable-name"
value = "widget"
registry = ""
tag_patterns = ["widget@v*"]
from = 2026-01-01
reason = "first name"

[[registry_names]]
registry = "npm"
name = "portal-legacy"
subject = "portal"
recorded_since = 2026-02-01

[[registry_names]]
registry = "npm"
name = "widget"
subject = "widget"
recorded_since = 2026-02-01
`
