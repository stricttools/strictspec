package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tomledit "github.com/stricttools/go-toml-edit"
)

// minimalRecord is the document a record without a file starts from.
const minimalRecord = "format_version = 1\n"

// source is the record's current document bytes.
func (r *Record) source() []byte {
	if r.doc == nil {
		return []byte(minimalRecord)
	}
	return r.doc.Bytes()
}

// edit applies f to a copy of the record's document and adopts the result only
// when the edited document passes the built-in schema and the structural
// checks, so a refused mutation leaves the record as it was. Comments in the
// document are kept.
func (r *Record) edit(f func(d *tomledit.Document) error) error {
	d, err := tomledit.Parse(r.source())
	if err != nil { // unreachable: the record's own bytes always parse
		return err
	}
	if err := f(d); err != nil {
		return err
	}
	next, err := Parse(d.Bytes())
	if err != nil {
		return err
	}
	present := r.present
	*r = *next
	r.present = present
	return nil
}

func localDate(t time.Time) tomledit.LocalDate {
	d := dateOf(t)
	return tomledit.LocalDate{Year: d.Year(), Month: int(d.Month()), Day: d.Day()}
}

// set runs a sequence of Set calls, stopping at the first error.
func set(d *tomledit.Document, pairs ...any) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		path := pairs[i].(string)
		if err := d.Set(path, pairs[i+1]); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

// OpenPeriod appends a new open period to the lifecycle or licenses table:
// value is the status for TableLifecycle and the license for TableLicenses.
// Opening a period while the subject's previous one is still open is refused;
// close it first with ClosePeriod.
func (r *Record) OpenPeriod(table Table, subject, value string, from time.Time, reason string) error {
	var valueKey string
	switch table {
	case TableLifecycle:
		valueKey = "status"
	case TableLicenses:
		valueKey = "license"
	case TableIdentities:
		return fmt.Errorf("OpenPeriod does not open identities; use AddIdentity or AddPendingIdentity")
	default:
		return fmt.Errorf("unknown table %q; the period tables are lifecycle and licenses", table)
	}
	if from.IsZero() {
		return fmt.Errorf("opening a %s period for %q needs its from date", table, subject)
	}
	return r.edit(func(d *tomledit.Document) error {
		if err := d.NewArrayTable(string(table)); err != nil {
			return fmt.Errorf("adding a [[%s]] entry: %w", table, err)
		}
		p := string(table) + "[-1]."
		return set(d,
			p+"subject", subject,
			p+valueKey, value,
			p+"from", localDate(from),
			p+"reason", reason,
		)
	})
}

