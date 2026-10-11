package declarations_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

// The fake resource types: a zone with records (exposing its assigned
// nameservers, and deriving a registered domain's nameservers when declared
// with delegate = true), a server exposing ipv4, a mail domain deriving its
// records in the zone, and a registered domain whose nameservers are a
// derivable field.

const zoneSchema = `name = "test-zone"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Zone"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Zone]
type = "record"
[types.Zone.fields.delegate]
type = "boolean"
required = true
[types.Zone.fields.record]
type = "array"
required = true
[types.Zone.fields.record.item]
type = "Record"
[types.Zone.fields.unmanaged]
type = "array"
required = true
[types.Zone.fields.unmanaged.item]
type = "UnmanagedEntry"

[types.Record]
type = "record"
[types.Record.fields.type]
type = "enum"
required = true
values = ["A", "MX", "TXT", "CNAME"]
[types.Record.fields.name]
type = "string"
required = true
non_empty = true
[types.Record.fields.content]
type = "Content"
required = true
[types.Record.fields.ttl]
type = "opaque"
required = true
consumer_check = "test-zone.ttl"
[types.Record.fields.comment]
type = "string"
required = true

[types.Content]
type = "node-kind-union"
[types.Content.arms.text]
type = "string"
[types.Content.arms.reference]
type = "ServerReference"

[types.ServerReference]
type = "record"
[types.ServerReference.fields.test_server]
type = "string"
required = true
[types.ServerReference.fields.value]
type = "string"
required = true
`

const serverSchema = `name = "test-server"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Server"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Server]
type = "record"
[types.Server.fields.location]
type = "string"
required = true
[types.Server.fields.labels]
type = "map"
required = true
key_pattern = "^[a-z]+$"
order = "incidental"
[types.Server.fields.labels.value]
type = "string"
[types.Server.fields.image]
type = "string"
required = true
[types.Server.fields.unmanaged]
type = "array"
required = true
[types.Server.fields.unmanaged.item]
type = "UnmanagedEntry"
`

const mailSchema = `name = "test-mail"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Mail"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Mail]
type = "record"
[types.Mail.fields.plan]
type = "string"
required = true
[types.Mail.fields.mailbox]
type = "array"
required = true
[types.Mail.fields.mailbox.item]
type = "Mailbox"
[types.Mail.fields.unmanaged]
type = "array"
required = true
[types.Mail.fields.unmanaged.item]
type = "UnmanagedEntry"

[types.Mailbox]
type = "record"
[types.Mailbox.fields.local_part]
type = "string"
required = true
[types.Mailbox.fields.name]
type = "string"
required = true
[types.Mailbox.fields.invitation]
type = "string"
required = false
`

const domainSchema = `name = "test-domain"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Domain"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Domain]
type = "record"
[types.Domain.fields.auto_renew]
type = "boolean"
required = true
[types.Domain.fields.nameservers]
type = "array"
required = false
[types.Domain.fields.nameservers.item]
type = "string"
[types.Domain.fields.unmanaged]
type = "array"
required = true
[types.Domain.fields.unmanaged.item]
type = "UnmanagedEntry"
`

// valueOf converts a schema-valid scalar, array, or table to a Value.
func valueOf(v strictspec.Value) declarations.Value {
	switch v.Kind() {
	case strictspec.KindString:
		s, _ := v.AsString()
		return s
	case strictspec.KindInteger:
		n, _ := v.Int()
		return n
	case strictspec.KindBool:
		b, _ := v.Bool()
		return b
	case strictspec.KindArray:
		out := []declarations.Value{}
		for _, it := range v.Items() {
			out = append(out, valueOf(it))
		}
		return out
	case strictspec.KindRecord:
		out := map[string]declarations.Value{}
		for _, kv := range v.Entries() {
			out[kv.Key] = valueOf(kv.Value)
		}
		return out
	}
	panic(fmt.Sprintf("a value valueOf does not bind, at %v", v))
}

func field(doc strictspec.Value, name string) (strictspec.Value, bool) { return doc.Field(name) }

type zoneType struct{}

