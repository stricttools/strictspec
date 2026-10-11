package declarations_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

type V = declarations.Value
type M = map[string]declarations.Value

func liveServer(name string) declarations.Resource {
	return declarations.Resource{Type: "test-server", Name: name, Fields: M{"location": "fsn1", "labels": M{"role": "game"}, "status": "running"}}
}

func liveZone(name string, delegate bool, records ...declarations.Item) declarations.Resource {
	return declarations.Resource{Type: "test-zone", Name: name, Fields: M{"delegate": delegate}, Children: map[string][]declarations.Item{"record": records}}
}

// siteProject declares the zone voxlibre.org (an A record pointing at the
// server gamehome's ipv4 and a TXT record) and the server.
func siteProject(t *testing.T) declarations.Project {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, "", aRecord, txtRecord))
	declare(t, p, "test-server", "gamehome", serverBody)
	return p
}

// siteLive is the live state siteProject declares.
func siteLive() *fakeLive {
	l := newLive()
	l.put(liveServer("gamehome"))
	l.exposed["test-server/gamehome/ipv4"] = "203.0.113.7"
	l.put(liveZone("voxlibre.org", false, record("A", "voxlibre.org", "203.0.113.7", "automatic", "the site"), record("TXT", "voxlibre.org", "hello", int64(300), "")))
	l.exposed["test-zone/voxlibre.org/nameservers"] = []V{"a.ns.test", "b.ns.test"}
	return l
}

// claimAll claims every resource of the set for its project, in an account
// repository elsewhere.
func claimAll(t *testing.T, s declarations.Set) declarations.Claims {
	c := declarations.Claims{Root: t.TempDir(), Claims: []declarations.Claim{}, Unmanaged: []declarations.UnmanagedResource{}}
	for _, r := range s.Resources {
		c.Claims = append(c.Claims, declarations.Claim{ResourceType: r.Type, Name: r.Name, Project: s.Project.Name})
	}
	return c
}

func compare(t *testing.T, s declarations.Set, live declarations.LiveReader) declarations.Report {
	t.Helper()
	r, err := declarations.Compare(s, live, claimAll(t, s))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	validateReport(t, r)
	return r
}

// validateReport checks the report's JSON against the declaration-report
// schema.
func validateReport(t *testing.T, r declarations.Report) {
	t.Helper()
	src, err := os.ReadFile("schemas/declaration-report.schema.toml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := strictspec.CompileEmbedded(map[string]string{"r.toml": string(src)}, "r.toml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("report JSON: %v", err)
	}
	if res := p.Validate(b, "json"); !res.Valid {
		t.Fatalf("the report does not validate: %+v\n%s", res.Diagnostics, b)
	}
}

// reportJSON is the report's JSON decoded; lines renders one of its arrays
// one element per string, keys sorted.
func reportJSON(t *testing.T, r declarations.Report) map[string]any {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func lines(t *testing.T, r declarations.Report, array string) []string {
	t.Helper()
	out := []string{}
	for _, e := range reportJSON(t, r)[array].([]any) {
		b, _ := json.Marshal(e)
		out = append(out, string(b))
	}
	return out
}

func wantLines(t *testing.T, r declarations.Report, array string, want ...string) {
	t.Helper()
	got := lines(t, r, array)
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got  %s\n want %s", array, strings.Join(got, "\n      "), strings.Join(want, "\n      "))
	}
}

func TestCompareMatching(t *testing.T) {
	p := siteProject(t)
	r := compare(t, load(t, p), siteLive())
	wantLines(t, r, "differences")
	if !r.Matching || r.ExitCode() != declarations.ExitMatching {
		t.Fatalf("matching %v, exit %d", r.Matching, r.ExitCode())
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(r.DiffID) {
		t.Fatalf("diff id %q", r.DiffID)
	}
	m := reportJSON(t, r)
	want := map[string]any{"format_version": float64(1), "tool": "www", "project": "voxlibre", "root": p.Root, "directory": declDir, "diff_id": r.DiffID, "matching": true, "differences": []any{}, "unmanaged": []any{}, "unreadable": []any{}}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("report %v", m)
	}
}

