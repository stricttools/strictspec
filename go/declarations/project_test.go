package declarations_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

func TestFindRootWalksUpToTheNearestStrictmetadata(t *testing.T) {
	outer := t.TempDir()
	writeFile(t, outer, ".strictmetadata/options/manifest.toml", "owner = \"strictspec\"\n")
	inner := filepath.Join(outer, "tools", "inner")
	writeFile(t, inner, ".strictmetadata/options/manifest.toml", "owner = \"strictspec\"\n")
	deep := filepath.Join(inner, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := declarations.FindRoot(testTool(), deep)
	if err != nil || got != inner {
		t.Fatalf("FindRoot = %q, %v; want %q", got, err, inner)
	}
	got, err = declarations.FindRoot(testTool(), outer)
	if err != nil || got != outer {
		t.Fatalf("FindRoot from the root = %q, %v", got, err)
	}
}

func TestFindProjectRefusesAMissingManifestNamingInitAndInitClearsIt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".strictmetadata/options/manifest.toml", "owner = \"strictspec\"\n")
	_, err := declarations.FindProject(testTool(), root)
	mustContain(t, err, declDir+"/manifest.toml", "infratool manifest init --project <name>")

	files, err := declarations.Init(testTool(), root, "example")
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range files {
		writeFile(t, "/", p, string(b))
	}
	p, err := declarations.FindProject(testTool(), filepath.Join(root))
	if err != nil {
		t.Fatalf("FindProject after Init: %v", err)
	}
	if p.Root != root || p.Name != "example" {
		t.Fatalf("project %+v", p)
	}
}

func TestInitReturnsTheManifestAndTheProjectFile(t *testing.T) {
	root := t.TempDir()
	files, err := declarations.Init(testTool(), root, "example")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".strictmetadata", "web-infrastructure")
	want := map[string]string{
		filepath.Join(dir, "manifest.toml"): "owner = \"infratool\"\n",
		filepath.Join(dir, "project.toml"):  "format_version = 1\nname = \"example\"\n",
	}
	if len(files) != len(want) {
		t.Fatalf("files %v", files)
	}
	for p, w := range want {
		if string(files[p]) != w {
			t.Fatalf("%s = %q, want %q", p, files[p], w)
		}
	}
}

func TestInitRefusesABadProjectName(t *testing.T) {
	for _, name := range []string{"", "Example", "-example", "an example"} {
		_, err := declarations.Init(testTool(), t.TempDir(), name)
		mustContain(t, err, "^[a-z0-9][a-z0-9-]*$")
	}
}

func TestInitRefusesAnExistingDirectory(t *testing.T) {
	p := newProject(t, "example")
	_, err := declarations.Init(testTool(), p.Root, "example")
	mustContain(t, err, filepath.Join(p.Root, ".strictmetadata", "web-infrastructure"), "exists")
}

func TestFindProjectRefusesAnotherOwner(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/manifest.toml", "owner = \"othertool\"\n")
	_, err := declarations.FindProject(testTool(), p.Root)
	mustContain(t, err, "othertool", "infratool")
}

func TestFindProjectRefusesAManifestWritingFormatVersionAndRemovingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/manifest.toml", "format_version = 1\nowner = \"infratool\"\n")
	_, err := declarations.FindProject(testTool(), p.Root)
	mustContain(t, err, "format_version", "remove it")
	writeFile(t, p.Root, declDir+"/manifest.toml", "owner = \"infratool\"\n")
	if _, err := declarations.FindProject(testTool(), p.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestFindProjectRefusesAnInvalidManifest(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/manifest.toml", "owner = \"infratool\"\nextra = 1\n")
	_, err := declarations.FindProject(testTool(), p.Root)
	mustContain(t, err, "manifest.toml", "STRICTSPEC_")
}

func TestFindProjectRefusesAMissingProjectFileAndWritingItClearsIt(t *testing.T) {
	p := newProject(t, "example")
	removeFile(t, p.Root, declDir+"/project.toml")
	_, err := declarations.FindProject(testTool(), p.Root)
	mustContain(t, err, "project.toml", "format_version = 1", "name = ")
	writeFile(t, p.Root, declDir+"/project.toml", "format_version = 1\nname = \"example\"\n")
	if _, err := declarations.FindProject(testTool(), p.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestFindProjectRefusesAnInvalidProjectFile(t *testing.T) {
	p := newProject(t, "example")
	writeFile(t, p.Root, declDir+"/project.toml", "format_version = 1\nname = \"An Example\"\n")
	_, err := declarations.FindProject(testTool(), p.Root)
	mustContain(t, err, "project.toml", "STRICTSPEC_")
}

func TestFindProjectFromASubdirectory(t *testing.T) {
	p := newProject(t, "example")
	sub := filepath.Join(p.Root, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := declarations.FindProject(testTool(), sub)
	if err != nil || got != p {
		t.Fatalf("FindProject = %+v, %v", got, err)
	}
}
