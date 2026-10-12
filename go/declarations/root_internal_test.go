package declarations

import (
	"errors"
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
	// The refusal names the program the user runs, not the tool's owner
	// name.
	tool := Tool{Name: "infratool", Command: "infra", Directory: "infrastructure", InitCommand: "infra manifest init", DryRunCommand: "infra manifest apply --dry-run"}
	_, err := FindRoot(tool, dir)
	if err == nil || !strings.Contains(err.Error(), "run infra from inside a project repository") || !errors.Is(err, ErrNoRoot) {
		t.Fatalf("FindRoot outside a project: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".strictmetadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(tool, dir); err != nil || got != dir {
		t.Fatalf("FindRoot after the fix = %q, %v", got, err)
	}
}

// A tool names the program its users run, which the engine's messages name.
func TestCheckToolRefusesAToolNamingNoCommand(t *testing.T) {
	tool := Tool{Name: "infratool", Directory: "infrastructure", InitCommand: "infra manifest init", DryRunCommand: "infra manifest apply --dry-run"}
	if err := checkTool(tool); err == nil || !strings.Contains(err.Error(), "Tool.Command is empty") {
		t.Fatalf("err = %v", err)
	}
	tool.Command = "infra"
	if err := checkTool(tool); err != nil {
		t.Fatalf("after the fix: %v", err)
	}
}