func TestCompareMissingResource(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	delete(l.resources, "test-zone/voxlibre.org")
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":{"delegate":false},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"missing"}`,
		`{"collection":"record","declared":{"comment":"the site","ttl":"automatic"},"key":{"content":"203.0.113.7","name":"voxlibre.org","type":"A"},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"missing"}`,
		`{"collection":"record","declared":{"comment":"","ttl":300},"key":{"content":"hello","name":"voxlibre.org","type":"TXT"},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"missing"}`,
	)
	if r.Matching || r.ExitCode() != declarations.ExitDifferences {
		t.Fatalf("matching %v, exit %d", r.Matching, r.ExitCode())
	}
}

func TestCompareUnexpectedAndChanged(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	z := l.resources["test-zone/voxlibre.org"]
	z.Children["record"][1].Fields["ttl"] = int64(600)
	z.Children["record"] = append(z.Children["record"], record("CNAME", "www.voxlibre.org", "voxlibre.org", "automatic", "old"))
	z.Fields["delegate"] = true
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":false,"field":"delegate","live":true,"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"changed"}`,
		`{"collection":"record","declared":300,"field":"ttl","key":{"content":"hello","name":"voxlibre.org","type":"TXT"},"live":600,"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"changed"}`,
		`{"collection":"record","key":{"content":"voxlibre.org","name":"www.voxlibre.org","type":"CNAME"},"live":{"comment":"old","ttl":"automatic"},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"unexpected"}`,
	)
}

func TestCompareUnresolvedReference(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	delete(l.resources, "test-server/gamehome")
	delete(l.exposed, "test-server/gamehome/ipv4")
	z := l.resources["test-zone/voxlibre.org"]
	z.Children["record"] = z.Children["record"][1:]
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":{"image":"debian-13","labels":{"role":"game"},"location":"fsn1"},"name":"gamehome","resource_type":"test-server","source":"test-server/gamehome","status":"missing"}`,
		`{"collection":"record","declared":{"comment":"the site","ttl":"automatic"},"key":{"content":{"test_server":"gamehome","value":"ipv4"},"name":"voxlibre.org","type":"A"},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"unresolved"}`,
	)
}

func TestCompareDerivedChildren(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-mail", "voxlibre.org", mailBody)
	l := newLive()
	l.put(declarations.Resource{Type: "test-mail", Name: "voxlibre.org", Fields: M{"plan": "micro", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{"mailbox": {{Fields: M{"local_part": "me", "name": "Me"}}}}})
	l.put(liveZone("voxlibre.org", false,
		record("TXT", "voxlibre.org", "hello", int64(300), ""),
		record("MX", "voxlibre.org", "mx.mail.test", int64(300), ""),
		record("MX", "voxlibre.org", "mx2.mail.test", int64(300), "")))
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"collection":"record","declared":{"comment":"","ttl":"automatic"},"key":{"content":"k=1","name":"dkim._domainkey.voxlibre.org","type":"TXT"},"name":"voxlibre.org","resource_type":"test-zone","source":"test-mail/voxlibre.org","status":"missing"}`,
		`{"collection":"record","key":{"content":"mx2.mail.test","name":"voxlibre.org","type":"MX"},"live":{"comment":"","ttl":300},"name":"voxlibre.org","resource_type":"test-zone","source":"test-mail/voxlibre.org","status":"unexpected"}`,
	)
}

func TestCompareUnresolvedDerivationLeavesCoveredChildrenAlone(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-mail", "voxlibre.org", mailBody)
	l := newLive()
	l.put(liveZone("voxlibre.org", false,
		record("TXT", "voxlibre.org", "hello", int64(300), ""),
		record("MX", "voxlibre.org", "mx.old.test", int64(300), "")))
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":{"plan":"micro"},"name":"voxlibre.org","resource_type":"test-mail","source":"test-mail/voxlibre.org","status":"missing"}`,
		`{"collection":"mailbox","declared":{"invitation":"me@elsewhere.test","name":"Me"},"key":{"local_part":"me"},"name":"voxlibre.org","resource_type":"test-mail","source":"test-mail/voxlibre.org","status":"missing"}`,
		`{"collection":"record","declared":"the mail domain voxlibre.org is not live yet","name":"voxlibre.org","resource_type":"test-zone","source":"test-mail/voxlibre.org","status":"unresolved"}`,
		`{"collection":"record","declared":"the mail domain voxlibre.org is not live yet","name":"voxlibre.org","resource_type":"test-zone","source":"test-mail/voxlibre.org","status":"unresolved"}`,
	)
}

