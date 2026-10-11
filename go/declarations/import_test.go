package declarations_test

import (
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

func TestRenderImportWritesAFileThatLoadsAndMatches(t *testing.T) {
	p := newProject(t, "example")
	l := newLive()
	z := liveZone("example.org", false,
		record("TXT", "example.org", "say \"hi\"\\", int64(300), ""),
		record("A", "example.org", "203.0.113.7", "automatic", "the site"))
	l.put(z)
	b, err := declarations.RenderImport(load(t, p), z)
	if err != nil {
		t.Fatal(err)
	}
	want := `# Imported from live state by infratool: review every value before committing it.
format_version = 1
delegate = false
unmanaged = []

[[record]]
type = "A"
name = "example.org"
content = "203.0.113.7"
ttl = "automatic"
comment = "the site"

[[record]]
type = "TXT"
name = "example.org"
content = "say \"hi\"\\"
ttl = 300
comment = ""
`
	if string(b) != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", b, want)
	}
	writeFile(t, p.Root, declDir+"/test-zone/example.org.toml", string(b))
	s := load(t, p)
	if r := compare(t, s, l); !r.Matching {
		t.Fatalf("the imported file does not match live state: %+v", r.Differences)
	}
}

func TestRenderImportOmitsCreateOnlyAndUndescribedFields(t *testing.T) {
	p := newProject(t, "example")
	srv := liveServer("web1")
	srv.Fields["image"] = "debian-13"
	b, err := declarations.RenderImport(load(t, p), srv)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\nlocation = \"fsn1\"\nlabels = { role = \"game\" }\nunmanaged = []\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}

	mail := declarations.Resource{Type: "test-mail", Name: "example.org", Fields: M{"plan": "micro", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{
		"mailbox": {{Fields: M{"local_part": "me", "name": "Me", "invitation": "me@elsewhere.test"}}},
	}}
	b, err = declarations.RenderImport(load(t, p), mail)
	if err != nil {
		t.Fatal(err)
	}
	want = "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\nplan = \"micro\"\nunmanaged = []\n\n[[mailbox]]\nlocal_part = \"me\"\nname = \"Me\"\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}
}

func TestRenderImportOmitsWhatASlotCovers(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-domain", "example.org", "auto_renew = true\nunmanaged = []\n")
	s := load(t, p)

	z := liveZone("example.org", true,
		record("MX", "example.org", "mx.mail.test", int64(300), ""),
		record("TXT", "dkim._domainkey.example.org", "k=1", "automatic", ""),
		record("TXT", "example.org", "hello", int64(300), ""))
	b, err := declarations.RenderImport(s, z)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\ndelegate = true\nunmanaged = []\n\n[[record]]\ntype = \"TXT\"\nname = \"example.org\"\ncontent = \"hello\"\nttl = 300\ncomment = \"\"\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}

	d := declarations.Resource{Type: "test-domain", Name: "example.org", Fields: M{"auto_renew": true, "nameservers": []V{"a.ns.test"}}}
	b, err = declarations.RenderImport(s, d)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\nauto_renew = true\nunmanaged = []\n"; string(b) != want {
		t.Fatalf("a derived field was rendered:\n%s", b)
	}
	d.Name = "other.org"
	b, err = declarations.RenderImport(s, d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "nameservers = [\"a.ns.test\"]") {
		t.Fatalf("a field no slot covers was left out:\n%s", b)
	}
}

func TestRenderImportRendersAnEmptyCollection(t *testing.T) {
	p := newProject(t, "example")
	b, err := declarations.RenderImport(load(t, p), liveZone("example.org", false))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\ndelegate = false\nrecord = []\nunmanaged = []\n"; string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}
}

func TestRenderImportRefusesAnUnknownResourceTypeAndBadValues(t *testing.T) {
	p := newProject(t, "example")
	_, err := declarations.RenderImport(load(t, p), declarations.Resource{Type: "hetzner-volume", Name: "data"})
	mustContain(t, err, "hetzner-volume", "test-zone")
	srv := liveServer("web1")
	srv.Fields["location"] = 3.5
	_, err = declarations.RenderImport(load(t, p), srv)
	mustContain(t, err, "location", "float64")
}
