package declarations_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
	"github.com/stricttools/strictspec/go/strictspec"
)

const pageSchema = `name = "test-page"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Page"
imports = [{ file = "declarations.types.toml", types = ["UnmanagedEntry"] }]

[types.Page]
type = "record"
[types.Page.fields.subject]
type = "string"
required = true
[types.Page.fields.html_file]
type = "string"
required = true
[types.Page.fields.unmanaged]
type = "array"
required = true
[types.Page.fields.unmanaged.item]
type = "UnmanagedEntry"
`

// pageType is a page whose HTML body is kept in a file beside its
// declaration.
type pageType struct{}

func (pageType) Name() string { return "test-page" }
func (pageType) Schema() (map[string]string, string) {
	return map[string]string{"test-page.schema.toml": pageSchema}, "test-page.schema.toml"
}
func (pageType) Descriptor() declarations.Descriptor {
	return declarations.Descriptor{Fields: []string{"subject", "html"}, Stage: 10,
		Companions: []declarations.Companion{{Field: "html", FileKey: "html_file", Suffix: ".html"}}}
}
func (pageType) Decode(name string, doc strictspec.Value) (declarations.Resource, error) {
	v, _ := field(doc, "subject")
	return declarations.Resource{Type: "test-page", Name: name, Fields: map[string]declarations.Value{"subject": valueOf(v)}, Children: map[string][]declarations.Item{}}, nil
}
func (pageType) Slots(declarations.Resource) []declarations.Slot { return nil }
func (pageType) Derive(declarations.Resource, declarations.LiveReader) ([]declarations.Derived, error) {
	return nil, nil
}

func pageTool() declarations.Tool {
	t := testTool()
	t.ResourceTypes = append(t.ResourceTypes, pageType{})
	return t
}

const pageBody = "subject = \"Welcome\"\nhtml_file = \"welcome.html\"\nunmanaged = []\n"

func livePage(html string) declarations.Resource {
	return declarations.Resource{Type: "test-page", Name: "welcome", Fields: M{"subject": "Welcome", "html": html}}
}

// The file a declaration names beside it is its companion field's value,
// compared with live state like any field.
func TestACompanionFileIsItsFieldsValue(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-page", "welcome", pageBody)
	writeFile(t, p.Root, declDir+"/test-page/welcome.html", "<p>Hi</p>\n")
	s, err := declarations.Load(pageTool(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Resources[0].Fields["html"]; got != "<p>Hi</p>\n" {
		t.Fatalf("html %q", got)
	}
	l := newLive()
	l.put(livePage("<p>Hi</p>\n"))
	if r := compare(t, s, l); !r.Matching {
		t.Fatalf("not matching: %+v", r.Differences)
	}
	l.put(livePage("<p>Old</p>\n"))
	wantLines(t, compare(t, s, l), "differences",
		`{"declared":"\u003cp\u003eHi\u003c/p\u003e\n","field":"html","live":"\u003cp\u003eOld\u003c/p\u003e\n","name":"welcome","resource_type":"test-page","source":"test-page/welcome","status":"changed"}`)
}

// A companion file that is missing, misnamed, or named by no declaration
// is refused, naming the fix, which clears it.
func TestCompanionFileRefusalsAndTheirFixes(t *testing.T) {
	p := newProject(t, "example")
	declare(t, p, "test-page", "welcome", pageBody)
	_, err := declarations.Load(pageTool(), p)
	mustContain(t, err, "test-page/welcome.toml names test-page/welcome.html, which does not exist: write the html there")
	writeFile(t, p.Root, declDir+"/test-page/welcome.html", "<p>Hi</p>\n")
	if _, err := declarations.Load(pageTool(), p); err != nil {
		t.Fatalf("after writing it: %v", err)
	}

	declare(t, p, "test-page", "welcome", "subject = \"Welcome\"\nhtml_file = \"body.html\"\nunmanaged = []\n")
	_, err = declarations.Load(pageTool(), p)
	mustContain(t, err, `html_file is "body.html": write html_file = "welcome.html"`)
	declare(t, p, "test-page", "welcome", pageBody)
	if _, err := declarations.Load(pageTool(), p); err != nil {
		t.Fatalf("after naming it: %v", err)
	}

	writeFile(t, p.Root, declDir+"/test-page/stray.html", "<p>?</p>\n")
	_, err = declarations.Load(pageTool(), p)
	mustContain(t, err, "test-page/stray.html", "no declaration names this one")
	removeFile(t, p.Root, declDir+"/test-page/stray.html")
	if _, err := declarations.Load(pageTool(), p); err != nil {
		t.Fatalf("after removing it: %v", err)
	}
}

// Import writes the declaration naming the file, and the file holding the
// live value; the two load and match.
func TestImportWritesACompanionFile(t *testing.T) {
	p := newProject(t, "example")
	set, err := declarations.LoadForImport(pageTool(), p, "test-page", "welcome")
	if err != nil {
		t.Fatal(err)
	}
	live := livePage("<p>Hi</p>\n")
	b, err := declarations.RenderImport(set, live)
	if err != nil {
		t.Fatal(err)
	}
	files, err := declarations.RenderImportCompanions(set, live)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, map[string][]byte{"test-page/welcome.html": []byte("<p>Hi</p>\n")}) {
		t.Fatalf("companions %q", files)
	}
	writeFile(t, p.Root, declDir+"/test-page/welcome.toml", string(b))
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(p.Root, declDir, rel), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := declarations.Load(pageTool(), p)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	l := newLive()
	l.put(live)
	if r := compare(t, s, l); !r.Matching {
		t.Fatalf("not matching: %+v", r.Differences)
	}
}
