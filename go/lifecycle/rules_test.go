package lifecycle_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/lifecycle"
)

func TestEveryRuleHasAClassAndADistinctName(t *testing.T) {
	seen := map[lifecycle.Rule]bool{}
	for _, rule := range lifecycle.Rules() {
		if rule.Class() == "" {
			t.Errorf("rule %s has no class", rule)
		}
		if seen[rule] {
			t.Errorf("rule %s listed twice", rule)
		}
		seen[rule] = true
	}
	want := map[lifecycle.Rule]lifecycle.RuleClass{
		lifecycle.RuleProprietaryRequiresPrivate:     lifecycle.WhileValueHolds,
		lifecycle.RuleProprietaryRefusesPublicOutput: lifecycle.WhileValueHolds,
		lifecycle.RulePrivateRepositoryPublishing:    lifecycle.WhileValueHolds,
		lifecycle.RuleLifecycleAllowsRelease:         lifecycle.WhileValueHolds,
		lifecycle.RuleConfidentialNames:              lifecycle.WhileValueHolds,
		lifecycle.RuleProprietaryHistoryIsSquashed:   lifecycle.OverCreatedDuringPeriod,
		lifecycle.RuleIdentityOwnsItsTags:            lifecycle.OverCreatedDuringPeriod,
		lifecycle.RuleRegistryNamesAreHeld:           lifecycle.PermanentOnceTriggered,
		lifecycle.RuleClosedPeriodsAreFinal:          lifecycle.PermanentOnceTriggered,
	}
	if len(want) != len(seen) {
		t.Fatalf("Rules() lists %d rules, the table here %d", len(seen), len(want))
	}
	for rule, class := range want {
		if rule.Class() != class {
			t.Errorf("rule %s: class %q, want %q", rule, rule.Class(), class)
		}
	}
}

func TestConfidentialFollowsTheProprietaryPeriod(t *testing.T) {
	r := parse(t, sharedRecord)
	if r.Confidential(day(t, "2026-10-06")) {
		t.Fatal("confidential the day before the proprietary period")
	}
	if !r.Confidential(day(t, "2026-10-07")) {
		t.Fatal("public on the first day of the proprietary period")
	}
	closed := parse(t, `format_version = 1
[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-01-01
until = 2026-06-01
reason = "a"
`)
	if closed.Confidential(day(t, "2026-06-01")) {
		t.Fatal("confidential on the until date, which is after the period")
	}
}

// proprietary-requires-private

func TestVisibilityAllowed(t *testing.T) {
	r := parse(t, sharedRecord)
	confidential, public := day(t, "2026-12-01"), day(t, "2026-05-01")
	if err := r.VisibilityAllowed(lifecycle.VisibilityPrivate, confidential); err != nil {
		t.Errorf("confidential and private: %v", err)
	}
	if err := r.VisibilityAllowed(lifecycle.VisibilityPublic, public); err != nil {
		t.Errorf("public and public: %v", err)
	}
	ref := refusal(t, r.VisibilityAllowed(lifecycle.VisibilityPublic, confidential), lifecycle.RuleProprietaryRequiresPrivate)
	if ref.Subject != "portal" || !ref.Period.From.Equal(day(t, "2026-10-07")) {
		t.Errorf("refusal names %q %v", ref.Subject, ref.Period)
	}
	refusal(t, r.VisibilityAllowed(lifecycle.VisibilityPrivate, public), lifecycle.RuleProprietaryRequiresPrivate)
	refusal(t, r.VisibilityAllowed(lifecycle.VisibilityUnknown, public), lifecycle.RuleProprietaryRequiresPrivate)
	refusal(t, r.VisibilityAllowed(lifecycle.VisibilityUnknown, confidential), lifecycle.RuleProprietaryRequiresPrivate)
}

func TestARepositoryWithoutARecordMustNotBePrivate(t *testing.T) {
	r, err := lifecycle.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, r.VisibilityAllowed(lifecycle.VisibilityPrivate, day(t, "2026-10-07")), lifecycle.RuleProprietaryRequiresPrivate)
	if err := r.VisibilityAllowed(lifecycle.VisibilityPublic, day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
}

// proprietary-refuses-public-output

