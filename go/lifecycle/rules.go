package lifecycle

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Rule is one rule of the closed rule set the record is evaluated against.
// Nothing in the record names a rule; callers ask the typed questions below,
// and each rule has one evaluation function:
//
//   - RuleProprietaryRequiresPrivate: (*Record).VisibilityAllowed
//   - RuleProprietaryRefusesPublicOutput: (*Record).PublicOutputAllowed
//   - RulePrivateRepositoryPublishing: (*Record).PrivateRepositoryOutputAllowed
//   - RuleLifecycleAllowsRelease: (*Record).ReleaseAllowed
//   - RuleConfidentialNames: (*Record).ConfidentialNames
//   - RuleProprietaryHistoryIsSquashed: (*Record).ProprietaryPeriods
//   - RuleIdentityOwnsItsTags: (*Record).TagOwner
//   - RuleRegistryNamesAreHeld: (*Record).CheckRegistryNamesHeld
//   - RuleClosedPeriodsAreFinal: (*Record).CheckClosedPeriodsFinal
type Rule string

const (
	RuleProprietaryRequiresPrivate     Rule = "proprietary-requires-private"
	RuleProprietaryRefusesPublicOutput Rule = "proprietary-refuses-public-output"
	RulePrivateRepositoryPublishing    Rule = "private-repository-publishing"
	RuleLifecycleAllowsRelease         Rule = "lifecycle-allows-release"
	RuleConfidentialNames              Rule = "confidential-names"
	RuleProprietaryHistoryIsSquashed   Rule = "proprietary-history-is-squashed"
	RuleIdentityOwnsItsTags            Rule = "identity-owns-its-tags"
	RuleRegistryNamesAreHeld           Rule = "registry-names-are-held"
	RuleClosedPeriodsAreFinal          Rule = "closed-periods-are-final"
)

// RuleClass is when a rule applies.
type RuleClass string

const (
	// WhileValueHolds rules apply while a period's value is in effect.
	WhileValueHolds RuleClass = "while a value holds"
	// OverCreatedDuringPeriod rules apply to what was created while a period
	// ran.
	OverCreatedDuringPeriod RuleClass = "over what was created during a period"
	// PermanentOnceTriggered rules apply forever once something is recorded.
	PermanentOnceTriggered RuleClass = "permanent once triggered"
)

var ruleClasses = map[Rule]RuleClass{
	RuleProprietaryRequiresPrivate:     WhileValueHolds,
	RuleProprietaryRefusesPublicOutput: WhileValueHolds,
	RulePrivateRepositoryPublishing:    WhileValueHolds,
	RuleLifecycleAllowsRelease:         WhileValueHolds,
	RuleConfidentialNames:              WhileValueHolds,
	RuleProprietaryHistoryIsSquashed:   OverCreatedDuringPeriod,
	RuleIdentityOwnsItsTags:            OverCreatedDuringPeriod,
	RuleRegistryNamesAreHeld:           PermanentOnceTriggered,
	RuleClosedPeriodsAreFinal:          PermanentOnceTriggered,
}

// Rules returns every rule, in a fixed order.
func Rules() []Rule {
	return []Rule{
		RuleProprietaryRequiresPrivate,
		RuleProprietaryRefusesPublicOutput,
		RulePrivateRepositoryPublishing,
		RuleLifecycleAllowsRelease,
		RuleConfidentialNames,
		RuleProprietaryHistoryIsSquashed,
		RuleIdentityOwnsItsTags,
		RuleRegistryNamesAreHeld,
		RuleClosedPeriodsAreFinal,
	}
}

// Class returns when the rule applies.
func (r Rule) Class() RuleClass { return ruleClasses[r] }

// Refusal is the error every rule returns: the rule, the subject (empty for a
// repository-wide refusal), the period the refusal rests on (zero when none
// does), what is refused, and what to do.
type Refusal struct {
	Rule    Rule
	Subject string
	Period  Period
	Detail  string
	Fix     string
}

func (e *Refusal) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Rule))
	b.WriteString(": ")
	if e.Subject != "" {
		fmt.Fprintf(&b, "%s: ", e.Subject)
	}
	b.WriteString(e.Detail)
	if !e.Period.From.IsZero() {
		fmt.Fprintf(&b, " (period %s)", e.Period.describe())
	}
	b.WriteString(". ")
	b.WriteString(e.Fix)
	return b.String()
}

// Visibility is the repository's visibility on GitHub, as the caller learned
// it. VisibilityUnknown is an unanswered question, never read as public.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
	VisibilityUnknown Visibility = "unknown"
)