func TestCompareDerivedField(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(true, ""))
	declare(t, p, "test-domain", "voxlibre.org", "auto_renew = true\nunmanaged = []\n")
	l := newLive()
	l.put(liveZone("voxlibre.org", true))
	l.exposed["test-zone/voxlibre.org/nameservers"] = []V{"a.ns.test", "b.ns.test"}
	l.put(declarations.Resource{Type: "test-domain", Name: "voxlibre.org", Fields: M{"auto_renew": true, "nameservers": []V{"dns1.registrar.test"}}})
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":["a.ns.test","b.ns.test"],"field":"nameservers","live":["dns1.registrar.test"],"name":"voxlibre.org","resource_type":"test-domain","source":"test-zone/voxlibre.org","status":"changed"}`,
	)

	delete(l.resources, "test-zone/voxlibre.org")
	delete(l.exposed, "test-zone/voxlibre.org/nameservers")
	r = compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":"the zone voxlibre.org is not live yet","field":"nameservers","live":["dns1.registrar.test"],"name":"voxlibre.org","resource_type":"test-domain","source":"test-zone/voxlibre.org","status":"unresolved"}`,
		`{"declared":{"delegate":true},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"missing"}`,
	)

	// A missing resource whose create waits on an unresolved value is
	// unresolved as a whole.
	delete(l.resources, "test-domain/voxlibre.org")
	r = compare(t, load(t, p), l)
	wantLines(t, r, "differences",
		`{"declared":{"auto_renew":true,"nameservers":"the zone voxlibre.org is not live yet"},"name":"voxlibre.org","resource_type":"test-domain","source":"test-domain/voxlibre.org","status":"unresolved"}`,
		`{"declared":{"delegate":true},"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"missing"}`,
	)
}

func TestCompareUnmanagedChildren(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, `{ collection = "record", match = { type = "CNAME" }, reason = "the old site, removed by hand" }`, txtRecord))
	l := newLive()
	l.put(liveZone("voxlibre.org", false,
		record("TXT", "voxlibre.org", "hello", int64(300), ""),
		record("CNAME", "old.voxlibre.org", "voxlibre.org", "automatic", "")))
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences")
	wantLines(t, r, "unmanaged",
		`{"collection":"record","key":{"content":"voxlibre.org","name":"old.voxlibre.org","type":"CNAME"},"name":"voxlibre.org","reason":"the old site, removed by hand","resource_type":"test-zone"}`)
	if !r.Matching {
		t.Fatal("unmanaged children are not differences")
	}
}

func TestCompareUnreadable(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	l.readErr["test-server/gamehome"] = errors.New("401 unauthorized")
	r := compare(t, load(t, p), l)
	wantLines(t, r, "differences")
	wantLines(t, r, "unreadable", `{"error":"401 unauthorized","name":"gamehome","resource_type":"test-server"}`)
	if r.Matching || r.ExitCode() != declarations.ExitFailed {
		t.Fatalf("matching %v, exit %d", r.Matching, r.ExitCode())
	}
}

func TestCompareRefusesWhatALiveReaderGotWrong(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	l.resources["test-server/gamehome"].Fields["location"] = 7
	_, err := declarations.Compare(load(t, p), l, claimAll(t, load(t, p)))
	mustContain(t, err, "test-server/gamehome", "location", "int")
}

