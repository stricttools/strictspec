package declarations_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

const claimsPath = declDir + "/claims.toml"

// newAccount makes the account repository, its claims.toml holding body.
func newAccount(t *testing.T, body string) declarations.Project {
	t.Helper()
	p := newProject(t, "account")
	writeFile(t, p.Root, claimsPath, "format_version = 1\n"+body)
	return p
}

func TestLoadClaims(t *testing.T) {
	a := newAccount(t, `unmanaged = [{ resource_type = "test-server", name = "old", reason = "a manual experiment" }]

[[claim]]
resource_type = "test-zone"
name = "example.org"
project = "example"
`)
	c, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	want := declarations.Claims{
		Root:      a.Root,
		Claims:    []declarations.Claim{{ResourceType: "test-zone", Name: "example.org", Project: "example"}},
		Unmanaged: []declarations.UnmanagedResource{{ResourceType: "test-server", Name: "old", Reason: "a manual experiment"}},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("claims %+v", c)
	}
}

func TestLoadClaimsRefusesAMissingFileAndWritingItClearsIt(t *testing.T) {
	a := newProject(t, "account")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, filepath.Join(a.Root, filepath.FromSlash(claimsPath)), "format_version = 1", "claim = []", "unmanaged = []")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nclaim = []\nunmanaged = []\n")
	if _, err := declarations.LoadClaims(testTool(), a.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestLoadClaimsRefusesAMissingArray(t *testing.T) {
	a := newAccount(t, "claim = []\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "claims.toml", "Missing required field unmanaged")
}

func TestLoadClaimsRefusesAResourceNamedTwiceNamingEachPlaceAndRemovingOneClearsIt(t *testing.T) {
	a := newAccount(t, `unmanaged = [{ resource_type = "test-zone", name = "example.org", reason = "r" }]
claim = [{ resource_type = "test-zone", name = "example.org", project = "example" }, { resource_type = "test-zone", name = "example.org", project = "other" }]
`)
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "test-zone/example.org", "claim[0]", "claim[1]", "unmanaged[0]")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nunmanaged = []\nclaim = [{ resource_type = \"test-zone\", name = \"example.org\", project = \"example\" }]\n")
	if _, err := declarations.LoadClaims(testTool(), a.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestLoadClaimsRefusesAnUnknownResourceType(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"hetzner-volume\", name = \"data\", project = \"example\" }]\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "claim[0]", "hetzner-volume", "test-zone")
}

func TestLoadClaimsRefusesARelativeAccountRoot(t *testing.T) {
	_, err := declarations.LoadClaims(testTool(), "account")
	mustContain(t, err, "account", "absolute")
}

// The account repository is recognized through a symlink to it: paths are
// compared after resolving symlinks on both sides.
func TestCompareRecognizesTheAccountRepositoryThroughASymlink(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = []\n")
	declare(t, a, "test-server", "backup", serverBody)
	link := filepath.Join(t.TempDir(), "account")
	if err := os.Symlink(a.Root, link); err != nil {
		t.Fatal(err)
	}
	claims, err := declarations.LoadClaims(testTool(), link)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("backup"))
	l.put(liveServer("stray"))
	r, err := declarations.Compare(load(t, a), l, claims)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	wantLines(t, r, "differences", `{"name":"stray","resource_type":"test-server","status":"unclaimed"}`)
}

// A path that does not resolve is an error, never a guess that the
// repositories differ.
func TestCompareRefusesAnAccountRootThatDoesNotResolve(t *testing.T) {
	p := siteProject(t)
	s := load(t, p)
	claims := claimAll(t, s)
	claims.Root = filepath.Join(claims.Root, "gone")
	_, err := declarations.Compare(s, siteLive(), claims)
	mustContain(t, err, claims.Root)
}

func TestCompareRefusesClaimsWithoutAnAccountPathAndLoadClaimsClearsIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = []\n")
	s := load(t, a)
	_, err := declarations.Compare(s, newLive(), declarations.Claims{})
	mustContain(t, err, "no account repository path", "LoadClaims")
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := declarations.Compare(s, newLive(), claims); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

// An account repository's declaration directory without manifest.toml is
// refused like any declaration directory, never read as holding claims of
// nobody's; writing it with the init command the refusal names clears it.
func TestLoadClaimsRefusesAnAccountWithoutItsManifestAndInitClearsIt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, claimsPath, "format_version = 1\nclaim = []\nunmanaged = []\n")
	_, err := declarations.LoadClaims(testTool(), root)
	mustContain(t, err, filepath.Join(root, declDir, "manifest.toml")+" is missing", "infratool manifest init --project <name> in "+root)
	files, err := declarations.Init(testTool(), root, "account")
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range files {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := declarations.LoadClaims(testTool(), root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

// An account repository whose manifest.toml names another owner is refused.
func TestLoadClaimsRefusesAnAccountAnotherToolOwns(t *testing.T) {
	a := newAccount(t, "claim = []\nunmanaged = []\n")
	writeFile(t, a.Root, declDir+"/manifest.toml", "owner = \"othertool\"\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, `names the owner "othertool"`)
}