// Output is a publishing output a release or a documentation tool produces.
type Output string

const (
	// RegistryPackage is any registry write for a package: a publish, an npm
	// deprecate, or a Go retract.
	RegistryPackage Output = "registry-package"
	// BuildAttestation is npm build provenance or a PyPI attestation.
	BuildAttestation Output = "build-attestation"
	// GoProxyNotification is a request to the Go module proxy for a released
	// version.
	GoProxyNotification Output = "go-proxy-notification"
	// GoLibrary is a Go pipeline publishing a library rather than binaries.
	GoLibrary Output = "go-library"
	// HomebrewTap is a Homebrew tap formula.
	HomebrewTap Output = "homebrew-tap"
	// RepositoryURLInManifest is a published manifest field naming the
	// repository (package.json repository, homepage, and bugs; pyproject.toml
	// [project.urls]).
	RepositoryURLInManifest Output = "repository-url-in-manifest"
	// PublicDocs is a public documentation deploy.
	PublicDocs Output = "public-docs"
	// BlogPost is a published blog post.
	BlogPost Output = "blog-post"
)

// Outputs returns every output, in a fixed order.
func Outputs() []Output {
	return []Output{
		RegistryPackage, BuildAttestation, GoProxyNotification, GoLibrary,
		HomebrewTap, RepositoryURLInManifest, PublicDocs, BlogPost,
	}
}

// privateRepositoryOutputs are the outputs a confidential or private
// repository never produces, whatever the subject's license.
var privateRepositoryOutputs = map[Output]bool{
	BuildAttestation:        true,
	GoProxyNotification:     true,
	GoLibrary:               true,
	HomebrewTap:             true,
	RepositoryURLInManifest: true,
}

// LicenseOn returns the subject's license period in effect on the date of on.
func (r *Record) LicenseOn(subject string, on time.Time) (LicensePeriod, bool) {
	for _, l := range r.licenses {
		if l.Subject == subject && l.Contains(on) {
			return l, true
		}
	}
	return LicensePeriod{}, false
}

// LifecycleOn returns the subject's lifecycle period in effect on the date of
// on.
func (r *Record) LifecycleOn(subject string, on time.Time) (LifecyclePeriod, bool) {
	for _, l := range r.lifecycle {
		if l.Subject == subject && l.Contains(on) {
			return l, true
		}
	}
	return LifecyclePeriod{}, false
}

// Confidential reports whether a subject's license period in effect on the
// date of on is proprietary. A repository without a record is public.
func (r *Record) Confidential(on time.Time) bool {
	_, ok := r.firstProprietary(on)
	return ok
}

func (r *Record) firstProprietary(on time.Time) (LicensePeriod, bool) {
	for _, l := range r.licenses {
		if l.Proprietary() && l.Contains(on) {
			return l, true
		}
	}
	return LicensePeriod{}, false
}

// VisibilityAllowed evaluates RuleProprietaryRequiresPrivate: a confidential
// repository must be private on GitHub, and a private repository must be
// confidential (private repositories without a proprietary releasable are not
// supported). An unknown visibility is refused.
func (r *Record) VisibilityAllowed(visibility Visibility, on time.Time) error {
	prop, confidential := r.firstProprietary(on)
	switch {
	case visibility == VisibilityUnknown:
		return &Refusal{
			Rule:   RuleProprietaryRequiresPrivate,
			Detail: "the repository's GitHub visibility could not be established, and the rule needs it",
			Fix:    "Check `gh auth status` and that the repository's origin is on GitHub, then run the command again.",
		}
	case confidential && visibility != VisibilityPrivate:
		return &Refusal{
			Rule:    RuleProprietaryRequiresPrivate,
			Subject: prop.Subject,
			Period:  prop.Period,
			Detail:  "the license is proprietary, so the repository is confidential, but GitHub reports it " + string(visibility),
			Fix:     "Make the GitHub repository private (the owner's action), or close the proprietary license period with a declassification.",
		}
	case !confidential && visibility == VisibilityPrivate:
		return &Refusal{
			Rule:   RuleProprietaryRequiresPrivate,
			Detail: "GitHub reports the repository private, but no releasable has a proprietary license period on " + formatDate(dateOf(on)) + ", and a private repository without a proprietary releasable is not supported",
			Fix:    "Classify the proprietary releasable (open a proprietary license period for it), or make the GitHub repository public.",
		}
	}
	return nil
}

