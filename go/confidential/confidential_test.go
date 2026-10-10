package confidential_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/stricttools/strictspec/go/confidential"
)

const sampleList = `format_version = 1

[[terms]]
term = "Portal"
added = 2026-10-10
reason = "a private project's name"
scope = { except = ["portal"] }

[[terms]]
pattern = "/home/[a-z]+"
added = 2026-10-10
reason = "a home path"
scope = "everywhere"
`

// writeEncrypted encrypts plaintext to a fresh identity under home, at the
// list's default place, and writes the identity beside it.
func writeEncrypted(t *testing.T, home, plaintext string) confidential.Location {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	loc := confidential.LocationIn(home)
	if err := os.MkdirAll(filepath.Dir(loc.List), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(plaintext)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loc.List, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loc.Identity, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return loc
}

func load(t *testing.T) *confidential.List {
	t.Helper()
	l, err := confidential.Load(writeEncrypted(t, t.TempDir(), sampleList))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAMissingListHasNoTermsAndSaysSo(t *testing.T) {
	l, err := confidential.Load(confidential.LocationIn(t.TempDir()))
	if err != nil {
		t.Fatalf("a missing list is an error: %v", err)
	}
	if !l.Missing() || len(l.Entries()) != 0 {
		t.Fatalf("missing %v, entries %v", l.Missing(), l.Entries())
	}
	if !strings.Contains(l.Status(), "does not exist, so there are no confidential terms") {
		t.Errorf("status %q", l.Status())
	}
	if m := l.For([]string{"site"}); m.Len() != 0 || len(m.Scan("x", "anything")) != 0 {
		t.Error("a missing list matched something")
	}
}

func TestTheListIsDecryptedInMemory(t *testing.T) {
	home := t.TempDir()
	loc := writeEncrypted(t, home, sampleList)
	l, err := confidential.Load(loc)
	if err != nil {
		t.Fatal(err)
	}
	es := l.Entries()
	if len(es) != 2 || es[0].Term != "Portal" || es[0].Added != "2026-10-10" || len(es[0].Except) != 1 || es[1].Pattern != "/home/[a-z]+" || len(es[1].Except) != 0 {
		t.Fatalf("entries %+v", es)
	}
	if !strings.Contains(l.Status(), "holds 2 entries") {
		t.Errorf("status %q", l.Status())
	}
	// Nothing but the encrypted list and the identity is on disk.
	var files []string
	filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 2 {
		t.Errorf("files on disk: %v", files)
	}
}

func TestAListTheIdentityCannotDecryptIsAnError(t *testing.T) {
	home := t.TempDir()
	loc := writeEncrypted(t, home, sampleList)
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loc.Identity, []byte(other.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := confidential.Load(loc); err == nil || !strings.Contains(err.Error(), "decrypting the confidential-term list") {
		t.Fatalf("want a decryption error, got %v", err)
	}
	if err := os.Remove(loc.Identity); err != nil {
		t.Fatal(err)
	}
	if _, err := confidential.Load(loc); err == nil || !strings.Contains(err.Error(), "age identity") {
		t.Fatalf("want a missing-identity error, got %v", err)
	}
}

func TestParseRefusesMalformedEntries(t *testing.T) {
	head := "format_version = 1\n\n[[terms]]\n"
	tail := "added = 2026-10-10\nreason = \"r\"\n"
	cases := map[string]string{
		"both term and pattern": head + "term = \"a\"\npattern = \"b\"\n" + tail + "scope = \"everywhere\"\n",
		"neither":               head + tail + "scope = \"everywhere\"\n",
		"empty term":            head + "term = \" \"\n" + tail + "scope = \"everywhere\"\n",
		"bad pattern":           head + "pattern = \"(\"\n" + tail + "scope = \"everywhere\"\n",
		"empty-matching":        head + "pattern = \"x*\"\n" + tail + "scope = \"everywhere\"\n",
		"unknown scope":         head + "term = \"a\"\n" + tail + "scope = \"somewhere\"\n",
		"empty except":          head + "term = \"a\"\n" + tail + "scope = { except = [] }\n",
		"unknown key":           head + "term = \"a\"\n" + tail + "scope = \"everywhere\"\ncolor = \"red\"\n",
		"missing reason":        head + "term = \"a\"\nadded = 2026-10-10\nscope = \"everywhere\"\n",
		"format version":        "format_version = 2\n",
	}
	for name, src := range cases {
		if _, err := confidential.Parse([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMatchingIgnoresCaseAndFindsSubstrings(t *testing.T) {
	m := load(t).For([]string{"site"})
	hits := m.Scan("README.md", "intro\nthe PORTAL and teleportals\npath /home/alice/x")
	if len(hits) != 3 {
		t.Fatalf("hits %+v", hits)
	}
	if hits[0].Line != 2 || hits[0].Column != 5 || hits[0].Matched != "PORTAL" || hits[0].Where != "README.md" {
		t.Errorf("first hit %+v", hits[0])
	}
	if hits[1].Column != 20 || hits[1].Matched != "portal" {
		t.Errorf("a substring inside a word was not matched: %+v", hits[1])
	}
	if hits[2].Matched != "/home/alice" || hits[2].Entry.Pattern == "" {
		t.Errorf("pattern hit %+v", hits[2])
	}
	if !strings.Contains(hits[0].Context, ">>PORTAL<<") {
		t.Errorf("context %q", hits[0].Context)
	}
}

func TestColumnsCountCharacters(t *testing.T) {
	m := load(t).For(nil)
	hits := m.Scan("x", "äöü portal")
	if len(hits) != 1 || hits[0].Column != 5 {
		t.Fatalf("hits %+v", hits)
	}
}

func TestAnExceptScopeExemptsTheNamedRepository(t *testing.T) {
	l := load(t)
	if got := l.For([]string{"Portal"}).Scan("x", "portal /home/bob"); len(got) != 1 || got[0].Entry.Pattern == "" {
		t.Fatalf("in the exempt repository: %+v", got)
	}
	if got := l.For([]string{"other", "site"}).Scan("x", "portal"); len(got) != 1 {
		t.Fatalf("in another repository: %+v", got)
	}
}

func TestTheSamePhraseHasTheSameIDInEveryRendering(t *testing.T) {
	m := load(t).For(nil)
	renderings := []string{
		`{"format_version":2,"type":"feature","description":"**New.** The Portal client, launches today"}`,
		"- **New.** the portal client launches today",
		"the `portal` client launches today!",
	}
	var ids []string
	for _, r := range renderings {
		hits := m.Scan("x", r)
		if len(hits) != 1 {
			t.Fatalf("hits in %q: %+v", r, hits)
		}
		ids = append(ids, hits[0].ID)
	}
	if ids[0] != ids[1] || ids[1] != ids[2] {
		t.Errorf("ids differ across renderings: %v", ids)
	}
	other := m.Scan("x", "the portal server launches today")[0].ID
	if other == ids[0] {
		t.Error("a different phrase has the same id")
	}
	if !confidential.ValidID(ids[0]) {
		t.Errorf("id %q is not a valid id", ids[0])
	}
}

func TestRedactRemovesEveryMatch(t *testing.T) {
	m := load(t).For(nil)
	if got := m.Redact("docs/portal-notes.md in /home/carol"); got != "docs/[confidential]-notes.md in [confidential]" {
		t.Errorf("redacted %q", got)
	}
}

type memWriter struct{ files map[string]string }

func (w *memWriter) WriteFile(p string, data []byte) error {
	w.files[p] = string(data)
	return nil
}
func (w *memWriter) MkdirAll(string) error { return nil }

func TestResolutionsAreRecordedAndResolveHits(t *testing.T) {
	m := load(t).For(nil)
	hits := m.Scan("docs/portal.md", "see the portal of the city\nthe portal launches")
	on := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

	if _, err := confidential.NewFalsePositive(m, hits[0], 69, "an ordinary word here", on); err == nil {
		t.Error("a false positive at 69 percent was accepted")
	}
	if _, err := confidential.NewFalsePositive(m, hits[0], 90, "about the portal", on); err == nil {
		t.Error("a reason naming the term was accepted")
	}
	fp, err := confidential.NewFalsePositive(m, hits[0], 90, "an ordinary word for a gate", on)
	if err != nil {
		t.Fatal(err)
	}
	if fp.Location != "docs/[confidential].md, line 1, column 9" {
		t.Errorf("location %q", fp.Location)
	}
	root := t.TempDir()
	w := &memWriter{files: map[string]string{}}
	if err := confidential.AddResolution(w, root, nil, fp); err != nil {
		t.Fatal(err)
	}
	if w.files[filepath.Join(root, ".strictmetadata/confidential-hits/manifest.toml")] != "owner = \"strictspec\"\n" {
		t.Errorf("manifest not written: %v", w.files)
	}
	written := w.files[filepath.Join(root, ".strictmetadata/confidential-hits/resolutions.toml")]
	rs, err := confidential.ParseResolutions([]byte(written))
	if err != nil {
		t.Fatalf("the written file does not read back: %v\n%s", err, written)
	}
	if len(rs) != 1 || rs[0] != fp {
		t.Fatalf("read back %+v, want %+v", rs, fp)
	}
	if err := confidential.AddResolution(w, root, rs, fp); err == nil {
		t.Error("a second resolution of the same hit was accepted")
	}

	o := confidential.Resolve(hits, rs)
	if o.Clear() || len(o.Unresolved) != 1 || len(o.Resolved) != 1 {
		t.Fatalf("outcome %+v", o)
	}
	err = o.Refusal("the push", "Resolve each hit.")
	if err == nil || !strings.Contains(err.Error(), hits[1].ID) || strings.Contains(err.Error(), hits[0].ID) || !strings.Contains(err.Error(), "Resolve each hit.") {
		t.Errorf("refusal %v", err)
	}
	ap, err := confidential.NewApproval(m, hits[1], "the owner publishes this name on purpose", on)
	if err != nil {
		t.Fatal(err)
	}
	o = confidential.Resolve(hits, append(rs, ap))
	if !o.Clear() || o.Refusal("the push", "") != nil || len(o.Used()) != 2 {
		t.Fatalf("outcome %+v", o)
	}
	if !strings.Contains(o.ReportResolved(), "90% certain") || !strings.Contains(o.ReportResolved(), "owner's approval") {
		t.Errorf("report %q", o.ReportResolved())
	}
}

func TestParseResolutionsRefusesMalformedEntries(t *testing.T) {
	entry := func(fields string) string {
		return "format_version = 1\n\n[[resolutions]]\n" + fields + "recorded = 2026-10-10\n"
	}
	cases := map[string]string{
		"bad id":             entry("hit = \"xyz\"\nlocation = \"a\"\nresolution = \"approved\"\nreason = \"r\"\n"),
		"low certainty":      entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"false-positive\"\ncertainty = 60\nreason = \"r\"\n"),
		"no certainty":       entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"false-positive\"\nreason = \"r\"\n"),
		"approval certainty": entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"approved\"\ncertainty = 80\nreason = \"r\"\n"),
		"unknown resolution": entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"ignored\"\nreason = \"r\"\n"),
		"empty reason":       entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"approved\"\nreason = \" \"\n"),
		"unknown key":        entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"approved\"\nreason = \"r\"\nnote = \"x\"\n"),
		"resolved more than once": entry("hit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"approved\"\nreason = \"r\"\n") +
			"\n[[resolutions]]\nhit = \"0123456789abcdef\"\nlocation = \"a\"\nresolution = \"approved\"\nreason = \"r\"\nrecorded = 2026-10-11\n",
	}
	for name, src := range cases {
		if _, err := confidential.ParseResolutions([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAMissingResolutionsFileHoldsNone(t *testing.T) {
	rs, err := confidential.LoadResolutions(t.TempDir())
	if err != nil || len(rs) != 0 {
		t.Fatalf("%v %v", rs, err)
	}
}

// The package writes no file itself: the decrypted list never reaches disk,
// and the resolutions are written through the caller's Writer.
func TestThePackageWritesNoFileItself(t *testing.T) {
	forbidden := map[string]bool{
		"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true,
		"Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "Remove": true,
		"RemoveAll": true, "Rename": true, "Chmod": true, "Symlink": true,
		"Link": true, "Truncate": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && forbidden[sel.Sel.Name] {
				t.Errorf("%s calls os.%s", name, sel.Sel.Name)
			}
			return true
		})
	}
}
