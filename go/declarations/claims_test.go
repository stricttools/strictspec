package declarations_test

import (
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
name = "voxlibre.org"
project = "voxlibre"
`)
	c, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	want := declarations.Claims{
		Root:      a.Root,
		Claims:    []declarations.Claim{{ResourceType: "test-zone", Name: "voxlibre.org", Project: "voxlibre"}},
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
	a := newAccount(t, `unmanaged = [{ resource_type = "test-zone", name = "voxlibre.org", reason = "r" }]
claim = [{ resource_type = "test-zone", name = "voxlibre.org", project = "voxlibre" }, { resource_type = "test-zone", name = "voxlibre.org", project = "other" }]
`)
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "test-zone/voxlibre.org", "claim[0]", "claim[1]", "unmanaged[0]")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nunmanaged = []\nclaim = [{ resource_type = \"test-zone\", name = \"voxlibre.org\", project = \"voxlibre\" }]\n")
	if _, err := declarations.LoadClaims(testTool(), a.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestLoadClaimsRefusesAnUnknownResourceType(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"hetzner-volume\", name = \"data\", project = \"voxlibre\" }]\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "claim[0]", "hetzner-volume", "test-zone")
}

func TestLoadClaimsRefusesARelativeAccountRoot(t *testing.T) {
	_, err := declarations.LoadClaims(testTool(), "account")
	mustContain(t, err, "account", "absolute")
}
