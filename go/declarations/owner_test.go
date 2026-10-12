package declarations_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

const addressSchema = `name = "test-address"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Address"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Address]
type = "record"
[types.Address.fields.server]
type = "string"
required = true
[types.Address.fields.unmanaged]
type = "array"
required = true
[types.Address.fields.unmanaged.item]
type = "UnmanagedEntry"
`

// addressType is an address a provider creates with a server, which owns it
// while it is assigned to it.
type addressType struct{}

func (addressType) Name() string { return "test-address" }
func (addressType) Schema() (map[string]string, string) {
	return map[string]string{"test-address.schema.toml": addressSchema}, "test-address.schema.toml"
}
func (addressType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{Fields: []string{"server"}, Stage: 45}
}
func (addressType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	v, _ := field(doc, "server")
	return declarations.Resource{Type: "test-address", Name: name, Fields: map[string]declarations.Value{"server": valueOf(v)}, Children: map[string][]declarations.Item{}}, nil
}
func (addressType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (addressType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}
func (addressType) Owner(r declarations.Resource) (string, string, bool) {
	s, _ := r.Fields["server"].(string)
	return "test-server", s, s != ""
}

func ownerTool() declarations.Tool {
	t := testTool()
	t.ResourceTypes = append(t.ResourceTypes, addressType{})
	return t
}

func liveAddress(name, server string) declarations.Resource {
	return declarations.Resource{Type: "test-address", Name: name, Fields: M{"server": server}}
}

// In the account repository an address its server owns is never unclaimed:
// it stays with a claimed, declared, or unmanaged server, and goes with an
// unclaimed one, in the server's deletion, never deleted on its own. An
// address nothing owns is unclaimed as any other resource.
func TestAnOwnedResourceGoesWithItsOwner(t *testing.T) {
	a := newAccount(t, "unmanaged = [{ resource_type = \"test-server\", name = \"old\", reason = \"a manual experiment\" }]\nclaim = [{ resource_type = \"test-server\", name = \"web1\", project = \"example\" }]\n")
	declare(t, a, "test-server", "db", serverBody)
	claims, err := declarations.LoadClaims(ownerTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := declarations.Load(ownerTool(), a)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	for _, n := range []string{"web1", "old", "db", "stray"} {
		l.put(liveServer(n))
	}
	l.put(liveAddress("ip-web1", "web1"))
	l.put(liveAddress("ip-old", "old"))
	l.put(liveAddress("ip-db", "db"))
	l.put(liveAddress("ip-stray", "stray"))
	l.put(liveAddress("ip-free", ""))
	r := verify(t, s, l, claims)
	wantLines(t, r, "differences",
		`{"name":"ip-free","resource_type":"test-address","status":"unclaimed"}`,
		`{"name":"stray","resource_type":"test-server","status":"unclaimed"}`)

	p, err := declarations.PlanApply(s, l, claims, &fakeWriter{live: l}, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describeAll(p.Changes), []string{"delete test-address/ip-free", "delete test-server/stray"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan %q, want %q", got, want)
	}
	del := p.Changes[1]
	if len(del.Owned) != 1 || del.Owned[0].Name != "ip-stray" || del.Owned[0].Fields["server"] != "stray" {
		t.Fatalf("the server's deletion carries what it owns: %+v", del.Owned)
	}
	if del.Resource.Fields["location"] != "fsn1" {
		t.Fatalf("a resource-level delete carries the live resource as read: %+v", del.Resource)
	}
}

// When listing the owners' resource type fails, an address a server owns
// is not taken for unclaimed: the report names the failed list and the
// address it leaves unjudged, and once the list reads, the address stays
// with its claimed server.
func TestAnOwnedResourceWhoseOwnersCannotBeListedIsUnreadableNotUnclaimed(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"web1\", project = \"example\" }]\n")
	claims, err := declarations.LoadClaims(ownerTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := declarations.Load(ownerTool(), a)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("web1"))
	l.put(liveAddress("ip-web1", "web1"))
	l.listErr["test-server"] = errors.New("the provider refused")
	r := verify(t, s, l, claims)
	wantLines(t, r, "differences")
	wantLines(t, r, "unreadable",
		`{"error":"its owner test-server/web1 is of a resource type whose list failed, so whether it goes with its owner is not known","name":"ip-web1","resource_type":"test-address"}`,
		`{"error":"the provider refused","resource_type":"test-server"}`)
	delete(l.listErr, "test-server")
	r = verify(t, s, l, claims)
	wantLines(t, r, "differences")
	wantLines(t, r, "unreadable")
}