func TestAProprietarySubjectProducesNoPublicOutput(t *testing.T) {
	r := parse(t, sharedRecord)
	on := day(t, "2026-12-01")
	for _, out := range lifecycle.Outputs() {
		ref := refusal(t, r.PublicOutputAllowed("portal", out, on), lifecycle.RuleProprietaryRefusesPublicOutput)
		if ref.Subject != "portal" || !strings.Contains(ref.Error(), string(out)) {
			t.Errorf("%s: refusal %v", out, ref)
		}
	}
}

func TestAPublicSubjectAndAnUnlicensedSubjectProduceOutput(t *testing.T) {
	r := parse(t, sharedRecord)
	on := day(t, "2026-12-01")
	for _, out := range lifecycle.Outputs() {
		if err := r.PublicOutputAllowed("widget", out, on); err != nil {
			t.Errorf("widget %s: %v", out, err)
		}
		if err := r.PublicOutputAllowed("gadget", out, on); err != nil {
			t.Errorf("gadget %s: %v", out, err)
		}
		if err := r.PublicOutputAllowed("portal", out, day(t, "2026-05-01")); err != nil {
			t.Errorf("portal before its proprietary period %s: %v", out, err)
		}
	}
}

// private-repository-publishing

func TestAConfidentialRepositoryRefusesIdentityRecordingOutputs(t *testing.T) {
	r := parse(t, sharedRecord)
	on := day(t, "2026-12-01")
	refused := map[lifecycle.Output]bool{
		lifecycle.BuildAttestation:        true,
		lifecycle.GoProxyNotification:     true,
		lifecycle.GoLibrary:               true,
		lifecycle.HomebrewTap:             true,
		lifecycle.RepositoryURLInManifest: true,
	}
	for _, out := range lifecycle.Outputs() {
		err := r.PrivateRepositoryOutputAllowed(out, lifecycle.VisibilityPrivate, on)
		if refused[out] {
			refusal(t, err, lifecycle.RulePrivateRepositoryPublishing)
		} else if err != nil {
			t.Errorf("%s refused: %v", out, err)
		}
	}
}

func TestAPublicRepositoryPublishesEveryOutput(t *testing.T) {
	r := parse(t, sharedRecord)
	for _, out := range lifecycle.Outputs() {
		if err := r.PrivateRepositoryOutputAllowed(out, lifecycle.VisibilityPublic, day(t, "2026-05-01")); err != nil {
			t.Errorf("%s: %v", out, err)
		}
	}
}

// The cases below port the private-repository publishing guard's tests: a Go
// library, a Go proxy notification, a PyPI attestation, and npm provenance
// are refused when GitHub reports the repository private, and pass when it
// reports it public.

func TestAPrivateRepositoryRefusesAGoLibrary(t *testing.T) {
	r := parse(t, "format_version = 1\n")
	ref := refusal(t, r.PrivateRepositoryOutputAllowed(lifecycle.GoLibrary, lifecycle.VisibilityPrivate, day(t, "2026-10-07")), lifecycle.RulePrivateRepositoryPublishing)
	if !strings.Contains(ref.Fix, "binaries") {
		t.Errorf("fix does not name binaries: %v", ref)
	}
}

func TestAPrivateRepositoryRefusesAGoProxyNotification(t *testing.T) {
	r := parse(t, "format_version = 1\n")
	refusal(t, r.PrivateRepositoryOutputAllowed(lifecycle.GoProxyNotification, lifecycle.VisibilityPrivate, day(t, "2026-10-07")), lifecycle.RulePrivateRepositoryPublishing)
}

func TestAPrivateRepositoryRefusesBuildAttestations(t *testing.T) {
	r := parse(t, "format_version = 1\n")
	ref := refusal(t, r.PrivateRepositoryOutputAllowed(lifecycle.BuildAttestation, lifecycle.VisibilityPrivate, day(t, "2026-10-07")), lifecycle.RulePrivateRepositoryPublishing)
	// The fix is performed in the committed publish workflow: no declaration
	// carries build attestations, so the text names no declaration key.
	if !strings.Contains(ref.Fix, "attestations: false") || !strings.Contains(ref.Fix, "--provenance") {
		t.Errorf("fix names neither PyPI attestations nor npm --provenance: %v", ref)
	}
	if strings.Contains(ref.Fix, "provenance false") || strings.Contains(ref.Fix, "pipeline") {
		t.Errorf("fix names a declaration key that does not exist: %v", ref)
	}
}

