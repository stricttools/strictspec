package lifecycle_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/lifecycle"
)

const commentedRecord = `# The lifecycle-and-license record.
format_version = 1

# portal has been active since its first release.
[[lifecycle]]
subject = "portal"
status = "active" # never paused
from = 2026-01-01
reason = "first release"

[[licenses]]
subject = "portal"
license = "MIT"
from = 2026-01-01
reason = "initial license"

[[identities]]
subject = "portal"
facet = "package-name"
value = "portal"
registry = "npm"
tag_patterns = ["v*"]
from = 2026-01-01
reason = "first name"
`

func bytesOf(t *testing.T, r *lifecycle.Record) string {
	t.Helper()
	w := newRecordingWriter()
	root := t.TempDir()
	if err := r.Write(w, root); err != nil {
		t.Fatal(err)
	}
	return string(w.files[filepath.Join(root, filepath.FromSlash(lifecycle.RecordFile))])
}

func TestClosingAndOpeningAPeriodKeepsComments(t *testing.T) {
	r := parse(t, commentedRecord)
	if err := r.ClosePeriod(lifecycle.TableLifecycle, "portal", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPeriod(lifecycle.TableLifecycle, "portal", "on-hold", day(t, "2026-10-07"), "paused for a redesign"); err != nil {
		t.Fatal(err)
	}
	out := bytesOf(t, r)
	for _, want := range []string{"# The lifecycle-and-license record.", "# portal has been active", "# never paused", "until = 2026-10-07", `status = "on-hold"`, `reason = "paused for a redesign"`} {
		if !strings.Contains(out, want) {
			t.Errorf("written record lacks %q:\n%s", want, out)
		}
	}
	lc := r.Lifecycle()
	if len(lc) != 2 || lc[0].Open() || !lc[1].Open() || lc[1].Status != lifecycle.StatusOnHold {
		t.Fatalf("lifecycle %+v", lc)
	}
	again := parse(t, out)
	if len(again.Lifecycle()) != 2 {
		t.Fatal("the written record does not read back")
	}
}

func TestOpeningAPeriodWhileOneIsOpenIsRefusedAndChangesNothing(t *testing.T) {
	r := parse(t, commentedRecord)
	before := bytesOf(t, r)
	err := r.OpenPeriod(lifecycle.TableLicenses, "portal", "Apache-2.0", day(t, "2026-10-07"), "relicensed")
	if err == nil || !strings.Contains(err.Error(), "open periods") {
		t.Fatalf("want a refusal of two open periods, got %v", err)
	}
	if after := bytesOf(t, r); after != before {
		t.Fatalf("a refused mutation changed the record:\n%s", after)
	}
	// The fix: close the open period first.
	if err := r.ClosePeriod(lifecycle.TableLicenses, "portal", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPeriod(lifecycle.TableLicenses, "portal", "Apache-2.0", day(t, "2026-10-07"), "relicensed"); err != nil {
		t.Fatalf("closing the open period did not clear the refusal: %v", err)
	}
}

func TestMutatorRefusals(t *testing.T) {
	r := parse(t, commentedRecord)
	if err := r.ClosePeriod(lifecycle.TableLicenses, "widget", day(t, "2026-10-07")); err == nil {
		t.Error("closing a period a subject does not have was accepted")
	}
	if err := r.ClosePeriod(lifecycle.TableLifecycle, "portal", day(t, "2025-01-01")); err == nil {
		t.Error("closing a period before it began was accepted")
	}
	if err := r.OpenPeriod(lifecycle.TableLifecycle, "widget", "paused", day(t, "2026-10-07"), "x"); err == nil {
		t.Error("an unknown status was accepted")
	}
	if err := r.OpenPeriod(lifecycle.TableLicenses, "widget", "MIT", day(t, "2026-10-07"), ""); err == nil {
		t.Error("an empty reason was accepted")
	}
	if err := r.OpenPeriod(lifecycle.TableIdentities, "widget", "x", day(t, "2026-10-07"), "x"); err == nil {
		t.Error("OpenPeriod opened an identity")
	}
	if err := r.AddRegistryName("npm", "portal", "portal", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddRegistryName("npm", "portal", "widget", day(t, "2026-10-08")); err == nil {
		t.Error("a registry name recorded twice was accepted")
	}
	if err := r.AddUnversionedTag("nightly", "a moving build tag", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddUnversionedTag("nightly", "again", day(t, "2026-10-08")); err == nil {
		t.Error("an unversioned tag recorded twice was accepted")
	}
	if len(r.RegistryNames()) != 1 || len(r.UnversionedTags()) != 1 {
		t.Fatalf("refused additions changed the record: %+v %+v", r.RegistryNames(), r.UnversionedTags())
	}
}

func TestRenamingClosesTheOldIdentityAndOpensTheNew(t *testing.T) {
	r := parse(t, commentedRecord)
	if err := r.CloseIdentity("portal", lifecycle.FacetPackageName, "npm", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddIdentity(lifecycle.Identity{
		Subject: "portal", Facet: lifecycle.FacetPackageName, Value: "portal-client",
		Registry: "npm", TagPatterns: []string{"v*"},
		Period: lifecycle.Period{From: day(t, "2026-10-07")}, Reason: "renamed",
	}); err != nil {
		t.Fatal(err)
	}
	ids := r.Identities()
	if len(ids) != 2 || ids[0].Open() || !ids[1].Open() || ids[1].Value != "portal-client" {
		t.Fatalf("identities %+v", ids)
	}
	if err := r.CloseIdentity("portal", lifecycle.FacetProjectName, "", day(t, "2026-10-07")); err == nil {
		t.Error("closing an identity the subject does not have was accepted")
	}
	if err := r.AddIdentity(lifecycle.Identity{Subject: "portal", Facet: lifecycle.FacetPackageName, Value: "x", Registry: "npm", EffectiveVersion: "0.5.0", Reason: "x"}); err == nil {
		t.Error("AddIdentity accepted a pending identity")
	}
}

func TestPendingIdentitiesAndTheirConversion(t *testing.T) {
	r := parse(t, commentedRecord)
	pending := lifecycle.Identity{
		Subject: "portal", Facet: lifecycle.FacetPackageName, Value: "portal-client",
		Registry: "npm", TagPatterns: []string{"v*"}, EffectiveVersion: "0.5.0", Reason: "renamed",
	}
	if err := r.AddPendingIdentity(pending); err != nil {
		t.Fatal(err)
	}
	second := pending
	second.Value, second.EffectiveVersion = "portal-next", "0.6.0"
	if err := r.AddPendingIdentity(second); err == nil || !strings.Contains(err.Error(), "pending entries") {
		t.Fatalf("a second pending identity of one facet: %v", err)
	}
	dated := pending
	dated.Period = lifecycle.Period{From: day(t, "2026-10-07")}
	if err := r.AddPendingIdentity(dated); err == nil {
		t.Error("a pending identity carrying a date was accepted")
	}

	converted, err := r.ActivatePendingIdentities("0.4.0", day(t, "2026-10-07"))
	if err != nil || len(converted) != 0 {
		t.Fatalf("another version converted %+v (%v)", converted, err)
	}
	if !r.Identities()[1].Pending() {
		t.Fatal("releasing another version converted the pending identity")
	}

	converted, err = r.ActivatePendingIdentities("0.5.0", day(t, "2026-10-09"))
	if err != nil {
		t.Fatal(err)
	}
	if len(converted) != 1 || converted[0].Value != "portal-client" || converted[0].Pending() || !converted[0].From.Equal(day(t, "2026-10-09")) {
		t.Fatalf("converted %+v", converted)
	}
	ids := r.Identities()
	if !ids[0].Until.Equal(day(t, "2026-10-09")) {
		t.Fatalf("the replaced identity was not closed on the release date: %+v", ids[0])
	}
	if ids[1].Pending() || !ids[1].Open() {
		t.Fatalf("the converted identity %+v", ids[1])
	}
	out := bytesOf(t, r)
	if strings.Contains(out, "effective_version") {
		t.Fatalf("the written record keeps effective_version:\n%s", out)
	}
	if id, ok, err := r.TagOwner("v0.5.0", day(t, "2026-10-09")); err != nil || !ok || id.Value != "portal-client" {
		t.Fatalf("the converted identity does not own the release's tag: %+v %v %v", id, ok, err)
	}
}

func TestWriteGoesThroughTheWriterOnly(t *testing.T) {
	root := t.TempDir()
	r, err := lifecycle.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPeriod(lifecycle.TableLicenses, "portal", "MIT", day(t, "2026-10-07"), "initial"); err != nil {
		t.Fatal(err)
	}
	w := newRecordingWriter()
	if err := r.Write(w, root); err != nil {
		t.Fatal(err)
	}
	if got := filesUnder(t, root); len(got) != 0 {
		t.Fatalf("the library wrote files itself: %v", got)
	}
	dir := filepath.Join(root, filepath.FromSlash(lifecycle.RecordDir))
	if len(w.dirs) != 1 || w.dirs[0] != dir {
		t.Fatalf("directories %v", w.dirs)
	}
	manifest := w.files[filepath.Join(root, filepath.FromSlash(lifecycle.ManifestFile))]
	if string(manifest) != "owner = \"strictspec\"\n" {
		t.Fatalf("manifest %q", manifest)
	}
	record := string(w.files[filepath.Join(root, filepath.FromSlash(lifecycle.RecordFile))])
	if !strings.HasPrefix(record, "format_version = 1\n") || !strings.Contains(record, `license = "MIT"`) {
		t.Fatalf("record %q", record)
	}
	if len(w.files) != 2 {
		t.Fatalf("writes %v", w.files)
	}
	if !r.Present() {
		t.Fatal("a written record is not present")
	}
}

func TestTheMinimalRecordWritesOnlyTheFormatVersion(t *testing.T) {
	root := t.TempDir()
	r, err := lifecycle.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write(diskWriter{}, root); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(lifecycle.RecordFile)))
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != "format_version = 1\n" {
		t.Fatalf("minimal record %q", src)
	}
	again, err := lifecycle.Load(root)
	if err != nil || !again.Present() {
		t.Fatalf("the minimal record does not read back: %v", err)
	}
}

func TestWriteKeepsAStrictspecManifestAndRefusesAnotherOwner(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(lifecycle.RecordDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, filepath.FromSlash(lifecycle.ManifestFile))
	if err := os.WriteFile(manifest, []byte("owner = \"strictspec\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := parse(t, commentedRecord)
	w := newRecordingWriter()
	if err := r.Write(w, root); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.files[manifest]; ok {
		t.Fatal("an existing manifest was rewritten")
	}
	if err := os.WriteFile(manifest, []byte("owner = \"selfdoc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := r.Write(newRecordingWriter(), root)
	if err == nil || !strings.Contains(err.Error(), "owner = \"strictspec\"") {
		t.Fatalf("want a refusal naming the owner, got %v", err)
	}
	// The fix the refusal names: correct the manifest.
	if err := os.WriteFile(manifest, []byte("owner = \"strictspec\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Write(newRecordingWriter(), root); err != nil {
		t.Fatalf("correcting the manifest did not clear the refusal: %v", err)
	}
}

// The release of a version converts the pending identity of each registry,
// closing the open identity of that registry only.
func TestPendingIdentitiesOfTwoRegistriesConvertSeparately(t *testing.T) {
	r := parse(t, `format_version = 1
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
`)
	for _, registry := range []string{"npm", "pypi"} {
		if err := r.AddPendingIdentity(lifecycle.Identity{
			Subject: "portal", Facet: lifecycle.FacetPackageName, Value: "portal-client",
			Registry: registry, TagPatterns: []string{"v*"}, EffectiveVersion: "0.5.0", Reason: "renamed",
		}); err != nil {
			t.Fatalf("pending %s identity: %v", registry, err)
		}
	}
	if err := r.AddPendingIdentity(lifecycle.Identity{
		Subject: "portal", Facet: lifecycle.FacetPackageName, Value: "portal-client",
		Registry: "", TagPatterns: []string{"v*"}, EffectiveVersion: "0.5.0", Reason: "renamed",
	}); err == nil || !strings.Contains(err.Error(), "names no registry") {
		t.Fatalf("a pending package-name identity without a registry: %v", err)
	}
	converted, err := r.ActivatePendingIdentities("0.5.0", day(t, "2026-10-09"))
	if err != nil {
		t.Fatal(err)
	}
	if len(converted) != 2 {
		t.Fatalf("converted %+v", converted)
	}
	for _, id := range r.Identities() {
		switch {
		case id.Value == "portal" && (id.Open() || !id.Until.Equal(day(t, "2026-10-09"))):
			t.Errorf("the replaced %s identity was not closed on the release date: %+v", id.Registry, id)
		case id.Value == "portal-client" && (id.Pending() || !id.Open()):
			t.Errorf("the converted %s identity %+v", id.Registry, id)
		}
	}
	if err := r.CloseIdentity("portal", lifecycle.FacetPackageName, "pypi", day(t, "2026-10-10")); err != nil {
		t.Fatal(err)
	}
	for _, id := range r.Identities() {
		if id.Value == "portal-client" && id.Registry == "npm" && !id.Open() {
			t.Errorf("closing the pypi identity closed the npm one: %+v", id)
		}
	}
}
