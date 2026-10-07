// Package lifecycle reads, validates, and writes a repository's
// lifecycle-and-license record
// (.strictmetadata/lifecycle-and-license/lifecycle-and-license.toml) and
// answers the questions the release, documentation, and commit tools ask of it.
//
// The record holds dated periods of each subject's lifecycle status, license,
// and identities, the registry names the repository holds, and the tags that
// release no version. A repository is confidential when one of its subjects
// has a proprietary license period in effect, and public otherwise; a
// repository without a record is public.
//
// The rules the record is evaluated against are a closed set fixed in code
// (see Rule). The package performs no file write of its own: every write goes
// through a FileWriter the caller injects. It never reads the clock: every
// question that depends on the date takes it.
package lifecycle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tomledit "github.com/stricttools/go-toml-edit"

	"github.com/stricttools/strictspec/go/strictspec"
)

const (
	// RecordDir is the record's directory, relative to the repository root.
	RecordDir = ".strictmetadata/lifecycle-and-license"
	// RecordFile is the record, relative to the repository root.
	RecordFile = RecordDir + "/lifecycle-and-license.toml"
	// ManifestFile is the directory's ownership manifest, relative to the
	// repository root.
	ManifestFile = RecordDir + "/manifest.toml"
	// FormatVersion is the record format this package reads and writes.
	FormatVersion = 1
	// ProprietaryLicense is the license value that makes a subject proprietary.
	ProprietaryLicense = "proprietary"
)

// manifestContent is the ownership manifest the record's directory carries.
const manifestContent = "owner = \"strictspec\"\n"

// FileWriter performs the file writes the package needs. Callers back it with
// their own effects handle, so a dry run records the writes instead of making
// them. Modes are the writer's choice.
type FileWriter interface {
	// WriteFile replaces the file at path with data.
	WriteFile(path string, data []byte) error
	// MkdirAll creates the directory at path and its parents; an existing
	// directory is not an error.
	MkdirAll(path string) error
}

// Status is a lifecycle status.
type Status string

const (
	StatusActive  Status = "active"
	StatusOnHold  Status = "on-hold"
	StatusRetired Status = "retired"
)

// Table names one of the record's period tables.
type Table string

const (
	TableLifecycle  Table = "lifecycle"
	TableLicenses   Table = "licenses"
	TableIdentities Table = "identities"
)

// Facet names which identity of a subject an identity entry records.
type Facet string

const (
	FacetReleasableName Facet = "releasable-name"
	FacetPackageName    Facet = "package-name"
	FacetProjectName    Facet = "project-name"
	FacetGoModulePath   Facet = "go-module-path"
	FacetTagFormat      Facet = "tag-format"
	FacetRepositoryURL  Facet = "repository-url"
)

// Period is a run of days. From is its first day; Until is the first day after
// it, and is the zero time while the period is open. Both are dates at
// midnight UTC.
type Period struct {
	From  time.Time
	Until time.Time
}

// Open reports whether the period has no end.
func (p Period) Open() bool { return p.Until.IsZero() }

// Contains reports whether the period covers the date of on.
func (p Period) Contains(on time.Time) bool {
	d := dateOf(on)
	if d.Before(p.From) {
		return false
	}
	return p.Open() || d.Before(p.Until)
}

// String renders the period for messages.
func (p Period) String() string {
	if p.Open() {
		return "from " + formatDate(p.From) + ", open"
	}
	return "from " + formatDate(p.From) + " until " + formatDate(p.Until)
}

// equal reports whether two periods have the same bounds.
func (p Period) equal(q Period) bool {
	return p.From.Equal(q.From) && p.Until.Equal(q.Until)
}

// overlaps reports whether two periods share a day.
func (p Period) overlaps(q Period) bool {
	pEndsBeforeQ := !p.Open() && !p.Until.After(q.From)
	qEndsBeforeP := !q.Open() && !q.Until.After(p.From)
	return !pEndsBeforeQ && !qEndsBeforeP
}

