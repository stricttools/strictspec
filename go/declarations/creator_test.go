package declarations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

// A claim names a project or a creator, one of them.
func TestLoadClaimsReadsACreatorClaim(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"snap\", creator = \"infratool backup\" }]\n")
	c, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if want := (declarations.Claim{ResourceType: "test-server", Name: "snap", Creator: "infratool backup"}); len(c.Claims) != 1 || c.Claims[0] != want {
		t.Fatalf("claims %+v", c.Claims)
	}
}

func TestLoadClaimsRefusesAClaimNamingNeitherProjectNorCreatorAndNamingOneClearsIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"web1\" }]\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "claim[0] names neither project nor creator", "project = ")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nunmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"web1\", project = \"example\" }]\n")
	if _, err := declarations.LoadClaims(testTool(), a.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

func TestLoadClaimsRefusesAClaimNamingProjectAndCreatorAndRemovingOneClearsIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"web1\", project = \"example\", creator = \"infratool backup\" }]\n")
	_, err := declarations.LoadClaims(testTool(), a.Root)
	mustContain(t, err, "claim[0] names both project and creator", "remove one")
	writeFile(t, a.Root, claimsPath, "format_version = 1\nunmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"web1\", creator = \"infratool backup\" }]\n")
	if _, err := declarations.LoadClaims(testTool(), a.Root); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}

// A resource its creator claimed is not unclaimed in the account repository,
// so its apply never deletes it; a project declaring it is misclaimed.
func TestACreatorClaimKeepsAResourceFromTheAccountApplyAndMisclaimsAProjectDeclaringIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = [{ resource_type = \"test-server\", name = \"snap\", creator = \"infratool backup\" }]\n")
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("snap"))
	l.put(liveServer("stray"))
	wantLines(t, verify(t, load(t, a), l, claims), "differences", `{"name":"stray","resource_type":"test-server","status":"unclaimed"}`)

	p := newProject(t, "example")
	declare(t, p, "test-server", "snap", serverBody)
	r := verify(t, load(t, p), l, claims)
	wantLines(t, r, "differences", `{"name":"snap","resource_type":"test-server","source":"test-server/snap","status":"misclaimed"}`)
	_, err = declarations.PlanApply(load(t, p), l, claims, &fakeWriter{live: l}, &fakeGit{})
	mustContain(t, err, "test-server/snap", "the command that created it")
}