func (zoneType) Name() string { return "test-zone" }
func (zoneType) Schema() (map[string]string, string) {
	return map[string]string{"test-zone.schema.toml": zoneSchema}, "test-zone.schema.toml"
}
func (zoneType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{
		Fields:      []string{"delegate"},
		Collections: []declarations.Collection{{Name: "record", Key: []string{"type", "name", "content"}, Fields: []string{"ttl", "comment"}}},
		Stage:       20,
		Exposes:     []string{"nameservers"},
	}
}
func (zoneType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-zone", Name: strings.ToLower(name), Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{"record": {}}}
	d, _ := field(doc, "delegate")
	r.Fields["delegate"] = valueOf(d)
	recs, _ := field(doc, "record")
	for i, rec := range recs.Items() {
		it := declarations.Item{Fields: map[string]declarations.Value{}}
		for _, k := range []string{"type", "name", "comment"} {
			v, _ := field(rec, k)
			it.Fields[k] = valueOf(v)
		}
		it.Fields["name"] = strings.ToLower(it.Fields["name"].(string))
		c, _ := field(rec, "content")
		if c.Kind() == strictspec.KindRecord {
			ref, err := declarations.ParseReference(c)
			if err != nil {
				return r, fmt.Errorf("test-zone/%s.toml: record[%d].content: %w", name, i, err)
			}
			it.Fields["content"] = ref
		} else {
			it.Fields["content"] = valueOf(c)
		}
		ttl, _ := field(rec, "ttl")
		switch ttl.Kind() {
		case strictspec.KindString:
			s, _ := ttl.AsString()
			if s != "automatic" {
				return r, fmt.Errorf("test-zone/%s.toml: record[%d].ttl is %q: write \"automatic\" or an integer of at least 60", name, i, s)
			}
			it.Fields["ttl"] = s
		case strictspec.KindInteger:
			n, _ := ttl.Int()
			if n < 60 {
				return r, fmt.Errorf("test-zone/%s.toml: record[%d].ttl is %d: write \"automatic\" or an integer of at least 60", name, i, n)
			}
			it.Fields["ttl"] = n
		default:
			return r, fmt.Errorf("test-zone/%s.toml: record[%d].ttl: write \"automatic\" or an integer of at least 60", name, i)
		}
		r.Children["record"] = append(r.Children["record"], it)
	}
	return r, nil
}
func (zoneType) Slots(r declarations.Resource) []declarations.Slot {
	if r.Fields["delegate"] != true {
		return nil
	}
	return []declarations.Slot{{ResourceType: "test-domain", Name: r.Name, Field: "nameservers"}}
}
func (zoneType) Derive(r declarations.Resource, live declarations.LiveReader) ([]declarations.Derived, error) {
	var out []declarations.Derived
	for _, s := range (zoneType{}).Slots(r) {
		v, ok, err := live.Exposed("test-zone", r.Name, "nameservers")
		if err != nil {
			return nil, err
		}
		if !ok {
			out = append(out, declarations.Derived{Slot: s, Unresolved: "the zone " + r.Name + " is not live yet"})
			continue
		}
		out = append(out, declarations.Derived{Slot: s, Value: v})
	}
	return out, nil
}

type serverType struct{}

