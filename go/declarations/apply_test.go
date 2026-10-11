package declarations_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

// planID is the identifier of the plan of an apply of s against live, with
// nothing uncommitted.
func planID(t *testing.T, s declarations.Set, live declarations.LiveReader, claims declarations.Claims) string {
	t.Helper()
	p, err := declarations.PlanApply(s, live, claims, &fakeWriter{}, &fakeGit{})
	if err != nil {
		t.Fatalf("PlanApply: %v", err)
	}
	return p.ID
}

// noPlan is an identifier no plan has, for an apply refused before it
// compares identifiers.
var noPlan = strings.Repeat("0", 64)

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

// One apply runs every step: a value an earlier change creates (the new
// server's ipv4, the nameservers of the new zone, the records of the new
// mail domain) resolves during the run, each change waiting on one runs
// after the creation it waits on, and the report after it matches.
func TestApplyRunsEveryStepResolvingWhatEarlierStepsCreate(t *testing.T) {
	p := stagedProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	w := &fakeWriter{live: l}
	g := &fakeGit{}

	plan, err := declarations.PlanApply(s, l, claims, w, g)
	if err != nil {
		t.Fatalf("PlanApply: %v", err)
	}
	wantPlan := []string{
		"create test-zone/example.org",
		"create test-zone/example.org record hello,example.org,TXT",
		"create test-mail/example.org",
		"deferred test-zone/example.org record from test-mail/example.org",
		"deferred test-zone/example.org record from test-mail/example.org",
		"create test-mail/example.org mailbox me",
		"create test-server/web1",
		"create test-zone/example.org record <ipv4 of new test-server web1>,example.org,A",
		"create test-domain/example.org",
	}
	var got []string
	for _, ch := range plan.Changes {
		if ch.Deferred != nil {
			got = append(got, "deferred "+ch.ResourceType+"/"+ch.Name+" "+ch.Collection+" from "+ch.Deferred.Source)
			continue
		}
		got = append(got, describe(ch.WithPlaceholders()))
	}
	if !reflect.DeepEqual(got, wantPlan) {
		t.Fatalf("plan:\n got  %q\n want %q", got, wantPlan)
	}
	if ns := plan.Changes[8].WithPlaceholders().Resource.Fields["nameservers"]; ns != "<nameservers test-zone example.org derives>" {
		t.Fatalf("the domain's create shows its nameservers by their placeholder: %v", ns)
	}
	if d := plan.Changes[3].Deferred; d.Why != "the mail domain example.org is not live yet" || !reflect.DeepEqual(d.After, []string{"test-mail/example.org", "test-zone/example.org"}) {
		t.Fatalf("a deferred change says why and what it waits on: %+v", d)
	}

	after, err := declarations.Apply(s, l, claims, w, g, plan.ID)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{
		"create test-zone/example.org",
		"create test-zone/example.org record hello,example.org,TXT",
		"create test-mail/example.org",
		"create test-zone/example.org record mx.mail.test,example.org,MX",
		"create test-zone/example.org record ,dkim._domainkey.example.org,TXT",
		"create test-mail/example.org mailbox me",
		"create test-server/web1",
		"create test-zone/example.org record 203.0.113.1,example.org,A",
		"create test-domain/example.org",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	create := w.changes[0]
	if create.Stage != 20 || create.Resource.Fields["delegate"] != true || len(create.Resource.Children["record"]) != 1 {
		t.Fatalf("a resource create carries the expected resource without what waits on a creation that has not run: %+v", create)
	}
	if w.changes[5].Item.Fields["invitation"] != "me@elsewhere.test" {
		t.Fatalf("an item create carries the expected item, create-only fields included: %+v", w.changes[5].Item)
	}
	if a := w.changes[7]; a.Item.Fields["content"] != "203.0.113.1" || a.Key["content"] != "203.0.113.1" || len(a.Resource.Children["record"]) != 2 {
		t.Fatalf("a reference to a value created earlier in the run is resolved in the change: %+v", a)
	}
	if !reflect.DeepEqual(w.changes[8].Resource.Fields["nameservers"], []declarations.Value{"a.ns.test", "b.ns.test"}) {
		t.Fatalf("a derived field created earlier in the run is in the expected resource: %+v", w.changes[8].Resource)
	}
	if !after.Matching {
		t.Fatalf("after one apply: %+v", after.Differences)
	}
}

// A deferred change makes the live children its slot covers match the
// derivation: it keeps a matching one, updates one whose fields differ, and
// deletes one the derivation does not give, each listed in the plan as
// covered.
func TestApplyDeferredChildrenReplaceTheCoveredLiveChildren(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-mail", "example.org", "plan = \"micro\"\nunmanaged = []\nmailbox = []\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(liveZone("example.org", false,
		record("TXT", "example.org", "hello", int64(300), ""),
		record("MX", "example.org", "mx.mail.test", int64(600), ""),
		record("MX", "example.org", "mx.old.test", int64(300), "")))
	w := &fakeWriter{live: l, created: map[string]declarations.Resource{"test-mail/example.org": {Fields: M{"dkim_key": "k=2"}}}}
	plan, err := declarations.PlanApply(s, l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	var covered []string
	for _, ch := range plan.Changes {
		if ch.Deferred != nil {
			for _, it := range ch.Deferred.Covered {
				covered = append(covered, fmt.Sprint(it.Fields["content"]))
			}
		}
	}
	sort.Strings(covered)
	if !reflect.DeepEqual(covered, []string{"mx.mail.test", "mx.old.test"}) {
		t.Fatalf("the plan lists the covered live children: %q", covered)
	}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, plan.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create test-mail/example.org",
		"update test-zone/example.org record mx.mail.test,example.org,MX ttl",
		"delete test-zone/example.org record mx.old.test,example.org,MX",
		"create test-zone/example.org record k=2,dkim._domainkey.example.org,TXT",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	if !verify(t, s, l, claims).Matching {
		t.Fatal("not matching after apply")
	}
}

