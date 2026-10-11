package declarations

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test's temporary directory may sit inside a repository (the test run's
// TMPDIR is inside this one), so stat hides every path outside it.
func TestFindRootRefusesOutsideAProjectAndTheFixClearsIt(t *testing.T) {
	dir := t.TempDir()
	stat = func(p string) (fs.FileInfo, error) {
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			return nil, fs.ErrNotExist
		}
		return os.Stat(p)
	}
	t.Cleanup(func() { stat = os.Stat })
	tool := Tool{Name: "www", Directory: "web-infrastructure", InitCommand: "www manifest init"}
	_, err := FindRoot(tool, dir)
	if err == nil || !strings.Contains(err.Error(), "run www from inside a project repository") {
		t.Fatalf("FindRoot outside a project: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".strictmetadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(tool, dir); err != nil || got != dir {
		t.Fatalf("FindRoot after the fix = %q, %v", got, err)
	}
}
