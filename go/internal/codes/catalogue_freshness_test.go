package codes

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCatalogueFreshness regenerates the catalogue from the spec and byte-compares
// it against the committed catalogue_gen.go. Any drift — a spec edit that was not
// regenerated, or a hand-edit of the generated file — fails the test. This is the
// wavescript specgen freshness pattern: the appendix is the single source and
// hand-transcription is forbidden.
func TestCatalogueFreshness(t *testing.T) {
	committed := "catalogue_gen.go" // relative to this package dir (the test CWD)
	want, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("reading committed catalogue: %v", err)
	}

	tmp := filepath.Join(t.TempDir(), "catalogue_gen.go")
	// Run the generator; it auto-locates the spec by walking up from the CWD
	// (this package dir) and writes to the temp path.
	cmd := exec.Command("go", "run", "github.com/smm-h/strictspec/go/tools/gencodes", "-out", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running gencodes: %v\n%s", err, out)
	}

	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("reading regenerated catalogue: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("catalogue_gen.go is STALE: regenerating from the spec produced different bytes.\n" +
			"Run `go generate ./internal/codes/...` (or `go run github.com/smm-h/strictspec/go/tools/gencodes`) and commit.")
	}
}
