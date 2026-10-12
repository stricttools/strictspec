package declarations_test

import (
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

const relaySchema = `name = "test-relay"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Relay"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Relay]
type = "record"
[types.Relay.fields.unmanaged]
type = "array"
required = true
[types.Relay.fields.unmanaged.item]
type = "UnmanagedEntry"
`

// relayType derives a DKIM record into its zone, the record test-mail
// derives too, giving way to test-mail where both are declared.
type relayType struct{}

func (relayType) Name() string { return "test-relay" }
func (relayType) Schema() (map[string]string, string) {
	return map[string]string{"test-relay.schema.toml": relaySchema}, "test-relay.schema.toml"
}
func (relayType) Descriptor() declarations.Descriptor { return declarations.Descriptor{Stage: 30} }
func (relayType) Decode(name string, _ strictspec.Value) (declarations.Resource, error) {
	return declarations.Resource{Type: "test-relay", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}, nil
}
func (relayType) Slots(r declarations.Resource) []declarations.Slot {
	return []declarations.Slot{{ResourceType: "test-zone", Name: r.Name, Collection: "record", YieldsTo: []string{"test-mail"},
		Match: map[string]declarations.Matcher{"type": declarations.Exact("TXT"), "name": declarations.Exact("dkim._domainkey." + r.Name)}}}
}
func (relayType) Derive(r declarations.Resource, _ declarations.LiveReader) ([]declarations.Derived, error) {
	return []declarations.Derived{{Slot: relayType{}.Slots(r)[0], Items: []declarations.Item{record("TXT", "dkim._domainkey."+r.Name, "relay-key", "automatic", "")}}}, nil
}

func relayTool() declarations.Tool {
	t := testTool()
	t.ResourceTypes = append(t.ResourceTypes, relayType{})
	return t
}

// A slot yielding to a resource type derives its child alone, and gives way
// where a resource of that type derives the same child, instead of both
// being refused.
func TestAYieldingSlotGivesWayToTheResourceTypeItYieldsTo(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	declare(t, p, "test-relay", "example.org", "unmanaged = []\n")
	l := newLive()
	l.put(liveZone("example.org", false, record("TXT", "example.org", "hello", int64(300), "")))
	l.put(declarations.Resource{Type: "test-relay", Name: "example.org"})
	l.put(declarations.Resource{Type: "test-mail", Name: "example.org", Fields: M{"plan": "micro", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{"mailbox": {{Fields: M{"local_part": "me", "name": "Me"}}}}})
	set, err := declarations.Load(relayTool(), p)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, compare(t, set, l), "differences",
		`{"collection":"record","declared":{"comment":"","ttl":"automatic"},"key":{"content":"relay-key","name":"dkim._domainkey.example.org","type":"TXT"},"name":"example.org","resource_type":"test-zone","source":"test-relay/example.org","status":"missing"}`)

	declare(t, p, "test-mail", "example.org", mailBody)
	set, err = declarations.Load(relayTool(), p)
	if err != nil {
		t.Fatalf("both declared: %v", err)
	}
	r := compare(t, set, l)
	var dkim []string
	for _, line := range lines(t, r, "differences") {
		if strings.Contains(line, "dkim._domainkey") {
			dkim = append(dkim, line)
		}
	}
	if len(dkim) != 1 || !strings.Contains(dkim[0], `"content":"k=1"`) || !strings.Contains(dkim[0], `"source":"test-mail/example.org"`) {
		t.Fatalf("the DKIM record %q", dkim)
	}
}
