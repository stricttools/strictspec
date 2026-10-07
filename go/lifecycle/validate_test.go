package lifecycle_test

import (
	"strings"
	"testing"
)

func TestValidateAcceptsTheSharedRecordWhileConfidential(t *testing.T) {
	r := parse(t, sharedRecord)
	if err := r.Validate(day(t, "2026-12-01"), []string{"portal", "widget"}); err != nil {
		t.Fatal(err)
	}
}

func TestCodenamesAndTermsAreRefusedWhilePublic(t *testing.T) {
	r := parse(t, sharedRecord)
	err := r.Validate(day(t, "2026-05-01"), []string{"portal", "widget"})
	if err == nil {
		t.Fatal("codenames accepted while the repository is public")
	}
	for _, want := range []string{"codenames (Bluebird)", "distinctive_terms (hyperlattice)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q: %v", want, err)
		}
	}
}

func TestRemovingTheTermsClearsThePublicRefusal(t *testing.T) {
	r := parse(t, sharedRecord)
	if err := r.ClearConfidentialTerms(); err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(day(t, "2026-05-01"), []string{"portal", "widget"}); err != nil {
		t.Fatalf("the refusal did not clear: %v", err)
	}
}

func TestAnUndeclaredSubjectOfAnOpenPeriodIsRefused(t *testing.T) {
	r := parse(t, sharedRecord)
	err := r.Validate(day(t, "2026-12-01"), []string{"portal"})
	if err == nil || !strings.Contains(err.Error(), `subject "widget"`) {
		t.Fatalf("want a refusal naming widget, got %v", err)
	}
	// The fix the refusal names: declare it.
	if err := r.Validate(day(t, "2026-12-01"), []string{"portal", "widget"}); err != nil {
		t.Fatalf("declaring the subject did not clear the refusal: %v", err)
	}
}

func TestClosedPeriodsMayNameUndeclaredSubjects(t *testing.T) {
	r := parse(t, `format_version = 1
[[lifecycle]]
subject = "oldportal"
status = "active"
from = 2026-01-01
until = 2026-03-01
reason = "renamed away"
[[identities]]
subject = "oldportal"
facet = "releasable-name"
value = "oldportal"
registry = ""
tag_patterns = ["v*"]
from = 2026-01-01
until = 2026-03-01
reason = "first name"
[[registry_names]]
registry = "npm"
name = "oldportal"
subject = "oldportal"
recorded_since = 2026-01-01
`)
	if err := r.Validate(day(t, "2026-10-07"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestARetiredSubjectNeedNotBeDeclared(t *testing.T) {
	r := parse(t, `format_version = 1
[[lifecycle]]
subject = "gadget"
status = "retired"
from = 2026-03-01
reason = "replaced"
[[licenses]]
subject = "gadget"
license = "MIT"
from = 2026-01-01
reason = "initial"
`)
	if err := r.Validate(day(t, "2026-10-07"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAPendingIdentityOfAnUndeclaredSubjectIsRefused(t *testing.T) {
	r := parse(t, `format_version = 1
[[identities]]
subject = "portal"
facet = "package-name"
value = "portal-client"
registry = "npm"
tag_patterns = ["v*"]
effective_version = "0.5.0"
reason = "renamed"
`)
	err := r.Validate(day(t, "2026-10-07"), nil)
	if err == nil || !strings.Contains(err.Error(), "a pending package-name identity") {
		t.Fatalf("want a refusal naming the pending identity, got %v", err)
	}
	if err := r.Validate(day(t, "2026-10-07"), []string{"portal"}); err != nil {
		t.Fatalf("declaring the subject did not clear the refusal: %v", err)
	}
}

func TestTheMinimalRecordHasNoSubjectsAndIsValid(t *testing.T) {
	r := parse(t, "format_version = 1\n")
	if err := r.Validate(day(t, "2026-10-07"), nil); err != nil {
		t.Fatal(err)
	}
	if r.Confidential(day(t, "2026-10-07")) {
		t.Fatal("the minimal record is confidential")
	}
}
