package declarations_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

func verify(t *testing.T, s declarations.Set, live declarations.LiveReader, claims declarations.Claims) declarations.Report {
	t.Helper()
	r, err := declarations.Compare(s, live, claims)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	return r
}

// describe renders a change as "<action> <type>/<name>[ <collection> <key>][ <field>]".
func describe(c declarations.Change) string {
	s := string(c.Action) + " " + c.ResourceType + "/" + c.Name
	if c.Collection != "" {
		var parts []string
		for _, k := range sortedKeys(c.Key) {
			parts = append(parts, fmt.Sprint(c.Key[k]))
		}
		s += " " + c.Collection + " " + strings.Join(parts, ",")
	}
	if c.Field != "" {
		s += " " + c.Field
	}
	return s
}

func sortedKeys(m map[string]declarations.Value) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func describeAll(cs []declarations.Change) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, describe(c))
	}
	return out
}

// stagedProject declares a zone (with an A record pointing at a server's
// ipv4, delegating the domain's nameservers), the server, a mail domain
// deriving records in the zone, and the domain.
func stagedProject(t *testing.T) declarations.Project {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, "", aRecord, txtRecord))
	declare(t, p, "test-server", "web1", serverBody)
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	return p
}

func TestApplyStagesCreatesThroughReferencesAndDerivations(t *testing.T) {
	p := stagedProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	w := &fakeWriter{live: l}
	g := &fakeGit{}

	first := verify(t, s, l, claims)
	after, err := declarations.Apply(s, l, claims, w, g, first.DiffID)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{
		"create test-zone/example.org",
		"create test-zone/example.org record hello,example.org,TXT",
		"create test-mail/example.org",
		"create test-mail/example.org mailbox me",
		"create test-server/web1",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("first apply:\n got  %q\n want %q", got, want)
	}
	if !reflect.DeepEqual(g.calls, [][2]string{{p.Root, declDir}}) {
		t.Fatalf("git status calls %v", g.calls)
	}
	create := w.changes[0]
	if create.Stage != 20 || create.Resource.Fields["delegate"] != true || len(create.Resource.Children["record"]) != 1 {
		t.Fatalf("a resource create carries the full expected resource without what is unresolved: %+v", create)
	}
	if w.changes[3].Item.Fields["invitation"] != "me@elsewhere.test" {
		t.Fatalf("an item create carries the expected item, create-only fields included: %+v", w.changes[3].Item)
	}

	// The servers and zones this apply created resolve in its report: the
	// next apply executes what they unblocked.
	w.changes = nil
	if after.Matching {
		t.Fatal("the staged apply's report must show the next stage")
	}
	if _, err := declarations.Apply(s, l, claims, w, g, after.DiffID); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	want = []string{
		"create test-zone/example.org record ,dkim._domainkey.example.org,TXT",
		"create test-zone/example.org record 203.0.113.1,example.org,A",
		"create test-zone/example.org record mx.mail.test,example.org,MX",
		"create test-domain/example.org",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("second apply:\n got  %q\n want %q", got, want)
	}
	if w.changes[1].Item.Fields["content"] != "203.0.113.1" || w.changes[1].Resource.Children["record"][0].Fields["content"] != "203.0.113.1" {
		t.Fatalf("references are resolved in a change: %+v", w.changes[1])
	}
	if !reflect.DeepEqual(w.changes[3].Resource.Fields["nameservers"], []declarations.Value{"a.ns.test", "b.ns.test"}) {
		t.Fatalf("derived fields are in the expected resource: %+v", w.changes[3].Resource)
	}
	final := verify(t, s, l, claims)
	if !final.Matching {
		t.Fatalf("after two applies: %+v", final.Differences)
	}
}

