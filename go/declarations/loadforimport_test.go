package declarations_test

import (
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

const importHeader = "# Imported from live state by infratool: review every value before committing it.\nformat_version = 1\n"

func TestLoadForImportAcceptsAnUndeclaredSlotTargetBeingImportedAndRenderImportOmitsWhatTheSlotCovers(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "write test-zone/example.org.toml")

	s, err := declarations.LoadForImport(testTool(), p, "test-zone", "example.org")
	if err != nil {
		t.Fatalf("LoadForImport: %v", err)
	}
	z := liveZone("example.org", false,
		record("MX", "example.org", "mx.mail.test", int64(300), ""),
		record("TXT", "dkim._domainkey.example.org", "k=1", "automatic", ""),
		record("TXT", "example.org", "hello", int64(300), ""))
	b, err := declarations.RenderImport(s, z)
	if err != nil {
		t.Fatal(err)
	}
	want := importHeader + "delegate = false\nunmanaged = []\n\n[[record]]\ntype = \"TXT\"\nname = \"example.org\"\ncontent = \"hello\"\nttl = 300\ncomment = \"\"\n"
	if string(b) != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", b, want)
	}

	// Writing the rendered file is what clears Load's refusal.
	writeFile(t, p.Root, declDir+"/test-zone/example.org.toml", string(b))
	load(t, p)
}

func TestLoadForImportOmitsADerivedFieldOfTheUndeclaredTarget(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	_, err := declarations.Load(testTool(), p)
	mustContain(t, err, "write test-domain/example.org.toml")

	s, err := declarations.LoadForImport(testTool(), p, "test-domain", "example.org")
	if err != nil {
		t.Fatalf("LoadForImport: %v", err)
	}
	d := declarations.Resource{Type: "test-domain", Name: "example.org", Fields: M{"auto_renew": true, "nameservers": []V{"a.ns.test"}}}
	b, err := declarations.RenderImport(s, d)
	if err != nil {
		t.Fatal(err)
	}
	if want := importHeader + "auto_renew = true\nunmanaged = []\n"; string(b) != want {
		t.Fatalf("rendered:\n%s", b)
	}
}

func TestLoadForImportStillRefusesAnUndeclaredSlotTargetOtherThanTheImportedOne(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-mail", "example.net", mailBody)
	_, err := declarations.LoadForImport(testTool(), p, "test-zone", "example.org")
	mustContain(t, err, "test-mail/example.net.toml", "write test-zone/example.net.toml")
	if strings.Contains(err.Error(), "write test-zone/example.org.toml") {
		t.Fatalf("the imported target was refused: %v", err)
	}
	_, err = declarations.LoadForImport(testTool(), p, "test-server", "example.org")
	mustContain(t, err, "write test-zone/example.org.toml", "write test-zone/example.net.toml")
}

func TestLoadForImportStillRefusesOverlappingSlotsOnTheImportedTarget(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-zone", "example.org", zoneBody(true, ""))
	declare(t, p, "test-forward", "relay", forwardBody("example.org", "TXT", "v=", "example.org"))
	_, err := declarations.LoadForImport(testTool(), p, "test-domain", "example.org")
	mustContain(t, err, "test-forward/relay", "test-zone/example.org", "both derive the field nameservers of test-domain/example.org")
}

func TestLoadForImportKeepsEveryOtherRefusal(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	declare(t, p, "test-server", "web1", "location = 3\n")
	_, err := declarations.LoadForImport(testTool(), p, "test-zone", "example.org")
	mustContain(t, err, "test-server/web1.toml is invalid")
}

func TestLoadForImportRefusesAResourceTypeTheToolDoesNotSupply(t *testing.T) {
	p := newProject(t, "example")
	_, err := declarations.LoadForImport(testTool(), p, "hetzner-volume", "data")
	mustContain(t, err, "hetzner-volume", "test-zone")
}

func TestCompareAndApplyRefuseASetLoadForImportReturned(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-mail", "example.org", mailBody)
	s, err := declarations.LoadForImport(testTool(), p, "test-zone", "example.org")
	if err != nil {
		t.Fatal(err)
	}
	_, err = declarations.Compare(s, newLive(), claimAll(t, s))
	mustContain(t, err, "LoadForImport", "Load")
	_, err = declarations.Apply(s, newLive(), claimAll(t, s), &fakeWriter{live: newLive()}, &fakeGit{}, "x")
	mustContain(t, err, "LoadForImport", "Load")
}
