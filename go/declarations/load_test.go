package declarations_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

const (
	serverBody = "location = \"fsn1\"\nlabels = { role = \"game\" }\nimage = \"debian-13\"\nunmanaged = []\n"
	aRecord    = "[[record]]\ntype = \"A\"\nname = \"example.org\"\ncontent = { test_server = \"web1\", value = \"ipv4\" }\nttl = \"automatic\"\ncomment = \"the site\"\n"
	txtRecord  = "[[record]]\ntype = \"TXT\"\nname = \"example.org\"\ncontent = \"hello\"\nttl = 300\ncomment = \"\"\n"
	mxRecord   = "[[record]]\ntype = \"MX\"\nname = \"example.org\"\ncontent = \"mx.elsewhere.test\"\nttl = 300\ncomment = \"\"\n"
	mailBody   = "plan = \"micro\"\nunmanaged = []\n[[mailbox]]\nlocal_part = \"me\"\nname = \"Me\"\ninvitation = \"me@elsewhere.test\"\n"
	domainBody = "auto_renew = true\nnameservers = [\"a.ns.test\", \"b.ns.test\"]\nunmanaged = []\n"
)

// zoneBody is a zone file's body: its delegate flag, its unmanaged entries
// (TOML array elements), and its records.
func zoneBody(delegate bool, unmanaged string, records ...string) string {
	d := "false"
	if delegate {
		d = "true"
	}
	b := "delegate = " + d + "\nunmanaged = [" + unmanaged + "]\n"
	if len(records) == 0 {
		return b + "record = []\n"
	}
	return b + strings.Join(records, "")
}

func forwardBody(zone, recordType, prefix, delegates string) string {
	b := "zone = \"" + zone + "\"\nrecord_type = \"" + recordType + "\"\nprefix = \"" + prefix + "\"\nunmanaged = []\n"
	if delegates != "" {
		b = "delegates = \"" + delegates + "\"\n" + b
	}
	return b
}

func TestLoadDecodesEveryFile(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, `{ collection = "record", match = { type = "CNAME", ttl = 300 }, reason = "managed by hand" }`, aRecord, txtRecord))
	declare(t, p, "test-server", "web1", serverBody)
	s := load(t, p)
	if s.Project != p || len(s.Resources) != 2 {
		t.Fatalf("set %+v", s)
	}
	srv, zone := s.Resources[0], s.Resources[1]
	if srv.Type != "test-server" || zone.Type != "test-zone" {
		t.Fatalf("resources are not sorted by type and name: %s, %s", srv.Type, zone.Type)
	}
	if zone.File != "test-zone/example.org.toml" || srv.File != "test-server/web1.toml" {
		t.Fatalf("files %q, %q", zone.File, srv.File)
	}
	want := declarations.Reference{ResourceType: "test-server", Name: "web1", Value: "ipv4"}
	if got := zone.Children["record"][0].Fields["content"]; got != want {
		t.Fatalf("reference %#v", got)
	}
	wantU := []declarations.UnmanagedEntry{{Collection: "record", Match: map[string]declarations.Value{"type": "CNAME", "ttl": int64(300)}, Reason: "managed by hand"}}
	if !reflect.DeepEqual(zone.Unmanaged, wantU) {
		t.Fatalf("unmanaged %#v", zone.Unmanaged)
	}
}

func TestLoadRefusesAnUnknownResourceTypeDirectoryAndRemovingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/hetzner-volume/data.toml", "format_version = 1\n")
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "hetzner-volume", "test-zone")
	removeFile(t, p.Root, declDir+"/hetzner-volume/data.toml")
	removeFile(t, p.Root, declDir+"/hetzner-volume")
	load(t, p)
}

func TestLoadRefusesAFileNotEndingInTOML(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/test-server/web1.yaml", "")
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-server/web1.yaml", ".toml")
}