// claimed writes ClaimCreated's or UnclaimCreated's bytes when they changed.
func writeClaims(t *testing.T, a declarations.Project, out []byte, changed bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		if err := os.WriteFile(declarations.ClaimFile(testTool(), a.Root), out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readClaimsFile(t *testing.T, a declarations.Project) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.Root, filepath.FromSlash(claimsPath)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A creator claims what it creates and removes the claim when it deletes
// it, in a claims.toml written either way (claim = [] inline, or [[claim]]
// tables), its comments kept; doing either again changes nothing.
func TestClaimCreatedAndUnclaimCreatedEditClaimsInPlace(t *testing.T) {
	for _, body := range []string{
		"# the account's claims\nunmanaged = []\nclaim = [] # none yet\n",
		"# the account's claims\nunmanaged = []\n\n[[claim]] # the site\nresource_type = \"test-zone\"\nname = \"example.org\"\nproject = \"example\"\n",
	} {
		a := newAccount(t, body)
		out, changed, err := declarations.ClaimCreated(testTool(), a.Root, "test-server", "4242", "infratool backup")
		writeClaims(t, a, out, changed, err)
		if !changed {
			t.Fatal("the first claim changed nothing")
		}
		claims, err := declarations.LoadClaims(testTool(), a.Root)
		if err != nil {
			t.Fatal(err)
		}
		last := claims.Claims[len(claims.Claims)-1]
		if last != (declarations.Claim{ResourceType: "test-server", Name: "4242", Creator: "infratool backup"}) {
			t.Fatalf("claims %+v", claims.Claims)
		}
		if got := readClaimsFile(t, a); !strings.Contains(got, "# the account's claims") {
			t.Fatalf("the comment is gone:\n%s", got)
		}
		_, changed, err = declarations.ClaimCreated(testTool(), a.Root, "test-server", "4242", "infratool backup")
		if err != nil || changed {
			t.Fatalf("claiming again: changed %v, %v", changed, err)
		}

		out, changed, err = declarations.UnclaimCreated(testTool(), a.Root, "test-server", "4242", "infratool backup")
		writeClaims(t, a, out, changed, err)
		if !changed {
			t.Fatal("removing the claim changed nothing")
		}
		claims, err = declarations.LoadClaims(testTool(), a.Root)
		if err != nil {
			t.Fatal(err)
		}
		for _, cl := range claims.Claims {
			if cl.Name == "4242" {
				t.Fatalf("the claim is still there: %+v", claims.Claims)
			}
		}
		if got := readClaimsFile(t, a); !strings.Contains(got, "# the account's claims") {
			t.Fatalf("the comment is gone:\n%s", got)
		}
		_, changed, err = declarations.UnclaimCreated(testTool(), a.Root, "test-server", "4242", "infratool backup")
		if err != nil || changed {
			t.Fatalf("removing again: changed %v, %v", changed, err)
		}
	}
}

// A resource listed otherwise is not the creator's to claim or unclaim.
func TestClaimCreatedRefusesAResourceListedOtherwise(t *testing.T) {
	a := newAccount(t, "unmanaged = [{ resource_type = \"test-server\", name = \"old\", reason = \"a manual experiment\" }]\nclaim = [{ resource_type = \"test-server\", name = \"web1\", project = \"example\" }, { resource_type = \"test-server\", name = \"snap\", creator = \"infratool park\" }]\n")
	_, _, err := declarations.ClaimCreated(testTool(), a.Root, "test-server", "web1", "infratool backup")
	mustContain(t, err, "claim[0]", "the project example")
	_, _, err = declarations.ClaimCreated(testTool(), a.Root, "test-server", "snap", "infratool backup")
	mustContain(t, err, "claim[1]", "its creator infratool park")
	_, _, err = declarations.ClaimCreated(testTool(), a.Root, "test-server", "old", "infratool backup")
	mustContain(t, err, "unmanaged[0]", "a manual experiment")
	_, _, err = declarations.UnclaimCreated(testTool(), a.Root, "test-server", "web1", "infratool backup")
	mustContain(t, err, "claim[0]", "the project example")
	_, _, err = declarations.ClaimCreated(testTool(), a.Root, "test-volume", "data", "infratool backup")
	mustContain(t, err, "test-volume is not a resource type")
}

// Renaming a resource renames its claim or unmanaged entry in place; a
// resource the file does not list changes nothing, and a name the file
// lists already is refused.
func TestRenameClaimRenamesAClaimOrAnUnmanagedEntry(t *testing.T) {
	a := newAccount(t, "# kept\nunmanaged = [{ resource_type = \"test-server\", name = \"old\", reason = \"by hand\" }]\n\n[[claim]]\nresource_type = \"test-server\"\nname = \"web1\"\nproject = \"example\"\n")
	for _, c := range [][2]string{{"web1", "web2"}, {"old", "older"}} {
		out, changed, err := declarations.RenameClaim(testTool(), a.Root, "test-server", c[0], c[1])
		writeClaims(t, a, out, changed, err)
		if !changed {
			t.Fatalf("renaming %s changed nothing", c[0])
		}
	}
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Claims[0].Name != "web2" || claims.Unmanaged[0].Name != "older" || !strings.Contains(readClaimsFile(t, a), "# kept") {
		t.Fatalf("claims %+v\n%s", claims, readClaimsFile(t, a))
	}
	if _, changed, err := declarations.RenameClaim(testTool(), a.Root, "test-server", "absent", "other"); err != nil || changed {
		t.Fatalf("an absent resource: changed %v, %v", changed, err)
	}
	_, _, err = declarations.RenameClaim(testTool(), a.Root, "test-server", "web2", "older")
	mustContain(t, err, "unmanaged[0] already lists test-server/older")
}
