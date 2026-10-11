package declarations_test

import (
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

func TestRenderImportWritesAFileThatLoadsAndMatches(t *testing.T) {
	p := newProject(t, "voxlibre")
	l := newLive()
	z := liveZone("voxlibre.org", false,
		record("TXT", "voxlibre.org", "say \"hi\"\\", int64(300), ""),
		record("A", "voxlibre.org", "203.0.113.7", "automatic", "the site"))
	l.put(z)
	b, err := declarations.RenderImport(load(t, p), z)
	if err != nil {
		t.Fatal(err)
	}
	want := `# Imported from live state by www: review every value before committing it.
format_version = 1
delegate = false
unmanaged = []

[[record]]
type = "A"
name = "voxlibre.org"
content = "203.0.113.7"
ttl = "automatic"
comment = "the site"

[[record]]
type = "TXT"
name = "voxlibre.org"
content = "say \"hi\"\\"
ttl = 300
comment = ""
`
	if string(b) != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", b, want)
	}
	writeFile(t, p.Root, declDir+"/test-zone/voxlibre.org.toml", string(b))
	s := load(t, p)
	if r := compare(t, s, l); !r.Matching {
		t.Fatalf("the imported file does not match live state: %+v", r.Differences)
	}
}

func TestRenderImportOmitsCreateOnlyAndUndescribedFields(t *testing.T) {
	p := newProject(t, "voxlibre")
	srv := liveServer("gamehome")
	srv.Fields["image"] = "debian-13"
	b, err := declarations.RenderImport(load(t, p), srv)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Imported from live state by www: review every value before committing it.\nformat_version = 1\nlocation = \"fsn1\"\nlabels = { role = \"game\" }\nunmanaged = []\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}

	mail := declarations.Resource{Type: "test-mail", Name: "voxlibre.org", Fields: M{"plan": "micro", "dkim_key": "k=1"}, Children: map[string][]declarations.Item{
		"mailbox": {{Fields: M{"local_part": "me", "name": "Me", "invitation": "me@elsewhere.test"}}},
	}}
	b, err = declarations.RenderImport(load(t, p), mail)
	if err != nil {
		t.Fatal(err)
	}
	want = "# Imported from live state by www: review every value before committing it.\nformat_version = 1\nplan = \"micro\"\nunmanaged = []\n\n[[mailbox]]\nlocal_part = \"me\"\nname = \"Me\"\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}
}

func TestRenderImportOmitsWhatASlotCovers(t *testing.T) {
	p := newProject(t, "voxlibre")
	declare(t, p, "test-zone", "voxlibre.org", zoneBody(true, ""))
	declare(t, p, "test-mail", "voxlibre.org", mailBody)
	declare(t, p, "test-domain", "voxlibre.org", "auto_renew = true\nunmanaged = []\n")
	s := load(t, p)

	z := liveZone("voxlibre.org", true,
		record("MX", "voxlibre.org", "mx.mail.test", int64(300), ""),
		record("TXT", "dkim._domainkey.voxlibre.org", "k=1", "automatic", ""),
		record("TXT", "voxlibre.org", "hello", int64(300), ""))
	b, err := declarations.RenderImport(s, z)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Imported from live state by www: review every value before committing it.\nformat_version = 1\ndelegate = true\nunmanaged = []\n\n[[record]]\ntype = \"TXT\"\nname = \"voxlibre.org\"\ncontent = \"hello\"\nttl = 300\ncomment = \"\"\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}

	d := declarations.Resource{Type: "test-domain", Name: "voxlibre.org", Fields: M{"auto_renew": true, "nameservers": []V{"a.ns.test"}}}
	b, err = declarations.RenderImport(s, d)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Imported from live state by www: review every value before committing it.\nformat_version = 1\nauto_renew = true\nunmanaged = []\n"; string(b) != want {
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
	p := newProject(t, "voxlibre")
	b, err := declarations.RenderImport(load(t, p), liveZone("voxlibre.org", false))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Imported from live state by www: review every value before committing it.\nformat_version = 1\ndelegate = false\nrecord = []\nunmanaged = []\n"; string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}
}

func TestRenderImportRefusesAnUnknownResourceTypeAndBadValues(t *testing.T) {
	p := newProject(t, "voxlibre")
	_, err := declarations.RenderImport(load(t, p), declarations.Resource{Type: "hetzner-volume", Name: "data"})
	mustContain(t, err, "hetzner-volume", "test-zone")
	srv := liveServer("gamehome")
	srv.Fields["location"] = 3.5
	_, err = declarations.RenderImport(load(t, p), srv)
	mustContain(t, err, "location", "float64")
}