func TestLoadRefusesAnUnknownFileAtTheTop(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/README.md", "")
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "README.md", "manifest.toml", "project.toml", "claims.toml")
}

func TestLoadReportsEveryDiagnosticOfEveryFileInOneError(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-server", "web1", "location = 1\nimage = \"x\"\nunmanaged = []\n")
	declare(t, p, "test-zone", "example.org", "delegate = false\n")
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-server/web1.toml", "test-zone/example.org.toml", "$.location", "Missing required field labels", "Missing required field record", "Missing required field unmanaged")
}

func TestLoadRefusesWhatDecodeRefusesAndTheNamedFixClearsIt(t *testing.T) {
	p := newProject(t, "example")
	bad := strings.Replace(txtRecord, "ttl = 300", "ttl = 30", 1)
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", bad))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "record[0].ttl", `write "automatic" or an integer of at least 60`)
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	load(t, p)
}

func TestLoadRefusesAFileNameThatIsNotTheNormalizedNameAndRenamingClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "Example.org", zoneBody(false, "", txtRecord))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/Example.org.toml", "test-zone/example.org.toml")
	removeFile(t, p.Root, declDir+"/test-zone/Example.org.toml")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	load(t, p)
}

func TestLoadRefusesTwoChildrenWithOneKey(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord, strings.Replace(txtRecord, "ttl = 300", "ttl = 600", 1)))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/example.org.toml", "record", "twice")
}

func TestLoadRefusesAReferenceToAnUndeclaredResourceAndDeclaringItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", aRecord))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/example.org.toml", "test-server/web1.toml")
	declare(t, p, "test-server", "web1", serverBody)
	load(t, p)
}

func TestLoadRefusesAReferenceToAValueTheTypeDoesNotExpose(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-server", "web1", serverBody)
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", strings.Replace(aRecord, `value = "ipv4"`, `value = "ipv6"`, 1)))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/example.org.toml", "ipv6", "ipv4")
}

func TestLoadRefusesASlotWhoseTargetIsUndeclaredAndDeclaringItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-mail/example.org.toml", "write test-zone/example.org.toml")
	declare(t, p, "test-zone", "example.org", zoneBody(false, ""))
	load(t, p)
}

func TestLoadRefusesAHandDeclaredChildASlotCoversAndRemovingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", mxRecord, txtRecord))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/example.org.toml", "record[0]", "test-mail/example.org")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	load(t, p)
}

func TestLoadRefusesOverlappingSlotsNamingBothSources(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, ""))
	declare(t, p, "test-forward", "spf", forwardBody("example.org", "TXT", "v=spf1", ""))
	declare(t, p, "test-forward", "spf-strict", forwardBody("example.org", "TXT", "v=spf1 -all", ""))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-forward/spf", "test-forward/spf-strict", "test-zone/example.org")
	declare(t, p, "test-forward", "spf-strict", forwardBody("example.org", "TXT", "v=DMARC1", ""))
	load(t, p)
}

func TestLoadRefusesOverlappingExactAndPrefixSlots(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, ""))
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-forward", "mx", forwardBody("example.org", "MX", "mx.", ""))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-forward/mx", "test-mail/example.org")
}

func TestLoadRefusesTwoSlotsOnOneField(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	declare(t, p, "test-forward", "ns", forwardBody("example.org", "TXT", "x", "example.org"))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-forward/ns", "test-zone/example.org", "nameservers")
}

func TestLoadRefusesAnUnmanagedEntryMatchingADeclaredChildAndRemovingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(false, `{ collection = "record", match = { type = "TXT" }, reason = "r" }`, txtRecord))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-zone/example.org.toml", "unmanaged[0]", "declared and unmanaged", "record[0]")
	declare(t, p, "test-zone", "example.org", zoneBody(false, "", txtRecord))
	load(t, p)
}

func TestLoadRefusesAnUnmanagedEntryMatchingSlotCoveredChildren(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-zone", "example.org", zoneBody(false, `{ collection = "record", match = { type = "MX" }, reason = "r" }`))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "unmanaged[0]", "derived and unmanaged", "test-mail/example.org")
}