// ClosePeriod closes the subject's open period in the lifecycle or licenses
// table, setting until (the first day after the period). A subject without an
// open period in the table is refused.
func (r *Record) ClosePeriod(table Table, subject string, until time.Time) error {
	var periods []Period
	switch table {
	case TableLifecycle:
		for _, l := range r.lifecycle {
			periods = append(periods, l.Period)
		}
	case TableLicenses:
		for _, l := range r.licenses {
			periods = append(periods, l.Period)
		}
	case TableIdentities:
		return fmt.Errorf("ClosePeriod does not close identities; use CloseIdentity")
	default:
		return fmt.Errorf("unknown table %q; the period tables are lifecycle and licenses", table)
	}
	index := -1
	for i, p := range periods {
		if r.subjectAt(table, i) == subject && p.Open() {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("%s: subject %q has no open period to close", table, subject)
	}
	return r.closeAt(table, index, until)
}

func (r *Record) subjectAt(table Table, i int) string {
	switch table {
	case TableLifecycle:
		return r.lifecycle[i].Subject
	case TableLicenses:
		return r.licenses[i].Subject
	default:
		return r.identities[i].Subject
	}
}

func (r *Record) closeAt(table Table, index int, until time.Time) error {
	if until.IsZero() {
		return fmt.Errorf("closing a %s period needs its until date", table)
	}
	return r.edit(func(d *tomledit.Document) error {
		return set(d, fmt.Sprintf("%s[%d].until", table, index), localDate(until))
	})
}

// AddIdentity appends a dated identity (From set, no EffectiveVersion). An
// identity whose subject and facet (and registry, for a registry-scoped
// facet) already have an open identity is refused; close that one first with
// CloseIdentity.
func (r *Record) AddIdentity(id Identity) error {
	if id.Pending() {
		return fmt.Errorf("identity %q carries an effective version; use AddPendingIdentity", id.Value)
	}
	if id.From.IsZero() {
		return fmt.Errorf("identity %q needs its from date", id.Value)
	}
	return r.appendIdentity(id)
}

// AddPendingIdentity appends a pending identity: EffectiveVersion set, with no
// From and no Until. ActivatePendingIdentities converts it when that version
// is released.
func (r *Record) AddPendingIdentity(id Identity) error {
	if !id.Pending() {
		return fmt.Errorf("pending identity %q needs the version whose release begins it", id.Value)
	}
	if !id.From.IsZero() || !id.Until.IsZero() {
		return fmt.Errorf("pending identity %q carries a date; a pending entry carries only its effective version", id.Value)
	}
	return r.appendIdentity(id)
}

func (r *Record) appendIdentity(id Identity) error {
	patterns := id.TagPatterns
	if patterns == nil {
		patterns = []string{}
	}
	return r.edit(func(d *tomledit.Document) error {
		if err := d.NewArrayTable(string(TableIdentities)); err != nil {
			return fmt.Errorf("adding an [[identities]] entry: %w", err)
		}
		p := "identities[-1]."
		if err := set(d,
			p+"subject", id.Subject,
			p+"facet", string(id.Facet),
			p+"value", id.Value,
			p+"registry", id.Registry,
			p+"tag_patterns", patterns,
		); err != nil {
			return err
		}
		if id.Pending() {
			if err := set(d, p+"effective_version", id.EffectiveVersion); err != nil {
				return err
			}
		} else {
			if err := set(d, p+"from", localDate(id.From)); err != nil {
				return err
			}
			if !id.Until.IsZero() {
				if err := set(d, p+"until", localDate(id.Until)); err != nil {
					return err
				}
			}
		}
		return set(d, p+"reason", id.Reason)
	})
}

// CloseIdentity closes the open dated identity of the subject and facet (in
// the registry, for a registry-scoped facet; the registry is not read
// otherwise), setting until. A subject and facet without an open identity is
// refused.
func (r *Record) CloseIdentity(subject string, facet Facet, registry string, until time.Time) error {
	index := r.openIdentity(subject, facet, registry)
	if index < 0 {
		if facet.RegistryScoped() {
			return fmt.Errorf("identities: subject %q facet %q registry %q has no open identity to close", subject, facet, registry)
		}
		return fmt.Errorf("identities: subject %q facet %q has no open identity to close", subject, facet)
	}
	return r.closeAt(TableIdentities, index, until)
}

func (r *Record) openIdentity(subject string, facet Facet, registry string) int {
	for i, id := range r.identities {
		if id.Fills(subject, facet, registry) && !id.Pending() && id.Open() {
			return i
		}
	}
	return -1
}

// ActivatePendingIdentities converts every pending identity whose
// EffectiveVersion is version, as the release of that version does at its
// archive step: the open identity of the same subject and facet (and
// registry, for a registry-scoped facet) is closed with
// until set to the date of on (the release commit's committer date), and the
// pending entry gets from set to that date and loses its effective version. It
// returns the converted identities; none pending for the version is not an
// error.
func (r *Record) ActivatePendingIdentities(version string, on time.Time) ([]Identity, error) {
	if version == "" {
		return nil, fmt.Errorf("activating pending identities needs the released version")
	}
	var pending []int
	for i, id := range r.identities {
		if id.Pending() && id.EffectiveVersion == version {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return nil, nil
	}
	day := localDate(on)
	err := r.edit(func(d *tomledit.Document) error {
		for _, i := range pending {
			id := r.identities[i]
			if open := r.openIdentity(id.Subject, id.Facet, id.Registry); open >= 0 {
				if err := set(d, fmt.Sprintf("identities[%d].until", open), day); err != nil {
					return err
				}
			}
			path := fmt.Sprintf("identities[%d].effective_version", i)
			if err := d.Delete(path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			if err := set(d, fmt.Sprintf("identities[%d].from", i), day); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]Identity, 0, len(pending))
	for _, i := range pending {
		id := r.identities[i]
		id.TagPatterns = append([]string(nil), id.TagPatterns...)
		out = append(out, id)
	}
	return out, nil
}

// AddRegistryName records a registry name held for subject. A name the
// record already holds in that registry is refused.
func (r *Record) AddRegistryName(registry, name, subject string, since time.Time) error {
	if since.IsZero() {
		return fmt.Errorf("recording the %s name %q needs its recorded_since date", registry, name)
	}
	return r.edit(func(d *tomledit.Document) error {
		if err := d.NewArrayTable("registry_names"); err != nil {
			return fmt.Errorf("adding a [[registry_names]] entry: %w", err)
		}
		p := "registry_names[-1]."
		return set(d,
			p+"registry", registry,
			p+"name", name,
			p+"subject", subject,
			p+"recorded_since", localDate(since),
		)
	})
}

// AddUnversionedTag records a tag that releases no version. A tag the record
// already holds is refused.
func (r *Record) AddUnversionedTag(tag, reason string, recorded time.Time) error {
	if recorded.IsZero() {
		return fmt.Errorf("recording the unversioned tag %q needs its recorded date", tag)
	}
	return r.edit(func(d *tomledit.Document) error {
		if err := d.NewArrayTable("unversioned_tags"); err != nil {
			return fmt.Errorf("adding an [[unversioned_tags]] entry: %w", err)
		}
		p := "unversioned_tags[-1]."
		return set(d,
			p+"tag", tag,
			p+"reason", reason,
			p+"recorded", localDate(recorded),
		)
	})
}

// ClearConfidentialTerms removes the codenames and distinctive terms, as a
// declassification does.
func (r *Record) ClearConfidentialTerms() error {
	return r.edit(func(d *tomledit.Document) error {
		if err := d.Delete("codenames"); err != nil {
			return err
		}
		return d.Delete("distinctive_terms")
	})
}

// Write writes the record to repoRoot's
// .strictmetadata/lifecycle-and-license/lifecycle-and-license.toml through w,
// creating the directory and its manifest naming strictspec when they are
// missing. The record is checked again before anything is written; a
// manifest naming another owner is refused. Every write goes through w.
func (r *Record) Write(w FileWriter, repoRoot string) error {
	src := r.source()
	if _, err := Parse(src); err != nil {
		return err
	}
	dir := filepath.Join(repoRoot, filepath.FromSlash(RecordDir))
	manifest := filepath.Join(repoRoot, filepath.FromSlash(ManifestFile))
	writeManifest, err := manifestNeeded(manifest)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(dir); err != nil {
		return err
	}
	if writeManifest {
		if err := w.WriteFile(manifest, []byte(manifestContent)); err != nil {
			return err
		}
	}
	if err := w.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(RecordFile)), src); err != nil {
		return err
	}
	r.present = true
	return nil
}

// manifestNeeded reports whether the directory's manifest is missing, and
// refuses one that names an owner other than strictspec.
func manifestNeeded(path string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	d, err := tomledit.Parse(src)
	if err != nil {
		return false, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	owner, err := d.GetString("owner")
	if err != nil || owner != "strictspec" {
		return false, fmt.Errorf("%s must hold owner = \"strictspec\" (strictspec owns the directory); fix the manifest by hand", ManifestFile)
	}
	return false, nil
}
