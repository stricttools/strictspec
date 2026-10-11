package declarations_test

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

// plan makes the plan of an apply of s against l, every resource claimed for
// its project, with nothing uncommitted.
func plan(t *testing.T, s declarations.Set, l declarations.LiveReader) declarations.Plan {
	t.Helper()
	p, err := declarations.PlanApply(s, l, claimAll(t, s), &fakeGit{})
	if err != nil {
		t.Fatalf("PlanApply: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(p.ID) {
		t.Fatalf("plan id %q", p.ID)
	}
	return p
}

func TestPlanIDIsStableAndChangesOnADeclarationEdit(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	a := plan(t, load(t, p), l)
	if b := plan(t, load(t, p), l); b.ID != a.ID {
		t.Fatal("the plan id is not stable")
	}
	declare(t, p, "test-server", "web1", "# a comment\n"+serverBody)
	c := plan(t, load(t, p), l)
	if c.ID == a.ID {
		t.Fatal("editing a declaration file did not change the plan id")
	}
	if !reflect.DeepEqual(c.Report.Differences, a.Report.Differences) {
		t.Fatal("a comment changed the differences")
	}
}

func TestPlanIDChangesOnALiveChange(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	a := plan(t, load(t, p), l)
	l.resources["test-server/web1"].Fields["status"] = "off"
	b := plan(t, load(t, p), l)
	if b.ID == a.ID {
		t.Fatal("a live change did not change the plan id")
	}
	if len(b.Report.Differences) != 0 {
		t.Fatalf("a field the descriptor does not compare is a difference: %+v", b.Report.Differences)
	}
}

func TestPlanIDCoversWhatADerivationReads(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, ""))
	declare(t, p, "test-forward", "spf", forwardBody("example.org", "TXT", "v=spf1", ""))
	l := newLive()
	l.put(liveZone("example.org", false, record("TXT", "example.org", "v=spf1 forward", "automatic", "")))
	l.put(declarations.Resource{Type: "test-forward", Name: "spf", Fields: M{"zone": "example.org", "record_type": "TXT", "prefix": "v=spf1"}})
	l.put(liveServer("relay"))
	a := plan(t, load(t, p), l)
	wantLines(t, a.Report, "differences")
	l.resources["test-server/relay"].Fields["status"] = "off"
	if b := plan(t, load(t, p), l); b.ID == a.ID {
		t.Fatal("a change in what a derivation read did not change the plan id")
	}
}