func (serverType) Name() string { return "test-server" }
func (serverType) Schema() (map[string]string, string) {
	return map[string]string{"test-server.schema.toml": serverSchema}, "test-server.schema.toml"
}
func (serverType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{Fields: []string{"location", "labels"}, Stage: 40, Exposes: []string{"ipv4"}, CreateOnly: []string{"image"}}
}
func (serverType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-server", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
	for _, k := range []string{"location", "labels", "image"} {
		v, _ := field(doc, k)
		r.Fields[k] = valueOf(v)
	}
	return r, nil
}
func (serverType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (serverType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

type mailType struct{}

func (mailType) Name() string { return "test-mail" }
func (mailType) Schema() (map[string]string, string) {
	return map[string]string{"test-mail.schema.toml": mailSchema}, "test-mail.schema.toml"
}
func (mailType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{
		Fields:      []string{"plan"},
		Collections: []declarations.Collection{{Name: "mailbox", Key: []string{"local_part"}, Fields: []string{"name"}}},
		Stage:       30,
	}
}
func (mailType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-mail", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{"mailbox": {}}}
	p, _ := field(doc, "plan")
	r.Fields["plan"] = valueOf(p)
	boxes, _ := field(doc, "mailbox")
	for _, b := range boxes.Items() {
		it := declarations.Item{Fields: map[string]declarations.Value{}}
		for _, kv := range b.Entries() {
			it.Fields[kv.Key] = valueOf(kv.Value)
		}
		r.Children["mailbox"] = append(r.Children["mailbox"], it)
	}
	return r, nil
}
func (mailType) Slots(r declarations.Resource) []declarations.Slot {
	return []declarations.Slot{
		{ResourceType: "test-zone", Name: r.Name, Collection: "record", Match: map[string]declarations.Matcher{"type": declarations.Exact("MX"), "name": declarations.Exact(r.Name)}},
		{ResourceType: "test-zone", Name: r.Name, Collection: "record", Match: map[string]declarations.Matcher{"type": declarations.Exact("TXT"), "name": declarations.Exact("dkim._domainkey." + r.Name)}},
	}
}

func record(typ, name, content string, ttl declarations.Value, comment string) declarations.Item {
	return declarations.Item{Fields: map[string]declarations.Value{"type": typ, "name": name, "content": content, "ttl": ttl, "comment": comment}}
}

// Derive reads the live mail domain for its DKIM key, as a mail provider's
// record set is read.
func (mailType) Derive(r declarations.Resource, live declarations.LiveReader) ([]declarations.Derived, error) {
	slots := mailType{}.Slots(r)
	m, ok, err := live.Read("test-mail", r.Name)
	if err != nil {
		return nil, err
	}
	if !ok {
		why := "the mail domain " + r.Name + " is not live yet"
		return []declarations.Derived{{Slot: slots[0], Unresolved: why}, {Slot: slots[1], Unresolved: why}}, nil
	}
	key, _ := m.Fields["dkim_key"].(string)
	return []declarations.Derived{
		{Slot: slots[0], Items: []declarations.Item{record("MX", r.Name, "mx.mail.test", int64(300), "")}},
		{Slot: slots[1], Items: []declarations.Item{record("TXT", "dkim._domainkey."+r.Name, key, "automatic", "")}},
	}, nil
}

type domainType struct{}

func (domainType) Name() string { return "test-domain" }
func (domainType) Schema() (map[string]string, string) {
	return map[string]string{"test-domain.schema.toml": domainSchema}, "test-domain.schema.toml"
}
func (domainType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{Fields: []string{"auto_renew", "nameservers"}, Stage: 50, DerivableFields: []string{"nameservers"}}
}
func (domainType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-domain", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
	a, _ := field(doc, "auto_renew")
	r.Fields["auto_renew"] = valueOf(a)
	if ns, ok := field(doc, "nameservers"); ok {
		r.Fields["nameservers"] = valueOf(ns)
	}
	return r, nil
}
func (domainType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (domainType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

const forwardSchema = `name = "test-forward"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Forward"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Forward]
type = "record"
[types.Forward.fields.zone]
type = "string"
required = true
[types.Forward.fields.record_type]
type = "string"
required = true
[types.Forward.fields.prefix]
type = "string"
required = true
[types.Forward.fields.delegates]
type = "string"
required = false
[types.Forward.fields.unmanaged]
type = "array"
required = true
[types.Forward.fields.unmanaged.item]
type = "UnmanagedEntry"
`

// forwardType causes, in the zone it names, the records of one type whose
// content starts with its prefix, and, when it names a domain it delegates,
// that domain's nameservers: a cause whose slots a test chooses.
type forwardType struct{}

func (forwardType) Name() string { return "test-forward" }
func (forwardType) Schema() (map[string]string, string) {
	return map[string]string{"test-forward.schema.toml": forwardSchema}, "test-forward.schema.toml"
}
func (forwardType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{Fields: []string{"zone", "record_type", "prefix", "delegates"}, Stage: 30}
}
func (forwardType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-forward", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
	for _, kv := range doc.Entries() {
		if kv.Key != "format_version" && kv.Key != "unmanaged" {
			r.Fields[kv.Key] = valueOf(kv.Value)
		}
	}
	return r, nil
}
func (forwardType) Slots(r declarations.Resource) []declarations.Slot {
	zone := r.Fields["zone"].(string)
	out := []declarations.Slot{{ResourceType: "test-zone", Name: zone, Collection: "record", Match: map[string]declarations.Matcher{
		"type": declarations.Exact(r.Fields["record_type"]), "name": declarations.Exact(zone), "content": declarations.Prefix(r.Fields["prefix"].(string)),
	}}}
	if d, ok := r.Fields["delegates"].(string); ok {
		out = append(out, declarations.Slot{ResourceType: "test-domain", Name: d, Field: "nameservers"})
	}
	return out
}

// Derive reads the relay server, declared or not, as a derivation reads
// what it needs.
func (forwardType) Derive(r declarations.Resource, live declarations.LiveReader) ([]declarations.Derived, error) {
	if _, _, err := live.Read("test-server", "relay"); err != nil {
		return nil, err
	}
	slots := forwardType{}.Slots(r)
	zone := r.Fields["zone"].(string)
	out := []declarations.Derived{{Slot: slots[0], Items: []declarations.Item{record(r.Fields["record_type"].(string), zone, r.Fields["prefix"].(string)+" forward", "automatic", "")}}}
	if len(slots) > 1 {
		out = append(out, declarations.Derived{Slot: slots[1], Value: []declarations.Value{"fwd.ns.test"}})
	}
	return out, nil
}

const firewallSchema = `name = "test-firewall"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Firewall"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Firewall]
type = "record"
[types.Firewall.fields.rule]
type = "array"
required = true
[types.Firewall.fields.rule.item]
type = "Rule"
[types.Firewall.fields.unmanaged]
type = "array"
required = true
[types.Firewall.fields.unmanaged.item]
type = "UnmanagedEntry"

[types.Rule]
type = "record"
[types.Rule.fields.port]
type = "integer"
required = true
[types.Rule.fields.source]
type = "Source"
required = true
[types.Rule.fields.description]
type = "string"
required = true

[types.Source]
type = "node-kind-union"
[types.Source.arms.text]
type = "string"
[types.Source.arms.reference]
type = "ServerReference"

[types.ServerReference]
type = "record"
[types.ServerReference.fields.test_server]
type = "string"
required = true
[types.ServerReference.fields.value]
type = "string"
required = true
`

// firewallType is a firewall whose rules are keyed by port alone, a rule's
// source possibly referring to a server's ipv4: a child whose non-key field
// can wait on a value that does not exist yet while its key is known.
type firewallType struct{}

func (firewallType) Name() string { return "test-firewall" }
func (firewallType) Schema() (map[string]string, string) {
	return map[string]string{"test-firewall.schema.toml": firewallSchema}, "test-firewall.schema.toml"
}
func (firewallType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{
		Collections: []declarations.Collection{{Name: "rule", Key: []string{"port"}, Fields: []string{"source", "description"}}},
		Stage:       45,
	}
}
func (firewallType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	r := declarations.Resource{Type: "test-firewall", Name: name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{"rule": {}}}
	rules, _ := field(doc, "rule")
	for i, rule := range rules.Items() {
		it := declarations.Item{Fields: map[string]declarations.Value{}}
		for _, k := range []string{"port", "description"} {
			v, _ := field(rule, k)
			it.Fields[k] = valueOf(v)
		}
		src, _ := field(rule, "source")
		if src.Kind() == strictspec.KindRecord {
			ref, err := declarations.ParseReference(src)
			if err != nil {
				return r, fmt.Errorf("test-firewall/%s.toml: rule[%d].source: %w", name, i, err)
			}
			it.Fields["source"] = ref
		} else {
			it.Fields["source"] = valueOf(src)
		}
		r.Children["rule"] = append(r.Children["rule"], it)
	}
	return r, nil
}
func (firewallType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (firewallType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

func testTool() declarations.Tool {
	return declarations.Tool{
		Name:          "infratool",
		Directory:     "infrastructure",
		InitCommand:   "infratool manifest init",
		DryRunCommand: "infratool manifest apply --dry-run",
		ResourceTypes: []declarations.ResourceType{zoneType{}, serverType{}, mailType{}, domainType{}, forwardType{}, firewallType{}},
	}
}

const declDir = ".strictmetadata/infrastructure"

// writeFile writes content to rel under root, creating directories.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

// newProject makes a repository holding a declaration directory Init wrote,
// for the project name.
func newProject(t *testing.T, name string) declarations.Project {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".strictmetadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := declarations.Init(testTool(), root, name)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	for p, b := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := declarations.FindProject(testTool(), root)
	if err != nil {
		t.Fatalf("FindProject: %v", err)
	}
	return p
}

// declare writes a resource file of the project.
func declare(t *testing.T, p declarations.Project, resourceType, name, body string) {
	t.Helper()
	writeFile(t, p.Root, declDir+"/"+resourceType+"/"+name+".toml", "format_version = 1\n"+body)
}

func load(t *testing.T, p declarations.Project) declarations.Set {
	t.Helper()
	s, err := declarations.Load(testTool(), p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func mustContain(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error; want one containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q does not contain %q", err.Error(), p)
		}
	}
}

// fakeLive is live state in memory: resources by "<type>/<name>" and exposed
// values by "<type>/<name>/<value>". It records every Read.
type fakeLive struct {
	resources  map[string]declarations.Resource
	exposed    map[string]declarations.Value
	readErr    map[string]error
	listErr    map[string]error
	exposedErr map[string]error
	reads      []string
	nextIP     int
}

func newLive() *fakeLive {
	return &fakeLive{resources: map[string]declarations.Resource{}, exposed: map[string]declarations.Value{}, readErr: map[string]error{}, listErr: map[string]error{}, exposedErr: map[string]error{}}
}

func (l *fakeLive) put(r declarations.Resource) {
	if r.Fields == nil {
		r.Fields = map[string]declarations.Value{}
	}
	if r.Children == nil {
		r.Children = map[string][]declarations.Item{}
	}
	l.resources[r.Type+"/"+r.Name] = r
}

func (l *fakeLive) List(resourceType string) ([]string, error) {
	if err := l.listErr[resourceType]; err != nil {
		return nil, err
	}
	var out []string
	for k, r := range l.resources {
		if strings.HasPrefix(k, resourceType+"/") {
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (l *fakeLive) Read(resourceType, name string) (declarations.Resource, bool, error) {
	k := resourceType + "/" + name
	l.reads = append(l.reads, k)
	if err := l.readErr[k]; err != nil {
		return declarations.Resource{}, false, err
	}
	r, ok := l.resources[k]
	if !ok {
		return declarations.Resource{}, false, nil
	}
	return copyResource(r), true, nil
}

func (l *fakeLive) Exposed(resourceType, name, value string) (declarations.Value, bool, error) {
	k := resourceType + "/" + name + "/" + value
	if err := l.exposedErr[k]; err != nil {
		return nil, false, err
	}
	v, ok := l.exposed[k]
	return v, ok, nil
}

func copyResource(r declarations.Resource) declarations.Resource {
	c := declarations.Resource{Type: r.Type, Name: r.Name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
	for k, v := range r.Fields {
		c.Fields[k] = v
	}
	for k, items := range r.Children {
		c.Children[k] = []declarations.Item{}
		for _, it := range items {
			c.Children[k] = append(c.Children[k], copyItem(it))
		}
	}
	return c
}

func copyItem(it declarations.Item) declarations.Item {
	c := declarations.Item{Fields: map[string]declarations.Value{}}
	for k, v := range it.Fields {
		c.Fields[k] = v
	}
	return c
}

// fakeWriter applies changes to a fakeLive, recording them; a created server
// gets an ipv4, a created zone its nameservers.
type fakeWriter struct {
	live    *fakeLive
	changes []declarations.Change
	failOn  int // 1-based index of the change that fails; 0 for none
}

func sameKey(it declarations.Item, key map[string]declarations.Value) bool {
	for k, v := range key {
		if fmt.Sprint(it.Fields[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

func (w *fakeWriter) Apply(c declarations.Change) error {
	w.changes = append(w.changes, c)
	if w.failOn == len(w.changes) {
		return fmt.Errorf("the provider refused")
	}
	k := c.ResourceType + "/" + c.Name
	r := w.live.resources[k]
	switch {
	case c.Collection == "" && c.Field == "" && c.Action == declarations.ActionCreate:
		n := declarations.Resource{Type: c.ResourceType, Name: c.Name, Fields: map[string]declarations.Value{}, Children: map[string][]declarations.Item{}}
		for f, v := range c.Resource.Fields {
			n.Fields[f] = v
		}
		w.live.put(n)
		switch c.ResourceType {
		case "test-server":
			w.live.nextIP++
			w.live.exposed[k+"/ipv4"] = fmt.Sprintf("203.0.113.%d", w.live.nextIP)
		case "test-zone":
			w.live.exposed[k+"/nameservers"] = []declarations.Value{"a.ns.test", "b.ns.test"}
		}
	case c.Collection == "" && c.Field == "" && c.Action == declarations.ActionDelete:
		delete(w.live.resources, k)
	case c.Collection == "" && c.Field != "":
		r.Fields[c.Field] = c.Resource.Fields[c.Field]
	case c.Collection != "" && c.Field == "" && c.Action == declarations.ActionCreate:
		r.Children[c.Collection] = append(r.Children[c.Collection], copyItem(c.Item))
	case c.Collection != "" && c.Field == "" && c.Action == declarations.ActionDelete:
		var kept []declarations.Item
		for _, it := range r.Children[c.Collection] {
			if !sameKey(it, c.Key) {
				kept = append(kept, it)
			}
		}
		r.Children[c.Collection] = kept
	case c.Collection != "" && c.Field != "":
		for i, it := range r.Children[c.Collection] {
			if sameKey(it, c.Key) {
				r.Children[c.Collection][i].Fields[c.Field] = c.Item.Fields[c.Field]
			}
		}
	default:
		return fmt.Errorf("fake writer: unexpected change %+v", c)
	}
	return nil
}

type fakeGit struct {
	status string
	calls  [][2]string
}

func (g *fakeGit) Status(root, dir string) (string, error) {
	g.calls = append(g.calls, [2]string{root, dir})
	return g.status, nil
}