// The plan's identifier covers its placeholders: a declaration edit to a
// value waiting on a creation changes it.
func TestPlanIdentifierCoversPendingValues(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	before := planID(t, s, l, claims)
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", strings.Replace(aRecord, "ipv4", "ipv6", 1), txtRecord))
	if _, err := declarations.Load(testTool(), p); err == nil {
		t.Fatal("a reference to a value the server does not expose loaded")
	}
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", strings.Replace(aRecord, "\"the site\"", "\"the new site\"", 1), txtRecord))
	if after := planID(t, load(t, p), l, claims); after == before {
		t.Fatal("the identifier did not change with the plan")
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
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
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
	g := &fakeGit{status: " M " + declDir + "/test-zone/example.org.toml\n?? " + declDir + "/test-server/backup.toml\n"}
	w := &fakeWriter{live: l}
	id := planID(t, s, l, claims)
	_, err := declarations.Apply(s, l, claims, w, g, id)
	mustContain(t, err, "uncommitted or untracked", declDir+"/test-zone/example.org.toml", declDir+"/test-server/backup.toml", "commit them, then run infratool manifest apply --dry-run")
	if _, err := declarations.PlanApply(s, l, claims, w, g); err == nil {
		t.Fatal("a dry run planned uncommitted declarations")
	}
	if len(w.changes) != 0 {
		t.Fatal("a refused apply wrote")
	}
	g.status = ""
	if _, err := declarations.Apply(s, l, claims, w, g, id); err != nil {
		t.Fatalf("after committing: %v", err)
	}
}

func TestApplyRefusesAStalePlanAndTheNewPlanClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	old := planID(t, s, l, claims)
	l.resources["test-zone/example.org"].Children["record"][1].Fields["comment"] = "edited by hand"
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, old)
	mustContain(t, err, "the live state or the declarations changed since the dry run that printed "+old+": run infratool manifest apply --dry-run again, review its plan, and pass the identifier it ends with")
	if len(w.changes) != 0 {
		t.Fatal("a refused apply wrote")
	}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatalf("with the new identifier: %v", err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, []string{"update test-zone/example.org record hello,example.org,TXT comment"}) {
		t.Fatalf("changes %q", got)
	}
}