func TestAnUnknownVisibilityRefusesIdentityRecordingOutputs(t *testing.T) {
	r := parse(t, "format_version = 1\n")
	ref := refusal(t, r.PrivateRepositoryOutputAllowed(lifecycle.BuildAttestation, lifecycle.VisibilityUnknown, day(t, "2026-10-07")), lifecycle.RulePrivateRepositoryPublishing)
	if !strings.Contains(ref.Fix, "gh auth status") {
		t.Errorf("fix does not name gh auth status: %v", ref)
	}
	if err := r.PrivateRepositoryOutputAllowed(lifecycle.RegistryPackage, lifecycle.VisibilityUnknown, day(t, "2026-10-07")); err != nil {
		t.Errorf("a registry package needs no visibility: %v", err)
	}
}

func TestPublishAllowedComposesBothPublishingRules(t *testing.T) {
	r := parse(t, sharedRecord)
	on := day(t, "2026-12-01")
	// A public client in a confidential repository publishes its package...
	if err := r.PublishAllowed("widget", lifecycle.RegistryPackage, lifecycle.VisibilityPrivate, on); err != nil {
		t.Fatalf("widget package: %v", err)
	}
	// ...without attestations.
	refusal(t, r.PublishAllowed("widget", lifecycle.BuildAttestation, lifecycle.VisibilityPrivate, on), lifecycle.RulePrivateRepositoryPublishing)
	// The proprietary server publishes nothing.
	refusal(t, r.PublishAllowed("portal", lifecycle.RegistryPackage, lifecycle.VisibilityPrivate, on), lifecycle.RuleProprietaryRefusesPublicOutput)
}

// lifecycle-allows-release

func TestReleaseAllowed(t *testing.T) {
	r := parse(t, sharedRecord)
	if err := r.ReleaseAllowed("portal", day(t, "2026-12-01")); err != nil {
		t.Errorf("active: %v", err)
	}
	if err := r.ReleaseAllowed("widget", day(t, "2026-10-31")); err != nil {
		t.Errorf("active before the hold: %v", err)
	}
	ref := refusal(t, r.ReleaseAllowed("widget", day(t, "2026-11-01")), lifecycle.RuleLifecycleAllowsRelease)
	if ref.Subject != "widget" || !strings.Contains(ref.Detail, "on-hold") {
		t.Errorf("on-hold refusal %v", ref)
	}
	ref = refusal(t, r.ReleaseAllowed("gadget", day(t, "2026-12-01")), lifecycle.RuleLifecycleAllowsRelease)
	if !strings.Contains(ref.Detail, "retired") {
		t.Errorf("retired refusal %v", ref)
	}
	if err := r.ReleaseAllowed("unrecorded", day(t, "2026-12-01")); err != nil {
		t.Errorf("a subject without a lifecycle period: %v", err)
	}
}

func TestReopeningAnActivePeriodClearsTheHold(t *testing.T) {
	r := parse(t, sharedRecord)
	if err := r.ClosePeriod(lifecycle.TableLifecycle, "widget", day(t, "2026-12-01")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPeriod(lifecycle.TableLifecycle, "widget", "active", day(t, "2026-12-01"), "resumed"); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseAllowed("widget", day(t, "2026-12-01")); err != nil {
		t.Fatalf("the hold did not clear: %v", err)
	}
}

// confidential-names

func TestConfidentialNames(t *testing.T) {
	r := parse(t, sharedRecord)
	names, err := r.ConfidentialNames(day(t, "2026-12-01"), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bluebird", "hyperlattice", "portal", "portal-legacy", "portal-server"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names %v, want %v (no tag format, no public subject, no repository name)", names, want)
	}
	public, err := r.ConfidentialNames(day(t, "2026-05-01"), "")
	if err != nil || len(public) != 0 {
		t.Fatalf("a public repository has names %v (%v)", public, err)
	}
}