func TestLoadRefusesMalformedUnmanagedEntries(t *testing.T) {
	cases := map[string][]string{
		`{ collection = "record", match = { type = ["TXT"] }, reason = "r" }`:   {"unmanaged[0].match.type", "a string, an integer, or a boolean"},
		`{ collection = "record", match = { type = { a = 1 } }, reason = "r" }`: {"unmanaged[0].match.type", "a string, an integer, or a boolean"},
		`{ collection = "record", match = {}, reason = "r" }`:                   {"unmanaged[0].match", "at least one"},
		`{ collection = "record", match = { color = "x" }, reason = "r" }`:      {"unmanaged[0].match.color", "type, name, content, ttl, comment"},
		`{ collection = "host", match = { type = "A" }, reason = "r" }`:         {"unmanaged[0].collection", "host", "record"},
	}
	for entry, parts := range cases {
		p := newProject(t, "example")
		declare(t, p, "test-zone", "example.org", zoneBody(false, entry))
		_, err := declarations.Load(testTool(), p)
		mustContain(t, err, append([]string{"test-zone/example.org.toml"}, parts...)...)
	}
}

func TestLoadRefusesAnUnsuppliedDerivableFieldAndEitherNamedWayClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-domain/example.org.toml", "nameservers", "declare it", "derivation")
	declare(t, p, "test-domain", "example.org", domainBody)
	load(t, p)

	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	load(t, p)
}

func TestLoadRefusesADerivableFieldDeclaredWhileDerivedAndRemovingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	declare(t, p, "test-domain", "example.org", domainBody)
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "test-domain/example.org.toml", "nameservers", "test-zone/example.org")
	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	load(t, p)
}

func TestParseReference(t *testing.T) {
	parse := func(src string) (declarations.Reference, error) {
		v, err := strictspec.LoadValue([]byte("r = "+src+"\n"), "toml")
		if err != nil {
			t.Fatal(err)
		}
		r, _ := v.Field("r")
		return declarations.ParseReference(r)
	}
	got, err := parse(`{ hetzner_server = "web1", value = "ipv4" }`)
	if err != nil || got != (declarations.Reference{ResourceType: "hetzner-server", Name: "web1", Value: "ipv4"}) {
		t.Fatalf("ParseReference = %+v, %v", got, err)
	}
	for _, bad := range []string{`{ hetzner_server = "web1" }`, `{ value = "ipv4" }`, `{ a = "x", b = "y", value = "ipv4" }`, `{ hetzner_server = 1, value = "ipv4" }`, `"web1"`, `{ hetzner_server = "", value = "ipv4" }`} {
		if _, err := parse(bad); err == nil {
			t.Errorf("ParseReference(%s) accepted", bad)
		}
	}
}

// describedServer is the server type with another descriptor.
type describedServer struct {
	serverType
	d declarations.Descriptor
}

func (s describedServer) Descriptor() declarations.Descriptor { return s.d }

func TestEveryEntryPointRefusesAnInconsistentDescriptor(t *testing.T) {
	cases := map[string]declarations.Descriptor{
		"DerivableFields": {Fields: []string{"location"}, DerivableFields: []string{"labels"}},
		"CreateOnly":      {Fields: []string{"location", "image"}, CreateOnly: []string{"image"}},
		"no key":          {Collections: []declarations.Collection{{Name: "disk"}}},
		"twice":           {Collections: []declarations.Collection{{Name: "disk", Key: []string{"id"}}, {Name: "disk", Key: []string{"id"}}}},
	}
	for what, d := range cases {
		tool := testTool()
		tool.ResourceTypes = []declarations.ResourceType{describedServer{d: d}}
		_, err := declarations.FindRoot(tool, t.TempDir())
		mustContain(t, err, "test-server", what)
	}
}