func TestApplyRefusesAStalePlanAfterADeclarationEdit(t *testing.T) {
	p := siteProject(t)
	l := siteLive()
	s := load(t, p)
	claims := claimAll(t, s)
	old := planID(t, s, l, claims)
	declare(t, p, "test-server", "web1", "# edited\n"+serverBody)
	_, err := declarations.Apply(load(t, p), l, claims, &fakeWriter{live: l}, &fakeGit{}, old)
	mustContain(t, err, "changed since the dry run that printed "+old)
}

func TestApplyRefusesWhileMisclaimedAndFixingTheClaimsClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	l := siteLive()
	claims := declarations.Claims{Root: t.TempDir(), Claims: []declarations.Claim{{ResourceType: "test-server", Name: "web1", Project: "example"}, {ResourceType: "test-zone", Name: "example.org", Project: "other"}}}
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, noPlan)
	mustContain(t, err, "misclaimed", "test-zone/example.org", "claims.toml", "run infratool manifest apply --dry-run again")
	claims.Claims[1].Project = "example"
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatalf("after fixing the claims: %v", err)
	}
}

func TestApplyRefusesWhileAReadFailsAndARetryClearsIt(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	l.readErr["test-server/web1"] = errors.New("429 rate limited")
	w := &fakeWriter{live: l}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, noPlan)
	mustContain(t, err, "test-server/web1", "429 rate limited", "run infratool manifest apply --dry-run again")
	delete(l.readErr, "test-server/web1")
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatalf("once the read succeeds: %v", err)
	}
}

func TestApplyStopsAtTheFirstWriterFailure(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	delete(l.resources, "test-zone/example.org")
	w := &fakeWriter{live: l, failOn: 2}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims))
	mustContain(t, err, "the provider refused", "test-zone/example.org", "1 change", "run infratool manifest apply --dry-run again")
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
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
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
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
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

// A value the plan waits on that does not exist once its creation ran stops
// the apply at the change needing it, naming the placeholder.
func TestApplyStopsWhenAPendingValueDoesNotExistAfterItsCreation(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	w := &fakeWriter{live: l, noIPv4: true}
	_, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims))
	mustContain(t, err, "apply stopped at the create of test-zone/example.org record", "<ipv4 of new test-server web1> does not exist after its creation", "3 change(s) were applied before it", "run infratool manifest apply --dry-run again")
}