func TestExitCodeOrder(t *testing.T) {
	r := declarations.Report{}
	if r.ExitCode() != declarations.ExitMatching {
		t.Fatal("an empty report is matching")
	}
	r.Differences = []declarations.Difference{{Status: declarations.StatusChanged}}
	if r.ExitCode() != declarations.ExitDifferences {
		t.Fatal("differences exit 2")
	}
	r.Unreadable = []declarations.Unreadable{{ResourceType: "test-zone", Error: "e"}}
	if r.ExitCode() != declarations.ExitFailed {
		t.Fatal("an unreadable resource exits 1, differences or not")
	}
}

func TestDiffIDIsStableAndChangesOnADeclarationEdit(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	a := compare(t, load(t, p), l)
	if b := compare(t, load(t, p), l); b.DiffID != a.DiffID {
		t.Fatal("the diff id is not stable")
	}
	declare(t, p, "test-server", "gamehome", "# a comment\n"+serverBody)
	c := compare(t, load(t, p), l)
	if c.DiffID == a.DiffID {
		t.Fatal("editing a declaration file did not change the diff id")
	}
	if !reflect.DeepEqual(c.Differences, a.Differences) {
		t.Fatal("a comment changed the differences")
	}
}

func TestDiffIDChangesOnALiveChange(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	a := compare(t, load(t, p), l)
	l.resources["test-server/gamehome"].Fields["status"] = "off"
	b := compare(t, load(t, p), l)
	if b.DiffID == a.DiffID {
		t.Fatal("a live change did not change the diff id")
	}
	if len(b.Differences) != 0 {
		t.Fatalf("a field the descriptor does not compare is a difference: %+v", b.Differences)
	}
}

func TestDiffIDCoversWhatADerivationReads(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, ""))
	declare(t, p, "test-forward", "spf", forwardBody("voxlibre.org", "TXT", "v=spf1", ""))
	l := newLive()
	l.put(liveZone("voxlibre.org", false, record("TXT", "voxlibre.org", "v=spf1 forward", "automatic", "")))
	l.put(declarations.Resource{Type: "test-forward", Name: "spf", Fields: M{"zone": "voxlibre.org", "record_type": "TXT", "prefix": "v=spf1"}})
	l.put(liveServer("relay"))
	a := compare(t, load(t, p), l)
	wantLines(t, a, "differences")
	l.resources["test-server/relay"].Fields["status"] = "off"
	if b := compare(t, load(t, p), l); b.DiffID == a.DiffID {
		t.Fatal("a change in what a derivation read did not change the diff id")
	}
}

func TestCompareSortsDifferences(t *testing.T) {
	p := siteProject(t)
	declare(t, p, "test-domain", "voxlibre.org", domainBody)
	l := newLive()
	r := compare(t, load(t, p), l)
	var got []string
	for _, d := range r.Differences {
		got = append(got, d.ResourceType+" "+d.Collection)
	}
	want := []string{"test-domain ", "test-server ", "test-zone ", "test-zone record", "test-zone record"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %q", got)
	}
	if r.Differences[3].Key["type"] != "TXT" || r.Differences[4].Status != declarations.StatusUnresolved {
		t.Fatalf("children sorted by key: %+v", r.Differences[3:])
	}
}

func TestCompareReportsMisclaimedResources(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := declarations.Claims{Root: t.TempDir(), Claims: []declarations.Claim{
		{ResourceType: "test-zone", Name: "voxlibre.org", Project: "other"},
	}}
	r, err := declarations.Compare(s, siteLive(), claims)
	if err != nil {
		t.Fatal(err)
	}
	validateReport(t, r)
	wantLines(t, r, "differences",
		`{"name":"gamehome","resource_type":"test-server","source":"test-server/gamehome","status":"misclaimed"}`,
		`{"name":"voxlibre.org","resource_type":"test-zone","source":"test-zone/voxlibre.org","status":"misclaimed"}`,
	)
}

