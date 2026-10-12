package declarations_test

import (
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/declarations"
)

// usingWriter is a fakeWriter that also says which live resources use
// which, by "<type>/<name>".
type usingWriter struct {
	fakeWriter
	users map[string][]string
}

func (w *usingWriter) Users(resourceType, name string) ([]string, error) {
	return w.users[resourceType+"/"+name], nil
}

// The account repository's apply deletes a resource after every resource
// the plan deletes that uses it, whatever their names' order: web-a, which
// web-b uses, goes after web-b.
func TestResourceDeletesRunAfterTheDeletesOfTheirUsers(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = []\n")
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("web-a"))
	l.put(liveServer("web-b"))
	l.put(liveServer("web-c"))
	w := &usingWriter{fakeWriter: fakeWriter{live: l}, users: map[string][]string{"test-server/web-a": {"test-server/web-b", "test-zone/elsewhere.org"}}}
	p, err := declarations.PlanApply(load(t, a), l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describeAll(p.Changes), []string{"delete test-server/web-b", "delete test-server/web-a", "delete test-server/web-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan %q, want %q", got, want)
	}
	// Without the use, the names' order stands.
	w.users = nil
	p, err = declarations.PlanApply(load(t, a), l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describeAll(p.Changes), []string{"delete test-server/web-a", "delete test-server/web-b", "delete test-server/web-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan %q, want %q", got, want)
	}
}

// Resources the plan deletes that use each other cannot be ordered: the plan
// is refused, naming them and the fix, which is performed: declaring one of
// them without its use of the other lets the apply delete the other, and
// removing that declaration lets the next apply delete it.
func TestResourceDeletesUsingEachOtherAreRefusedAndDeclaringOneWithoutTheUseClearsIt(t *testing.T) {
	a := newAccount(t, "unmanaged = []\nclaim = []\n")
	claims, err := declarations.LoadClaims(testTool(), a.Root)
	if err != nil {
		t.Fatal(err)
	}
	l := newLive()
	l.put(liveServer("web-a"))
	l.put(liveServer("web-b"))
	w := &usingWriter{fakeWriter: fakeWriter{live: l}, users: map[string][]string{"test-server/web-a": {"test-server/web-b"}, "test-server/web-b": {"test-server/web-a"}}}
	_, err = declarations.PlanApply(load(t, a), l, claims, w, &fakeGit{})
	mustContain(t, err, "use each other", "test-server/web-a", "test-server/web-b", "declare one of them in this repository without its use of the other", "remove that declaration")
	// The fix: web-b is declared without its use of web-a, so the apply
	// keeps it and deletes web-a; removing the declaration deletes web-b.
	declare(t, a, "test-server", "web-b", serverBody)
	w.users = map[string][]string{"test-server/web-b": {"test-server/web-a"}}
	p, err := declarations.PlanApply(load(t, a), l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatalf("after the fix: %v", err)
	}
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.resources["test-server/web-a"]; ok {
		t.Fatal("web-a was not deleted")
	}
	removeFile(t, a.Root, declDir+"/test-server/web-b.toml")
	w.changes = nil
	p, err = declarations.PlanApply(load(t, a), l, claims, w, &fakeGit{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := describeAll(w.changes); !reflect.DeepEqual(got, []string{"delete test-server/web-b"}) || len(l.resources) != 0 {
		t.Fatalf("apply %q, left %v", got, l.resources)
	}
}