// PublicOutputAllowed evaluates RuleProprietaryRefusesPublicOutput: a subject
// whose license on the date of on is proprietary produces no public output of
// any type.
func (r *Record) PublicOutputAllowed(subject string, output Output, on time.Time) error {
	l, ok := r.LicenseOn(subject, on)
	if !ok || !l.Proprietary() {
		return nil
	}
	return &Refusal{
		Rule:    RuleProprietaryRefusesPublicOutput,
		Subject: subject,
		Period:  l.Period,
		Detail:  fmt.Sprintf("the license is proprietary, so it produces no public output, and %s is one", output),
		Fix:     "Remove the output from the subject's release (set publish_mode to none for a registry), or open a public license period for the subject.",
	}
}

// PrivateRepositoryOutputAllowed evaluates RulePrivateRepositoryPublishing: in
// a confidential repository, and in any repository GitHub reports private or
// whose visibility is unknown, no output records the repository's identity
// anywhere public (build attestations, Go module proxy notifications, Go
// libraries, Homebrew taps, and repository URLs in published manifests).
func (r *Record) PrivateRepositoryOutputAllowed(output Output, visibility Visibility, on time.Time) error {
	if !privateRepositoryOutputs[output] {
		return nil
	}
	prop, confidential := r.firstProprietary(on)
	fix := privateOutputFix[output]
	switch {
	case confidential:
		return &Refusal{
			Rule:    RulePrivateRepositoryPublishing,
			Subject: prop.Subject,
			Period:  prop.Period,
			Detail:  fmt.Sprintf("the repository is confidential (this license is proprietary), and %s records the repository's identity publicly", output),
			Fix:     fix,
		}
	case visibility == VisibilityPrivate:
		return &Refusal{
			Rule:   RulePrivateRepositoryPublishing,
			Detail: fmt.Sprintf("GitHub reports the repository private, and %s records the repository's identity publicly", output),
			Fix:    fix + " Making the repository public also clears this.",
		}
	case visibility == VisibilityUnknown:
		return &Refusal{
			Rule:   RulePrivateRepositoryPublishing,
			Detail: fmt.Sprintf("the repository's GitHub visibility could not be established, and %s needs a public repository", output),
			Fix:    fix + " If the repository is on GitHub, check `gh auth status` and run the command again.",
		}
	}
	return nil
}

var privateOutputFix = map[Output]string{
	BuildAttestation:        "Turn build attestations off in the committed publish workflow: regenerate it so its npm publish carries no --provenance and its PyPI publish step carries attestations: false, and commit the result.",
	GoProxyNotification:     "Publish the Go code as binaries only (no library pipeline and no local Go pipeline), regenerate the publish workflow, and commit the result.",
	GoLibrary:               "Publish the Go code as binaries through npm per-platform packages and PyPI wheels instead of a library pipeline.",
	HomebrewTap:             "Remove the Homebrew tap from the releasable's pipelines.",
	RepositoryURLInManifest: "Remove the repository, homepage, and bugs fields from package.json and the [project.urls] table from pyproject.toml.",
}

// PublishAllowed evaluates both publishing rules for one output of one
// subject: RuleProprietaryRefusesPublicOutput, then
// RulePrivateRepositoryPublishing.
func (r *Record) PublishAllowed(subject string, output Output, visibility Visibility, on time.Time) error {
	if err := r.PublicOutputAllowed(subject, output, on); err != nil {
		return err
	}
	return r.PrivateRepositoryOutputAllowed(output, visibility, on)
}

// ReleaseAllowed evaluates RuleLifecycleAllowsRelease: a subject whose
// lifecycle status on the date of on is on-hold or retired is not released. A
// subject without a lifecycle period is released.
func (r *Record) ReleaseAllowed(subject string, on time.Time) error {
	l, ok := r.LifecycleOn(subject, on)
	if !ok || l.Status == StatusActive {
		return nil
	}
	fix := "Open an active lifecycle period for the subject before releasing it."
	if l.Status == StatusRetired {
		fix = "A retired subject's release history is a record, not something to release from; open an active lifecycle period first if the subject returns."
	}
	return &Refusal{
		Rule:    RuleLifecycleAllowsRelease,
		Subject: subject,
		Period:  l.Period,
		Detail:  fmt.Sprintf("the lifecycle status is %s, so it is not released", l.Status),
		Fix:     fix,
	}
}

