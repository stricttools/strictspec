package declarations_test

import (
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

// What import could declare is every live resource no declaration covers:
// not declared in the project or the account repository, not claimed, not
// listed unmanaged, and not owned by a live resource.
func TestListImportableListsTheLiveResourcesNoDeclarationCovers(t *testing.T) {
	a := newAccount(t, "unmanaged = [{ resource_type = \"test-server\", name = \"d\", reason = \"by hand\" }]\nclaim = [{ resource_type = \"test-server\", name = \"c\", project = \"other\" }]\n")
	declare(t, a, "test-server", "a", serverBody)
	p := newProject(t, "example")
	declare(t, p, "test-server", "b", serverBody)
	claims, err := declarations.LoadClaims(ownerTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := declarations.Load(ownerTool(), p)
	if err != nil {
		t.Fatal(err)
	}
	account, err := declarations.Load(ownerTool(), a)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		l.put(liveServer(n))
	}
	l.put(liveAddress("ip-e", "e"))
	l.put(liveAddress("ip-free", ""))
	got, err := declarations.ListImportable(set, account, l, claims, []string{"test-server", "test-address"})
	if err != nil {
		t.Fatal(err)
	}
	want := []declarations.Importable{{ResourceType: "test-address", Name: "ip-free"}, {ResourceType: "test-server", Name: "e"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("importable %+v, want %+v", got, want)
	}
	// In the account repository itself, the account set is the set.
	got, err = declarations.ListImportable(account, account, l, claims, []string{"test-server"})
	if err != nil || !reflect.DeepEqual(got, []declarations.Importable{{ResourceType: "test-server", Name: "b"}, {ResourceType: "test-server", Name: "e"}}) {
		t.Fatalf("importable in the account repository %+v, %v", got, err)
	}
	_, err = declarations.ListImportable(set, account, l, claims, []string{"test-volume"})
	mustContain(t, err, "test-volume is not a resource type")
	_, err = declarations.ListImportable(set, set, l, claims, []string{"test-server"})
	mustContain(t, err, "is not the claims' account repository")
}