func TestCompareRefusesClaimsOutsideTheAccountRepositoryAndRemovingThemClearsIt(t *testing.T) {
	p := siteProject(t)
	writeFile(t, p.Root, claimsPath, "format_version = 1\nclaim = []\nunmanaged = []\n")
	s := load(t, p)
	claims := claimAll(t, s)
	_, err := declarations.Compare(s, siteLive(), claims)
	mustContain(t, err, "claims.toml", claims.Root, "move")
	removeFile(t, p.Root, claimsPath)
	if _, err := declarations.Compare(load(t, p), siteLive(), claims); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestCompareInTheAccountRepository(t *testing.T) {
	a := newAccount(t, `unmanaged = [{ resource_type = "test-server", name = "old", reason = "a manual experiment" }]
claim = [{ resource_type = "test-zone", name = "voxlibre.org", project = "voxlibre" }]
`)
	declare(t, a, "test-server", "backup", serverBody)
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	l := siteLive()
	l.put(liveServer("backup"))
	l.put(liveServer("old"))
	l.listErr["test-mail"] = errors.New("503 unavailable")
	r, err := declarations.Compare(load(t, a), l, claims)
	if err != nil {
		t.Fatal(err)
	}
	validateReport(t, r)
	wantLines(t, r, "differences",
		`{"name":"gamehome","resource_type":"test-server","status":"unclaimed"}`)
	wantLines(t, r, "unmanaged",
		`{"name":"old","reason":"a manual experiment","resource_type":"test-server"}`)
	wantLines(t, r, "unreadable",
		`{"error":"503 unavailable","resource_type":"test-mail"}`)
}

func TestCompareRefusesAClaimOfTheAccountRepositorysOwnResourceAndRemovingItClearsIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"backup\", project = \"voxlibre\" }]\n")
	declare(t, a, "test-server", "backup", serverBody)
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("backup"))
	_, err = declarations.Compare(load(t, a), l, claims)
	mustContain(t, err, "test-server/backup", "claim[0]", "test-server/backup.toml")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nunmanaged = []\nclaim = []\n")
	claims, err = declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := declarations.Compare(load(t, a), l, claims); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestCompareReportsAFailedExposedReadAsUnreadable(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	l.exposedErr["test-server/gamehome/ipv4"] = errors.New("500 internal error")
	r := compare(t, load(t, p), l)
	wantLines(t, r, "unreadable", `{"error":"500 internal error","name":"gamehome","resource_type":"test-server"}`)
	wantLines(t, r, "differences")
	if r.ExitCode() != declarations.ExitFailed {
		t.Fatalf("exit %d", r.ExitCode())
	}
}

func TestCompareReportsAFailedDerivationUnderItsCauseAndLeavesItsChildrenAlone(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, ""))
	declare(t, p, "test-forward", "spf", forwardBody("voxlibre.org", "TXT", "v=spf1", ""))
	l := newLive()
	l.put(liveZone("voxlibre.org", false, record("TXT", "voxlibre.org", "v=spf1 old", "automatic", "")))
	l.put(declarations.Resource{Type: "test-forward", Name: "spf", Fields: M{"zone": "voxlibre.org", "record_type": "TXT", "prefix": "v=spf1"}})
	l.readErr["test-server/relay"] = errors.New("403 forbidden")
	r := compare(t, load(t, p), l)
	wantLines(t, r, "unreadable", `{"error":"403 forbidden","name":"spf","resource_type":"test-forward"}`)
	wantLines(t, r, "differences")
}

// strayMail derives a record its slot does not cover.
type strayMail struct{ mailType }

func (strayMail) Derive(r declarations.Resource, live declarations.LiveReader) ([]declarations.Derived, error) {
	slots := mailType{}.Slots(r)
	return []declarations.Derived{
		{Slot: slots[0], Items: []declarations.Item{record("A", r.Name, "203.0.113.9", "automatic", "")}},
		{Slot: slots[1], Unresolved: "later"},
	}, nil
}

func TestCompareRefusesADerivationOutsideItsSlot(t *testing.T) {
	tool := testTool()
	tool.ResourceTypes = []declarations.ResourceType{zoneType{}, strayMail{}}
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(false, ""))
	declare(t, p, "test-mail", "voxlibre.org", mailBody)
	s, err := declarations.Load(tool, p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = declarations.Compare(s, newLive(), claimAll(t, s))
	mustContain(t, err, "test-mail/voxlibre.org.toml", "does not match", "a bug in the tool")
}