func TestTheRepositoryNameIsConfidentialWhenEveryLicenseIsProprietary(t *testing.T) {
	r := parse(t, `format_version = 1
[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-01-01
reason = "a"
`)
	ref := refusal(t, func() error { _, err := r.ConfidentialNames(day(t, "2026-10-07"), ""); return err }(), lifecycle.RuleConfidentialNames)
	if !strings.Contains(ref.Fix, "repository's name") {
		t.Errorf("fix %q", ref.Fix)
	}
	names, err := r.ConfidentialNames(day(t, "2026-10-07"), "Portal")
	if err != nil {
		t.Fatalf("passing the repository name did not clear the refusal: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"Portal"}) {
		t.Fatalf("names %v: the subject and the repository name differ only in case and keep one spelling", names)
	}
}

// proprietary-history-is-squashed

func TestProprietaryPeriodsMergeOverlappingAndTouchingRanges(t *testing.T) {
	r := parse(t, `format_version = 1
[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-01-01
until = 2026-03-01
reason = "a"
[[licenses]]
subject = "widget"
license = "proprietary"
from = 2026-02-01
until = 2026-04-01
reason = "b"
[[licenses]]
subject = "gadget"
license = "proprietary"
from = 2026-04-01
until = 2026-05-01
reason = "c"
[[licenses]]
subject = "portal"
license = "MIT"
from = 2026-03-01
until = 2026-07-01
reason = "d"
[[licenses]]
subject = "portal"
license = "proprietary"
from = 2026-07-01
reason = "e"
`)
	got := r.ProprietaryPeriods()
	if len(got) != 2 {
		t.Fatalf("periods %v", got)
	}
	if !got[0].From.Equal(day(t, "2026-01-01")) || !got[0].Until.Equal(day(t, "2026-05-01")) {
		t.Errorf("first range %v", got[0])
	}
	if !got[1].From.Equal(day(t, "2026-07-01")) || !got[1].Open() {
		t.Errorf("second range %v", got[1])
	}
}

func TestARepositoryNeverProprietaryHasNoPeriodsToSquash(t *testing.T) {
	r := parse(t, `format_version = 1
[[licenses]]
subject = "widget"
license = "MIT"
from = 2026-01-01
reason = "a"
`)
	if got := r.ProprietaryPeriods(); len(got) != 0 {
		t.Fatalf("periods %v", got)
	}
}

// identity-owns-its-tags

func TestTagOwner(t *testing.T) {
	r := parse(t, `format_version = 1
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["portal@v*"]
from = 2026-01-01
until = 2026-06-01
reason = "first name"
[[identities]]
subject = "gateway"
facet = "releasable-name"
value = "gateway"
registry = ""
tag_patterns = ["gateway@v*"]
from = 2026-06-01
reason = "renamed"
[[identities]]
subject = "gateway"
facet = "package-name"
value = "gateway-client"
registry = "npm"
tag_patterns = ["gateway@v*"]
effective_version = "0.9.0"
reason = "pending rename"
`)
	id, ok, err := r.TagOwner("portal@v0.3.0", day(t, "2026-02-01"))
	if err != nil || !ok || id.Subject != "portal" {
		t.Fatalf("owner %+v %v %v", id, ok, err)
	}
	if _, ok, _ := r.TagOwner("portal@v0.4.0", day(t, "2026-07-01")); ok {
		t.Fatal("a tag created after the identity's period is owned by it")
	}
	if _, ok, _ := r.TagOwner("gateway@v0.1.0", day(t, "2026-05-31")); ok {
		t.Fatal("a tag created before the identity's period is owned by it")
	}
	id, ok, err = r.TagOwner("gateway@v0.1.0", day(t, "2026-06-01"))
	if err != nil || !ok || id.Facet != lifecycle.FacetReleasableName {
		t.Fatalf("owner %+v %v %v (a pending identity owns nothing)", id, ok, err)
	}
}

func TestATagMatchingTwoSubjectsIsAmbiguous(t *testing.T) {
	r := parse(t, `format_version = 1
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["v*"]
from = 2026-01-01
reason = "a"
[[identities]]
subject = "widget"
facet = "releasable-name"
value = "widget"
registry = ""
tag_patterns = ["v*"]
from = 2026-01-01
reason = "b"
`)
	_, _, err := r.TagOwner("v0.1.0", day(t, "2026-02-01"))
	ref := refusal(t, err, lifecycle.RuleIdentityOwnsItsTags)
	if !strings.Contains(ref.Detail, `"portal"`) || !strings.Contains(ref.Detail, `"widget"`) {
		t.Fatalf("refusal names %v", ref)
	}
}

func TestMatchTag(t *testing.T) {
	cases := []struct {
		pattern, tag string
		want         bool
	}{
		{"v*", "v1.2.3", true},
		{"v*", "portal@v1.2.3", false},
		{"portal@v*", "portal@v0.1.0", true},
		{"go/v*", "go/v1/extra", true},
		{"v?.1.0", "v0.1.0", true},
		{"v?.1.0", "v10.1.0", false},
		{"nightly", "nightly", true},
		{"nightly", "nightly2", false},
		{"*", "", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
	}
	for _, c := range cases {
		if got := lifecycle.MatchTag(c.pattern, c.tag); got != c.want {
			t.Errorf("MatchTag(%q, %q) = %v", c.pattern, c.tag, got)
		}
	}
}

// registry-names-are-held and closed-periods-are-final

func TestCheckAppendOnlyAcceptsGrowth(t *testing.T) {
	previous := parse(t, sharedRecord)
	current := parse(t, sharedRecord)
	if err := current.CheckAppendOnly(nil); err != nil {
		t.Fatalf("no previous record: %v", err)
	}
	if err := current.ClosePeriod(lifecycle.TableLicenses, "widget", day(t, "2026-12-01")); err != nil {
		t.Fatal(err)
	}
	if err := current.OpenPeriod(lifecycle.TableLicenses, "widget", "Apache-2.0", day(t, "2026-12-01"), "relicensed"); err != nil {
		t.Fatal(err)
	}
	if err := current.AddRegistryName("pypi", "widget", "widget", day(t, "2026-12-01")); err != nil {
		t.Fatal(err)
	}
	if err := current.CheckAppendOnly(previous); err != nil {
		t.Fatalf("closing an open period and adding entries: %v", err)
	}
}

func TestARemovedRegistryNameIsRefused(t *testing.T) {
	previous := parse(t, sharedRecord)
	current := parse(t, strings.Replace(sharedRecord, `name = "portal-legacy"`, `name = "portal-renamed"`, 1))
	err := current.CheckAppendOnly(previous)
	if err == nil || !strings.Contains(err.Error(), string(lifecycle.RuleRegistryNamesAreHeld)) || !strings.Contains(err.Error(), "portal-legacy") {
		t.Fatalf("want a refusal naming portal-legacy, got %v", err)
	}
	if err := current.CheckRegistryNamesHeld(previous); err == nil {
		t.Fatal("the rule's own evaluation accepted the removal")
	}
	// The fix: restore the entry.
	if err := parse(t, sharedRecord).CheckAppendOnly(previous); err != nil {
		t.Fatalf("restoring the entry did not clear the refusal: %v", err)
	}
}

func TestAChangedClosedPeriodIsRefused(t *testing.T) {
	previous := parse(t, sharedRecord)
	// The proprietary period moves with the closed one so the two stay
	// non-overlapping.
	current := parse(t, strings.Replace(strings.Replace(sharedRecord, "until = 2026-10-07", "until = 2026-10-08", 1),
		"from = 2026-10-07\nreason = \"server logic\"", "from = 2026-10-08\nreason = \"server logic\"", 1))
	err := current.CheckAppendOnly(previous)
	if err == nil || !strings.Contains(err.Error(), string(lifecycle.RuleClosedPeriodsAreFinal)) || !strings.Contains(err.Error(), "license MIT") {
		t.Fatalf("want a refusal naming the closed MIT period, got %v", err)
	}
	if err := current.CheckClosedPeriodsFinal(previous); err == nil {
		t.Fatal("the rule's own evaluation accepted the change")
	}
}

func TestARemovedClosedIdentityIsRefused(t *testing.T) {
	src := `format_version = 1
[[identities]]
subject = "portal"
facet = "releasable-name"
value = "portal"
registry = ""
tag_patterns = ["v*"]
from = 2026-01-01
until = 2026-03-01
reason = "first name"
`
	previous := parse(t, src)
	current := parse(t, "format_version = 1\n")
	err := current.CheckAppendOnly(previous)
	if err == nil || !strings.Contains(err.Error(), `releasable-name "portal"`) {
		t.Fatalf("want a refusal naming the identity, got %v", err)
	}
}

func TestActivatingAPendingIdentityIsNotAChangeToAClosedPeriod(t *testing.T) {
	src := `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal"
registry = "npm"
tag_patterns = ["v*"]
from = 2026-01-01
reason = "first name"
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "npm"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"
`
	previous := parse(t, src)
	current := parse(t, src)
	if _, err := current.ActivatePendingIdentities("0.5.0", day(t, "2026-10-07")); err != nil {
		t.Fatal(err)
	}
	if err := current.CheckAppendOnly(previous); err != nil {
		t.Fatal(err)
	}
}