// A rule keyed by port whose source refers to a server the apply creates,
// while its description changed: both updates run after the server's
// creation, the source's carrying the new server's ipv4, and the
// description's carrying the rule with that source too.
func TestApplyUpdateOfAChildWithAPendingFieldRunsAfterTheCreation(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-server", "web1", serverBody)
	declare(t, p, "test-firewall", "edge", "unmanaged = []\n[[rule]]\nport = 22\nsource = { test_server = \"web1\", value = \"ipv4\" }\ndescription = \"new\"\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(declarations.Resource{Type: "test-firewall", Name: "edge", Children: map[string][]declarations.Item{"rule": {{Fields: M{"port": int64(22), "source": "198.51.100.1", "description": "old"}}}}})
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create test-server/web1",
		"update test-firewall/edge rule 22 description",
		"update test-firewall/edge rule 22 source",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
	wantItem := declarations.Item{Fields: M{"port": int64(22), "source": "203.0.113.1", "description": "new"}}
	for _, ch := range w.changes[1:] {
		if !reflect.DeepEqual(ch.Item, wantItem) || !reflect.DeepEqual(ch.Resource.Children["rule"], []declarations.Item{wantItem}) {
			t.Fatalf("the update carries %+v and %+v; want %+v", ch.Item, ch.Resource.Children["rule"], wantItem)
		}
	}
	if !verify(t, s, l, claims).Matching {
		t.Fatal("not matching after apply")
	}
}

// A rule whose source refers to a live server lacking the value waits on a
// value no change creates: an update of its description carries the live
// source in place of the unresolved one.
func TestApplyUpdateOfAChildWithAnUnresolvedFieldCarriesTheLiveValue(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-server", "web1", serverBody)
	declare(t, p, "test-firewall", "edge", "unmanaged = []\n[[rule]]\nport = 22\nsource = { test_server = \"web1\", value = \"ipv4\" }\ndescription = \"new\"\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(liveServer("web1"))
	l.put(declarations.Resource{Type: "test-firewall", Name: "edge", Children: map[string][]declarations.Item{"rule": {{Fields: M{"port": int64(22), "source": "198.51.100.1", "description": "old"}}}}})
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatal(err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, []string{"update test-firewall/edge rule 22 description"}) {
		t.Fatalf("apply: %q", got)
	}
	wantItem := declarations.Item{Fields: M{"port": int64(22), "source": "198.51.100.1", "description": "new"}}
	if !reflect.DeepEqual(w.changes[0].Item, wantItem) || !reflect.DeepEqual(w.changes[0].Resource.Children["rule"], []declarations.Item{wantItem}) {
		t.Fatalf("the update carries %+v; want %+v", w.changes[0], wantItem)
	}
}

const boxesSchema = `name = "test-boxes"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Boxes"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Boxes]
type = "record"
[types.Boxes.fields.mailbox]
type = "array"
required = true
[types.Boxes.fields.mailbox.item]
type = "Mailbox"
[types.Boxes.fields.forwarding]
type = "array"
required = true
[types.Boxes.fields.forwarding.item]
type = "Forwarding"
[types.Boxes.fields.unmanaged]
type = "array"
required = true
[types.Boxes.fields.unmanaged.item]
type = "UnmanagedEntry"

[types.Mailbox]
type = "record"
[types.Mailbox.fields.local_part]
type = "string"
required = true

[types.Forwarding]
type = "record"
[types.Forwarding.fields.mailbox]
type = "string"
required = true
[types.Forwarding.fields.address]
type = "string"
required = true
`

// boxesType holds mailboxes and their forwardings, a forwarding naming its
// mailbox: its collections are listed in the order a mailbox must exist
// before its forwardings, which is not their names' order.
type boxesType struct{}

func (boxesType) Name() string { return "test-boxes" }
func (boxesType) Schema() (map[string]string, string) {
	return map[string]string{"test-boxes.schema.toml": boxesSchema}, "test-boxes.schema.toml"
}
func (boxesType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{
		Collections: []declarations.Collection{{Name: "mailbox", Key: []string{"local_part"}}, {Name: "forwarding", Key: []string{"mailbox", "address"}}},
		Stage:       30,
	}
}
func (boxesType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-boxes", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
	for _, col := range []string{"mailbox", "forwarding"} {
		items, _ := field(doc, col)
		r.Children[col] = []declarations.Item{}
		for _, it := range items.Items() {
			item := declarations.Item{Fields: map[string]declarations.Value{}}
			for _, kv := range it.Entries() {
				item.Fields[kv.Key] = valueOf(kv.Value)
			}
			r.Children[col] = append(r.Children[col], item)
		}
	}
	return r, nil
}
func (boxesType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (boxesType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

// Within one resource, apply creates and updates children in the order of
// its resource type's collections, and deletes them in the reverse order:
// a mailbox is created before its forwarding and deleted after its
// forwarding.
func TestApplyOrdersAResourcesChildrenByItsCollections(t *testing.T) {
	tool := testTool()
	tool.ResourceTypes = []declarations.ResourceType{boxesType{}}
	p := newProject(t, "example")
	declare(t, p, "test-boxes", "example.org", "unmanaged = []\n[[mailbox]]\nlocal_part = \"new\"\n[[forwarding]]\nmailbox = \"new\"\naddress = \"me@elsewhere.test\"\n")
	s, err := declarations.Load(tool, p)
	if err != nil {
		t.Fatal(err)
	}
	claims := claimAll(t, s)
	l := newLive()
	l.put(declarations.Resource{Type: "test-boxes", Name: "example.org", Children: map[string][]declarations.Item{
		"mailbox":    {{Fields: M{"local_part": "old"}}},
		"forwarding": {{Fields: M{"mailbox": "old", "address": "old@elsewhere.test"}}},
	}})
	w := &fakeWriter{live: l}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create test-boxes/example.org mailbox new",
		"create test-boxes/example.org forwarding me@elsewhere.test,new",
		"delete test-boxes/example.org forwarding old@elsewhere.test,old",
		"delete test-boxes/example.org mailbox old",
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("apply:\n got  %q\n want %q", got, want)
	}
}

// The plan lists every change Execute passes to the writer, in the same
// order, without writing anything itself; its identifier is the one Apply
// accepts.
func TestPlanListsTheChangesExecuteRuns(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	l := siteLive()
	delete(l.resources, "test-zone/example.org")
	w := &fakeWriter{live: l}
	plan, err := declarations.PlanApply(s, l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) == 0 || plan.Report.Matching {
		t.Fatalf("a missing zone planned nothing: %+v", plan)
	}
	if err := plan.Accept(plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := plan.Accept(noPlan); err == nil {
		t.Fatal("a plan accepted another identifier")
	}
	if len(w.changes) != 0 {
		t.Fatal("planning wrote")
	}
	if _, err := plan.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := describeAll(w.changes), describeAll(plan.Changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("executed %q; planned %q", got, want)
	}
	if !verify(t, s, l, claims).Matching {
		t.Fatal("not matching after executing the plan")
	}
}

// A change the writer refuses is refused when the plan is made, before any
// write, naming every refused change and the writer's reason; the writer sees
// the whole plan. Once the declaration no longer asks for the change, the
// refusal clears and the apply runs.
func TestAPlanHoldingAChangeTheWriterRefusesIsRefusedBeforeAnyWrite(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-mail", "example.org", "plan = \"micro\"\nunmanaged = []\nmailbox = []\n")
	s := load(t, p)
	claims := claimAll(t, s)
	l := newLive()
	l.put(declarations.Resource{Type: "test-mail", Name: "example.org", Fields: M{"plan": "mini", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{"mailbox": {}}})
	l.put(liveZone("example.org", false,
		record("TXT", "example.org", "hello", int64(600), ""),
		record("MX", "example.org", "mx.mail.test", int64(300), ""),
		record("TXT", "dkim._domainkey.example.org", "k=1", "automatic", "")))
	w := &fakeWriter{live: l, refuse: map[string]string{"update test-mail/example.org plan": "the provider cannot change a plan: declare plan = \"mini\""}}
	_, err := declarations.PlanApply(s, l, claims, w, &fakeGit{})
	mustContain(t, err, "apply refuses the plan before any write", "the update of test-mail/example.org field plan: the provider cannot change a plan: declare plan = \"mini\"", "then run infratool manifest apply --dry-run again")
	if len(w.changes) != 0 {
		t.Fatalf("a refused plan wrote: %q", describeAll(w.changes))
	}
	want := []string{"update test-zone/example.org record hello,example.org,TXT ttl", "update test-mail/example.org plan"}
	for _, plan := range w.plans {
		if got := describeAll(plan); !reflect.DeepEqual(got, want) {
			t.Fatalf("Refuse was given the plan %q, want %q", got, want)
		}
	}
	if len(w.plans) != len(want) {
		t.Fatalf("Refuse was asked of %d changes, want %d", len(w.plans), len(want))
	}
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, noPlan); err == nil || !strings.Contains(err.Error(), "apply refuses the plan before any write") || len(w.changes) != 0 {
		t.Fatalf("apply ran a plan holding a refused change: %v, %q", err, describeAll(w.changes))
	}
	// The fix: declare the live plan.
	declare(t, p, "test-mail", "example.org", "plan = \"mini\"\nunmanaged = []\nmailbox = []\n")
	s = load(t, p)
	if _, err := declarations.Apply(s, l, claims, w, &fakeGit{}, planID(t, s, l, claims)); err != nil {
		t.Fatalf("after declaring the live plan: %v", err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("applied %q", got)
	}
}
