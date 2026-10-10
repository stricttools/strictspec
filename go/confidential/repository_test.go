package confidential_test

import (
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/confidential"
)

func TestRepositoryNames(t *testing.T) {
	got, err := confidential.RepositoryNames("/work/portal-checkout", "git@github.com:owner/portal.git")
	if err != nil || !reflect.DeepEqual(got, []string{"portal-checkout", "portal"}) {
		t.Fatalf("names %v, %v", got, err)
	}
	got, err = confidential.RepositoryNames("/work/portal", "")
	if err != nil || !reflect.DeepEqual(got, []string{"portal"}) {
		t.Fatalf("names without an origin %v, %v", got, err)
	}
	if _, err := confidential.RepositoryNames("/work/portal", "ftp://example.com/portal"); err == nil {
		t.Fatal("an unreadable origin was accepted")
	}
}

func TestNormalizeOrigin(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/portal.git":    "github.com/owner/portal",
		"https://GitHub.com/owner/portal/":       "github.com/owner/portal",
		"http://github.com/owner/portal":         "github.com/owner/portal",
		"ssh://git@github.com:22/owner/portal":   "github.com/owner/portal",
		"git@github.com:owner/portal.git":        "github.com/owner/portal",
		"github.com:owner/portal.git":            "github.com/owner/portal",
		"gp:owner/portal.git":                    "gp/owner/portal",
		"git+ssh://git@github.com/owner/portal":  "github.com/owner/portal",
		"file:///srv/remotes/portal.git":         "file:///srv/remotes/portal",
		"/srv/remotes/portal.git/":               "file:///srv/remotes/portal",
		"  https://github.com/owner/portal.git ": "github.com/owner/portal",
	}
	for in, want := range cases {
		got, err := confidential.NormalizeOrigin(in)
		if err != nil || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "portal", "../portal", "ftp://example.com/portal", "https://github.com/"} {
		if got, err := confidential.NormalizeOrigin(bad); err == nil {
			t.Errorf("NormalizeOrigin(%q) = %q, accepted", bad, got)
		}
	}
}