// ConfidentialNames evaluates RuleConfidentialNames: while the repository is
// confidential on the date of on, the names the confidential-name index holds
// for it. They are every subject whose license on that date is proprietary,
// every identity value of those subjects other than tag formats, the registry
// names recorded for them, the repository's name when no subject has a
// non-proprietary license on that date, the codenames, and the distinctive
// terms; deduplicated ignoring case and sorted. A public repository has none.
// repositoryName is required only when it is one of the names.
func (r *Record) ConfidentialNames(on time.Time, repositoryName string) ([]string, error) {
	if !r.Confidential(on) {
		return nil, nil
	}
	proprietary := map[string]bool{}
	anyPublic := false
	for _, l := range r.licenses {
		if !l.Contains(on) {
			continue
		}
		if l.Proprietary() {
			proprietary[l.Subject] = true
		} else {
			anyPublic = true
		}
	}
	var names []string
	for subject := range proprietary {
		names = append(names, subject)
	}
	for _, id := range r.identities {
		if proprietary[id.Subject] && id.Facet != FacetTagFormat {
			names = append(names, id.Value)
		}
	}
	for _, n := range r.registryNames {
		if proprietary[n.Subject] {
			names = append(names, n.Name)
		}
	}
	if !anyPublic {
		if strings.TrimSpace(repositoryName) == "" {
			return nil, &Refusal{
				Rule:   RuleConfidentialNames,
				Detail: "no releasable carries a non-proprietary license, so the repository's name is confidential, and no repository name was given",
				Fix:    "Pass the repository's name (the last segment of its origin URL).",
			}
		}
		names = append(names, repositoryName)
	}
	names = append(names, r.codenames...)
	names = append(names, r.distinctiveTerms...)
	return dedupeFold(names), nil
}