func TestApplyUpdatesThenDeletesInDescendingStage(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-mail", "example.org", "plan = \"micro\"\nunmanaged = []\nmailbox = []\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(declarations.Resource{Type: "test-mail", Name: "example.org", Fields: M{"plan": "mini", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{"mailbox": {{Fields: M{"local_part": "old", "name": "Old"}}}}})
	l.put(liveZone("example.org", false,
		record("TXT", "example.org", "hello", int64(600), ""),
		record("CNAME", "www.example.org", "example.org", "automatic", ""),
		record("MX", "example.org", "mx.mail.test", int64(300), ""),
		record("TXT", "dkim._domainkey.example.org", "k=1", "automatic", "")))
	w := &fakeWriter{live: l}
	r := verify(t, s, l, claims)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"update test-zone/example.org record hello,example.org,TXT ttl",
		"update test-mail/example.org plan",
		"delete test-mail/example.org mailbox old",
		"delete test-zone/example.org record example.org,www.example.org,CNAME",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	if w.changes[2].Item.Fields["name"] != "Old" || w.changes[0].Item.Fields["ttl"] != int64(300) {
		t.Fatalf("a delete carries the live item, an update the expected one: %+v", w.changes)
	}
	if !verify(t, s, l, claims).Matching {
		t.Fatal("not matching after apply")
	}
}

func TestApplyRefusesUncommittedFilesAndCommittingClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	delete(l.resources, "test-zone/example.org")
	r := verify(t, s, l, claims)
	g := &fakeGit{status: " M " + declDir + "/test-zone/example.org.toml\n?? " + declDir + "/test-server/backup.toml\n"}
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, g, r.DiffID)
	mustContain(t, err, "uncommitted or untracked", declDir+"/test-zone/example.org.toml", declDir+"/test-server/backup.toml", "commit them")
	if len(w.changes) != 0 {
		t.Fatal("a refused apply wrote")
	}
	g.status = ""
	if _, err := declarations.Apply(s, l, claims, w, g, r.DiffID); err != nil {
		t.Fatalf("after committing: %v", err)
	}
}

func TestApplyRefusesAStaleDiffAndTheNewDiffClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	old := verify(t, s, l, claims)
	l.resources["test-zone/example.org"].Children["record"][1].Fields["comment"] = "edited by hand"
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, old.DiffID)
	mustContain(t, err, "the live state or the declarations changed since verify printed "+old.DiffID+": run verify again and pass its new diff")
	if len(w.changes) != 0 {
		t.Fatal("a refused apply wrote")
	}
	fresh := verify(t, s, l, claims)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, fresh.DiffID); err != nil {
		t.Fatalf("with the new diff: %v", err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, []string{"update test-zone/example.org record hello,example.org,TXT comment"}) {
		t.Fatalf("changes %q", got)
	}
}

func TestApplyRefusesAStaleDiffAfterADeclarationEdit(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	s := load(t, p)
	claims := claimAll(t, s)
	old := verify(t, s, l, claims)
	declare(t, p, "test-server", "web1", "# edited\n"+serverBody)
	_, err := declarations.Apply(load(t, p), l, claims, &fakeWriter{live: l}, &fakeGit{}, old.DiffID)
	mustContain(t, err, "changed since verify printed "+old.DiffID)
}

func TestApplyRefusesWhileMisclaimedAndFixingTheClaimsClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	l := siteLive()
	claims := declarations.Claims{Root: t.TempDir(), Claims: []declarations.Claim{{ResourceType: "test-server", Name: "web1", Project: "example"}, {ResourceType: "test-zone", Name: "example.org", Project: "other"}}}
	r := verify(t, s, l, claims)
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID)
	mustContain(t, err, "misclaimed", "test-zone/example.org", "claims.toml")
	claims.Claims[1].Project = "example"
	r = verify(t, s, l, claims)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatalf("after fixing the claims: %v", err)
	}
}

func TestApplyRefusesWhileAReadFailsAndARetryClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	l.readErr["test-server/web1"] = errors.New("429 rate limited")
	r := verify(t, s, l, claims)
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID)
	mustContain(t, err, "test-server/web1", "429 rate limited", "run verify again")
	delete(l.readErr, "test-server/web1")
	r = verify(t, s, l, claims)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatalf("once the read succeeds: %v", err)
	}
}

