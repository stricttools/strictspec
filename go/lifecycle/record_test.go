package lifecycle_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/lifecycle"
)

func TestLoadWithoutARecordIsEmptyAndPublic(t *testing.T) {
	r, err := lifecycle.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.Present() {
		t.Fatal("a repository without a record reports a present record")
	}
	if r.Confidential(day(t, "2026-10-07")) {
		t.Fatal("a repository without a record is confidential")
	}
	if err := r.Validate(day(t, "2026-10-07"), nil); err != nil {
		t.Fatalf("an empty record is invalid: %v", err)
	}
}

func TestLoadReadsEveryTable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(lifecycle.RecordDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := sharedRecord + `
[[identities]]
subject = "widget"
facet = "package-name"
value = "widget-client"
registry = "npm"
tag_patterns = ["widget@v*"]
effective_version = "0.5.0"
reason = "renamed"

[[unversioned_tags]]
tag = "nightly"
reason = "a moving build tag"
recorded = 2026-10-07
`
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(lifecycle.RecordFile)), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := lifecycle.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Present() {
		t.Fatal("a loaded record is not present")
	}
	lc := r.Lifecycle()
	if len(lc) != 4 || lc[1].Subject != "widget" || lc[1].Status != lifecycle.StatusActive ||
		!lc[1].From.Equal(day(t, "2026-01-01")) || !lc[1].Until.Equal(day(t, "2026-11-01")) ||
		lc[2].Status != lifecycle.StatusOnHold || !lc[2].Open() {
		t.Fatalf("lifecycle %+v", lc)
	}
	lic := r.Licenses()
	if len(lic) != 3 || !lic[1].Proprietary() || lic[0].Proprietary() || lic[1].Reason != "server logic" {
		t.Fatalf("licenses %+v", lic)
	}
	ids := r.Identities()
	last := ids[len(ids)-1]
	if !last.Pending() || last.EffectiveVersion != "0.5.0" || !last.From.IsZero() ||
		last.Registry != "npm" || !reflect.DeepEqual(last.TagPatterns, []string{"widget@v*"}) ||
		last.Facet != lifecycle.FacetPackageName {
		t.Fatalf("pending identity %+v", last)
	}
	if ids[0].Pending() || ids[0].Registry != "" {
		t.Fatalf("dated identity %+v", ids[0])
	}
	names := r.RegistryNames()
	if len(names) != 2 || names[0].Name != "portal-legacy" || !names[0].RecordedSince.Equal(day(t, "2026-02-01")) {
		t.Fatalf("registry names %+v", names)
	}
	tags := r.UnversionedTags()
	if len(tags) != 1 || tags[0].Tag != "nightly" || !tags[0].Recorded.Equal(day(t, "2026-10-07")) {
		t.Fatalf("unversioned tags %+v", tags)
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	r := parse(t, sharedRecord)
	ids := r.Identities()
	ids[0].TagPatterns[0] = "changed"
	ids[0].Value = "changed"
	if r.Identities()[0].Value != "portal" || r.Identities()[0].TagPatterns[0] != "portal@v*" {
		t.Fatal("changing an accessor's result changed the record")
	}
}

func TestLoadRefusesWhatTheSchemaRefuses(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(lifecycle.RecordDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "format_version = 1\ndisclosure = \"confidential\"\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(lifecycle.RecordFile)), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := lifecycle.Load(root)
	var ve *lifecycle.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a validation error, got %v", err)
	}
	if !strings.Contains(err.Error(), lifecycle.RecordFile) {
		t.Fatalf("the error does not name the file: %v", err)
	}
}

func TestStructuralRefusals(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"overlapping lifecycle periods", `format_version = 1
[[lifecycle]]
subject = "portal"
status = "active"
from = 2026-01-01
until = 2026-06-01
reason = "a"
[[lifecycle]]
subject = "portal"
status = "on-hold"
from = 2026-05-01
until = 2026-07-01
reason = "b"
`, "overlapping periods"},
		{"two open license periods", `format_version = 1
[[licenses]]
subject = "portal"
license = "MIT"
from = 2026-01-01
reason = "a"
[[licenses]]
subject = "portal"
license = "Apache-2.0"
from = 2026-05-01
reason = "b"
`, "open periods"},
		{"until before from", `format_version = 1
[[lifecycle]]
subject = "portal"
status = "active"
from = 2026-06-01
until = 2026-01-01
reason = "a"
`, "before its from"},
		{"identity with from and effective version", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "npm"
tag_patterns = []
from = 2026-01-01
effective_version = "0.5.0"
reason = "a"
`, "both from and effective_version"},
		{"identity with neither from nor effective version", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "npm"
tag_patterns = []
reason = "a"
`, "neither from nor effective_version"},
		{"pending identity with until", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "npm"
tag_patterns = []
until = 2026-01-01
effective_version = "0.5.0"
reason = "a"
`, "pending and carries until"},
		{"two pending identities of one facet", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "npm"
tag_patterns = []
effective_version = "0.5.0"
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "q"
registry = "npm"
tag_patterns = []
effective_version = "0.6.0"
reason = "b"
`, "pending entries"},
		{"package-name identity without a registry", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = ""
tag_patterns = []
effective_version = "0.5.0"
reason = "a"
`, "names no registry"},
		{"two open package-name identities in one registry", `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "p"
registry = "pypi"
tag_patterns = []
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "q"
registry = "pypi"
tag_patterns = []
from = 2026-03-01
reason = "b"
`, "open periods"},
		{"bracket in a tag pattern", `format_version = 1
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["v[0-9]*"]
from = 2026-01-01
reason = "a"
`, "never brackets"},
		{"overlapping identities of one facet", `format_version = 1
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = []
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "gateway"
registry = ""
tag_patterns = []
from = 2026-03-01
reason = "b"
`, "open periods"},
		{"repeated registry name", `format_version = 1
[[registry_names]]
registry = "npm"
name = "portal"
subject = "portal"
recorded_since = 2026-01-01
[[registry_names]]
registry = "npm"
name = "portal"
subject = "widget"
recorded_since = 2026-02-01
`, "more than once"},
		{"repeated unversioned tag", `format_version = 1
[[unversioned_tags]]
tag = "nightly"
reason = "a"
recorded = 2026-01-01
[[unversioned_tags]]
tag = "nightly"
reason = "b"
recorded = 2026-02-01
`, "more than once"},
	}
	for _, c := range cases {
		_, err := lifecycle.Parse([]byte(c.src))
		var ve *lifecycle.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want a validation error, got %v", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error does not say %q: %v", c.name, c.want, err)
		}
	}
}

// A package name is a name in one registry: a subject publishing to npm and
// PyPI holds one package-name identity in each, open and pending alike.
func TestPackageNameIdentitiesAreKeyedByRegistry(t *testing.T) {
	parse(t, `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal"
registry = "npm"
tag_patterns = ["v*"]
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal"
registry = "pypi"
tag_patterns = ["v*"]
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "npm"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "pypi"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"
`)
}

func TestAdjacentPeriodsAndOneOfEachFacetAreAccepted(t *testing.T) {
	parse(t, `format_version = 1
[[lifecycle]]
subject = "portal"
status = "active"
from = 2026-01-01
until = 2026-06-01
reason = "a"
[[lifecycle]]
subject = "portal"
status = "on-hold"
from = 2026-06-01
reason = "b"
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["v*"]
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal"
registry = "npm"
tag_patterns = ["v*"]
from = 2026-01-01
reason = "a"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "npm"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"
`)
}