// dedupeFold drops empty names and names equal ignoring case to an earlier
// one (keeping the first spelling in sorted order), and sorts the result.
func dedupeFold(names []string) []string {
	sort.Strings(names)
	seen := map[string]bool{}
	out := []string{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		k := strings.ToLower(n)
		if n == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return out
}

// ProprietaryPeriods evaluates RuleProprietaryHistoryIsSquashed: the union of
// every subject's proprietary license periods, as disjoint date ranges in
// date order. Periods that overlap or touch are merged; an open period stays
// open. Each range's commits are squashed into one before the repository is
// declassified.
func (r *Record) ProprietaryPeriods() []Period {
	var ps []Period
	for _, l := range r.licenses {
		if l.Proprietary() {
			ps = append(ps, l.Period)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].From.Before(ps[j].From) })
	var out []Period
	for _, p := range ps {
		if len(out) == 0 {
			out = append(out, p)
			continue
		}
		last := &out[len(out)-1]
		if last.Open() || !p.From.After(last.Until) {
			if last.Open() || p.Open() {
				last.Until = time.Time{}
			} else if p.Until.After(last.Until) {
				last.Until = p.Until
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// TagOwner evaluates RuleIdentityOwnsItsTags: the dated identity one of whose
// tag patterns matches tag and whose period covers the date of created (the
// tag's creation). Pending identities own nothing. When several identities of
// one subject match, the first in record order is returned; identities of
// different subjects matching the same tag are an error naming both, because
// the tag's owner would be a guess.
func (r *Record) TagOwner(tag string, created time.Time) (Identity, bool, error) {
	var found []Identity
	for _, id := range r.identities {
		if id.Pending() || !id.Contains(created) {
			continue
		}
		for _, p := range id.TagPatterns {
			if MatchTag(p, tag) {
				found = append(found, id)
				break
			}
		}
	}
	if len(found) == 0 {
		return Identity{}, false, nil
	}
	for _, id := range found[1:] {
		if id.Subject != found[0].Subject {
			return Identity{}, false, &Refusal{
				Rule:   RuleIdentityOwnsItsTags,
				Detail: fmt.Sprintf("tag %q created on %s matches the tag patterns of %q (%s) and of %q (%s), so its owner is ambiguous", tag, formatDate(dateOf(created)), found[0].Subject, found[0].Facet, id.Subject, id.Facet),
				Fix:    "Narrow the tag patterns of one of the identities so each tag has one owner.",
			}
		}
	}
	id := found[0]
	id.TagPatterns = append([]string(nil), id.TagPatterns...)
	return id, true, nil
}

// MatchTag reports whether tag matches the glob pattern: * matches any run of
// characters (slashes included, as git's tag listing does) and ? matches one
// character; every other character matches itself.
func MatchTag(pattern, tag string) bool {
	p := []rune(pattern)
	t := []rune(tag)
	// Iterative glob match with single-star backtracking.
	pi, ti := 0, 0
	star, mark := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star = pi
			mark = ti
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// CheckAppendOnly evaluates the permanent rules against the record as it
// stood at the nearest release commit: RuleRegistryNamesAreHeld and
// RuleClosedPeriodsAreFinal. A nil or absent previous record holds nothing.
func (r *Record) CheckAppendOnly(previous *Record) error {
	var problems []string
	if err := r.CheckRegistryNamesHeld(previous); err != nil {
		problems = append(problems, err.(*AppendOnlyError).Problems...)
	}
	if err := r.CheckClosedPeriodsFinal(previous); err != nil {
		problems = append(problems, err.(*AppendOnlyError).Problems...)
	}
	if len(problems) > 0 {
		return &AppendOnlyError{Problems: problems}
	}
	return nil
}

// AppendOnlyError lists every entry of the previous record the current one
// removed or changed, each a refusal naming its rule.
type AppendOnlyError struct {
	Problems []string
}

func (e *AppendOnlyError) Error() string {
	return "the lifecycle-and-license record changed what it must keep:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// CheckRegistryNamesHeld evaluates RuleRegistryNamesAreHeld: every registry
// name the previous record held is still recorded, unchanged.
func (r *Record) CheckRegistryNamesHeld(previous *Record) error {
	if previous == nil {
		return nil
	}
	var problems []string
	for _, old := range previous.registryNames {
		kept := false
		for _, n := range r.registryNames {
			if registryNameEqual(n, old) {
				kept = true
				break
			}
		}
		if !kept {
			problems = append(problems, (&Refusal{
				Rule:    RuleRegistryNamesAreHeld,
				Subject: old.Subject,
				Detail:  fmt.Sprintf("the %s name %q recorded since %s was removed or changed", old.Registry, old.Name, formatDate(old.RecordedSince)),
				Fix:     "Restore the [[registry_names]] entry as it was; a registry name, once recorded, stays in the record.",
			}).Error())
		}
	}
	if len(problems) > 0 {
		return &AppendOnlyError{Problems: problems}
	}
	return nil
}

// CheckClosedPeriodsFinal evaluates RuleClosedPeriodsAreFinal: every period
// the previous record had closed is still recorded, unchanged.
func (r *Record) CheckClosedPeriodsFinal(previous *Record) error {
	if previous == nil {
		return nil
	}
	var problems []string
	refuse := func(table Table, subject string, p Period, what string) {
		problems = append(problems, (&Refusal{
			Rule:    RuleClosedPeriodsAreFinal,
			Subject: subject,
			Period:  p,
			Detail:  fmt.Sprintf("the closed %s entry %s was removed or changed", table, what),
			Fix:     fmt.Sprintf("Restore the [[%s]] entry as it was; a closed period is never changed or removed.", table),
		}).Error())
	}
	for _, old := range previous.lifecycle {
		if old.Open() {
			continue
		}
		kept := false
		for _, l := range r.lifecycle {
			if l.Subject == old.Subject && l.Status == old.Status && l.Reason == old.Reason && l.Period.equal(old.Period) {
				kept = true
				break
			}
		}
		if !kept {
			refuse(TableLifecycle, old.Subject, old.Period, fmt.Sprintf("(status %s)", old.Status))
		}
	}
	for _, old := range previous.licenses {
		if old.Open() {
			continue
		}
		kept := false
		for _, l := range r.licenses {
			if l.Subject == old.Subject && l.License == old.License && l.Reason == old.Reason && l.Period.equal(old.Period) {
				kept = true
				break
			}
		}
		if !kept {
			refuse(TableLicenses, old.Subject, old.Period, fmt.Sprintf("(license %s)", old.License))
		}
	}
	for _, old := range previous.identities {
		if old.Pending() || old.Open() {
			continue
		}
		kept := false
		for _, id := range r.identities {
			if identityEqual(id, old) {
				kept = true
				break
			}
		}
		if !kept {
			refuse(TableIdentities, old.Subject, old.Period, fmt.Sprintf("(%s %q)", old.Facet, old.Value))
		}
	}
	if len(problems) > 0 {
		return &AppendOnlyError{Problems: problems}
	}
	return nil
}

func identityEqual(a, b Identity) bool {
	if a.Subject != b.Subject || a.Facet != b.Facet || a.Value != b.Value ||
		a.Registry != b.Registry || !a.Period.equal(b.Period) ||
		a.EffectiveVersion != b.EffectiveVersion || a.Reason != b.Reason ||
		len(a.TagPatterns) != len(b.TagPatterns) {
		return false
	}
	for i := range a.TagPatterns {
		if a.TagPatterns[i] != b.TagPatterns[i] {
			return false
		}
	}
	return true
}

func registryNameEqual(a, b RegistryName) bool {
	return a.Registry == b.Registry && a.Name == b.Name && a.Subject == b.Subject &&
		a.RecordedSince.Equal(b.RecordedSince)
}