// LifecyclePeriod is one [[lifecycle]] entry.
type LifecyclePeriod struct {
	Subject string
	Status  Status
	Period
	Reason string
}

// LicensePeriod is one [[licenses]] entry.
type LicensePeriod struct {
	Subject string
	License string
	Period
	Reason string
}

// Proprietary reports whether the period's license is proprietary.
func (l LicensePeriod) Proprietary() bool { return l.License == ProprietaryLicense }

// Identity is one [[identities]] entry. A pending entry carries
// EffectiveVersion and a zero Period; a dated entry carries From and no
// EffectiveVersion.
type Identity struct {
	Subject     string
	Facet       Facet
	Value       string
	Registry    string
	TagPatterns []string
	Period
	EffectiveVersion string
	Reason           string
}

// Pending reports whether the identity waits for the release of its
// EffectiveVersion.
func (i Identity) Pending() bool { return i.EffectiveVersion != "" }

// RegistryName is one [[registry_names]] entry.
type RegistryName struct {
	Registry      string
	Name          string
	Subject       string
	RecordedSince time.Time
}

// UnversionedTag is one [[unversioned_tags]] entry.
type UnversionedTag struct {
	Tag      string
	Reason   string
	Recorded time.Time
}

// Record is a repository's lifecycle-and-license record. Its entries are read
// through the accessor methods and changed only through the mutators, which
// keep the parsed document (and its comments) in step for Write.
type Record struct {
	codenames        []string
	distinctiveTerms []string
	lifecycle        []LifecyclePeriod
	licenses         []LicensePeriod
	identities       []Identity
	registryNames    []RegistryName
	unversionedTags  []UnversionedTag

	present bool
	doc     *tomledit.Document
}

// Present reports whether the record was read from a file (or has been
// written). A repository without a record is public.
func (r *Record) Present() bool { return r.present }

// Codenames returns the record's codenames.
func (r *Record) Codenames() []string { return append([]string(nil), r.codenames...) }

// DistinctiveTerms returns the record's distinctive terms.
func (r *Record) DistinctiveTerms() []string {
	return append([]string(nil), r.distinctiveTerms...)
}

// Lifecycle returns the record's lifecycle periods in record order.
func (r *Record) Lifecycle() []LifecyclePeriod {
	return append([]LifecyclePeriod(nil), r.lifecycle...)
}

// Licenses returns the record's license periods in record order.
func (r *Record) Licenses() []LicensePeriod {
	return append([]LicensePeriod(nil), r.licenses...)
}

// Identities returns the record's identities in record order.
func (r *Record) Identities() []Identity {
	out := make([]Identity, len(r.identities))
	for i, id := range r.identities {
		id.TagPatterns = append([]string(nil), id.TagPatterns...)
		out[i] = id
	}
	return out
}

// RegistryNames returns the record's registry names in record order.
func (r *Record) RegistryNames() []RegistryName {
	return append([]RegistryName(nil), r.registryNames...)
}

// UnversionedTags returns the record's unversioned tags in record order.
func (r *Record) UnversionedTags() []UnversionedTag {
	return append([]UnversionedTag(nil), r.unversionedTags...)
}

// Load reads the record of the repository at repoRoot. A missing file yields
// an empty record, which is public and whose Present is false. A record that
// fails the built-in schema or the structural checks (see Validate) is an
// error naming every problem.
func Load(repoRoot string) (*Record, error) {
	path := filepath.Join(repoRoot, filepath.FromSlash(RecordFile))
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Record{}, nil
		}
		return nil, err
	}
	r, err := Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", RecordFile, err)
	}
	return r, nil
}

// Parse reads a record from its bytes: the built-in schema's checks, then the
// structural checks. The result is Present.
func Parse(src []byte) (*Record, error) {
	res := strictspec.LifecycleAndLicenseProgram().Validate(src, "toml")
	if !res.Valid {
		lines := make([]string, 0, len(res.Diagnostics))
		for _, d := range res.Diagnostics {
			lines = append(lines, fmt.Sprintf("%s at %s: %s", d.Code, d.Path, d.Message))
		}
		return nil, &ValidationError{Problems: lines}
	}
	v, err := strictspec.LoadValue(src, "toml")
	if err != nil { // unreachable: Validate parsed the same bytes
		return nil, err
	}
	doc, err := tomledit.Parse(src)
	if err != nil { // unreachable: Validate parsed the same bytes
		return nil, err
	}
	r := &Record{present: true, doc: doc}
	if err := r.bind(v); err != nil {
		return nil, err
	}
	if problems := r.structuralProblems(); len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	return r, nil
}

