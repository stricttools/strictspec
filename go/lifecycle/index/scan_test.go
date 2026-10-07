package index_test

import (
	"reflect"
	"testing"

	"github.com/stricttools/strictspec/go/lifecycle/index"
)

func TestScanTermsMatchesWholeTokensIgnoringCase(t *testing.T) {
	text := "Portal ships today.\n" +
		"portals, portal-client, portal_x, myportal: none of these\n" +
		"see (PORTAL) and portal/api and \"portal\"\n" +
		"ünïcode portal end"
	got := index.ScanTerms(text, []string{"portal"})
	want := []index.Match{
		{Term: "portal", Line: 1, Column: 1},
		{Term: "portal", Line: 3, Column: 6},
		{Term: "portal", Line: 3, Column: 18},
		{Term: "portal", Line: 3, Column: 34},
		{Term: "portal", Line: 4, Column: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches %+v\nwant %+v", got, want)
	}
}

func TestScanTermsWithPunctuationInsideATerm(t *testing.T) {
	got := index.ScanTerms("module github.com/owner/portal-go v1\n", []string{"github.com/owner/portal-go", "owner"})
	want := []index.Match{
		{Term: "github.com/owner/portal-go", Line: 1, Column: 8},
		{Term: "owner", Line: 1, Column: 19},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches %+v\nwant %+v", got, want)
	}
}

func TestScanTermsIgnoresEmptyTermsAndCleanText(t *testing.T) {
	if got := index.ScanTerms("nothing to see", []string{"", "  ", "portal"}); len(got) != 0 {
		t.Fatalf("matches %+v", got)
	}
}

func TestScanTermsReportsEveryTermAtOnePlace(t *testing.T) {
	got := index.ScanTerms("Bluebird", []string{"bluebird", "BLUEBIRD"})
	if len(got) != 2 || got[0].Term != "BLUEBIRD" || got[1].Term != "bluebird" {
		t.Fatalf("matches %+v", got)
	}
}