func TestApplyStopsAtTheFirstWriterFailure(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	delete(l.resources, "test-zone/example.org")
	r := verify(t, s, l, claims)
	w := &fakeWriter{live: l, failOn: 2}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID)
	mustContain(t, err, "the provider refused", "test-zone/example.org", "1 change", "run verify again")
	if len(w.changes) != 2 {
		t.Fatalf("apply went on after a failure: %q", describeAll(w.changes))
	}
}

// In the account repository, apply deletes each unclaimed resource: a
// resource-level delete, after every create and update, in descending Stage
// with the child deletes. Claimed and unmanaged resources are left alone.
func TestApplyDeletesUnclaimedResourcesInTheAccountRepository(t *testing.T) {
	a := newAccount(t, `unmanaged = [{ resource_type = "test-server", name = "old", reason = "a manual experiment" }]

[[claim]]
resource_type = "test-server"
name = "web1"
project = "example"
`)
	declare(t, a, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	s := load(t, a)
	l := newLive()
	l.put(liveZone("example.org", false, record("CNAME", "www.example.org", "example.org", "automatic", "")))
	l.put(liveServer("web1"))
	l.put(liveServer("old"))
	l.put(liveServer("stray"))
	l.put(declarations.Resource{Type: "test-mail", Name: "other.org", Fields: M{"plan": "mini"}})
	r := verify(t, s, l, claims)
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create test-zone/example.org record hello,example.org,TXT",
		"delete test-server/stray",
		"delete test-mail/other.org",
		"delete test-zone/example.org record example.org,www.example.org,CNAME",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	del := w.changes[1]
	if del.Stage != 40 || del.Resource.Type != "test-server" || del.Resource.Name != "stray" {
		t.Fatalf("a resource delete carries its type's stage and the resource's type and name: %+v", del)
	}
	if !verify(t, s, l, claims).Matching {
		t.Fatal("not matching after apply")
	}
}

// In a project, apply never deletes a top-level resource: a live resource the
// project does not declare is not its difference (the account repository
// reports it unclaimed).
func TestApplyInAProjectNeverDeletesAResource(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	l.put(liveServer("stray"))
	delete(l.resources, "test-zone/example.org")
	r := verify(t, s, l, claims)
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.changes {
		if c.Action == declarations.ActionDelete {
			t.Fatalf("a project apply deleted: %q", describeAll(w.changes))
		}
	}
	if _, ok := l.resources["test-server/stray"]; !ok {
		t.Fatal("the undeclared server is gone")
	}
}

// A rule keyed by port whose source waits on a server that is not live yet,
// while its description changed: the description's update carries the rule
// with the live source in place of the unresolved one, and the expected
// resource holds that rule too.
func TestApplyUpdateOfAChildWithAnUnresolvedFieldCarriesTheLiveValue(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-server", "web1", serverBody)
	declare(t, p, "test-firewall", "edge", "unmanaged = []\n[[rule]]\nport = 22\nsource = { test_server = \"web1\", value = \"ipv4\" }\ndescription = \"new\"\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(declarations.Resource{Type: "test-firewall", Name: "edge", Children: map[string][]declarations.Item{"rule": {{Fields: M{"port": int64(22), "source": "198.51.100.1", "description": "old"}}}}})
	w := &fakeWriter{live: l}
	r := verify(t, s, l, claims)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, r.DiffID); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create test-server/web1",
		"update test-firewall/edge rule 22 description",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	wantItem := declarations.Item{Fields: M{"port": int64(22), "source": "198.51.100.1", "description": "new"}}
	if !reflect.DeepEqual(w.changes[1].Item, wantItem) {
		t.Fatalf("the update carries %+v; want %+v", w.changes[1].Item, wantItem)
	}
	if got := w.changes[1].Resource.Children["rule"]; !reflect.DeepEqual(got, []declarations.Item{wantItem}) {
		t.Fatalf("the expected resource's rules are %+v; want %+v", got, []declarations.Item{wantItem})
	}
}