func (r *Record) bind(v strictspec.Value) error {
	r.codenames = stringList(v, "codenames")
	r.distinctiveTerms = stringList(v, "distinctive_terms")
	items, _ := v.Field("lifecycle")
	for _, it := range items.Items() {
		p, err := period(it)
		if err != nil {
			return err
		}
		r.lifecycle = append(r.lifecycle, LifecyclePeriod{
			Subject: str(it, "subject"),
			Status:  Status(str(it, "status")),
			Period:  p,
			Reason:  str(it, "reason"),
		})
	}
	items, _ = v.Field("licenses")
	for _, it := range items.Items() {
		p, err := period(it)
		if err != nil {
			return err
		}
		r.licenses = append(r.licenses, LicensePeriod{
			Subject: str(it, "subject"),
			License: str(it, "license"),
			Period:  p,
			Reason:  str(it, "reason"),
		})
	}
	items, _ = v.Field("identities")
	for _, it := range items.Items() {
		p, err := period(it)
		if err != nil {
			return err
		}
		r.identities = append(r.identities, Identity{
			Subject:          str(it, "subject"),
			Facet:            Facet(str(it, "facet")),
			Value:            str(it, "value"),
			Registry:         str(it, "registry"),
			TagPatterns:      stringList(it, "tag_patterns"),
			Period:           p,
			EffectiveVersion: str(it, "effective_version"),
			Reason:           str(it, "reason"),
		})
	}
	items, _ = v.Field("registry_names")
	for _, it := range items.Items() {
		since, _, err := date(it, "recorded_since")
		if err != nil {
			return err
		}
		r.registryNames = append(r.registryNames, RegistryName{
			Registry:      str(it, "registry"),
			Name:          str(it, "name"),
			Subject:       str(it, "subject"),
			RecordedSince: since,
		})
	}
	items, _ = v.Field("unversioned_tags")
	for _, it := range items.Items() {
		recorded, _, err := date(it, "recorded")
		if err != nil {
			return err
		}
		r.unversionedTags = append(r.unversionedTags, UnversionedTag{
			Tag:      str(it, "tag"),
			Reason:   str(it, "reason"),
			Recorded: recorded,
		})
	}
	return nil
}

func str(v strictspec.Value, key string) string {
	f, ok := v.Field(key)
	if !ok {
		return ""
	}
	s, _ := f.AsString()
	return s
}

// stringList reads an optional array of strings; an absent array is empty.
func stringList(v strictspec.Value, key string) []string {
	f, ok := v.Field(key)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range f.Items() {
		s, _ := it.AsString()
		out = append(out, s)
	}
	return out
}

func date(v strictspec.Value, key string) (time.Time, bool, error) {
	f, ok := v.Field(key)
	if !ok {
		return time.Time{}, false, nil
	}
	lexeme, _ := f.Datetime()
	t, err := time.Parse(time.DateOnly, lexeme)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s: %q is not a date: %w", key, lexeme, err)
	}
	return t, true, nil
}

func period(v strictspec.Value) (Period, error) {
	from, _, err := date(v, "from")
	if err != nil {
		return Period{}, err
	}
	until, _, err := date(v, "until")
	if err != nil {
		return Period{}, err
	}
	return Period{From: from, Until: until}, nil
}

// dateOf is the calendar date of t, in t's own location, at midnight UTC.
func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func formatDate(t time.Time) string { return t.Format(time.DateOnly) }

// ValidationError lists every problem a record has.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid lifecycle-and-license record:\n  - " + strings.Join(e.Problems, "\n  - ")
}
