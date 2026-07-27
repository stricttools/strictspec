package schema

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// repoRoot locates the repository root relative to this test file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	// go/internal/schema/sweep_test.go -> repo root is three levels up from go/.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// knownInvalid maps a schema filename (base) to the authoring code its file is
// DELIBERATELY authored to trigger. Every other role-bearing file under
// examples/ must read with ZERO authoring diagnostics.
var knownInvalid = map[string]string{
	"INVALID-types-transitive.toml":      "STRICTSPEC_IMPORT_TRANSITIVE",
	"INVALID-types-with-constraint.toml": "STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
	"opaque-no-stance.reject.toml":       "STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE",
	"unchecked-no-reason.reject.toml":    "STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON",
}

// TestExamplesSweep loads every schema / type-definition file under examples/
// through the meta-schema reader and asserts zero authoring diagnostics — except
// the deliberately-invalid companions, whose expected rejection is asserted.
func TestExamplesSweep(t *testing.T) {
	root := repoRoot(t)
	examples := filepath.Join(root, "examples")
	if _, err := os.Stat(examples); err != nil {
		t.Skipf("examples/ not found: %v", err)
	}

	var scanned int
	err := filepath.WalkDir(examples, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".toml" {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		dd, perr := tomldoc.Parse(src)
		if perr != nil {
			return nil // not a well-formed TOML schema; skip
		}
		role, _ := strEntry(dd.Root, "role")
		if role != "schema" && role != "type-definitions" {
			return nil // documents, migration files, manifests: not schemas
		}
		scanned++
		s, diags := ReadSchema(dd.Root, filepath.Dir(path))
		base := filepath.Base(path)
		if want, invalid := knownInvalid[base]; invalid {
			if !hasCode(diags, want) {
				t.Errorf("%s (%s): expected authoring diagnostic %s, got %v",
					base, s.Name, want, codesOf(diags))
			}
			return nil
		}
		if len(diags) != 0 {
			t.Errorf("%s (%s): expected zero authoring diagnostics, got %v",
				base, s.Name, codesOf(diags))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 30 {
		t.Fatalf("sweep scanned only %d schema files; expected the full examples/ corpus", scanned)
	}
}

func hasCode(diags []diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func codesOf(diags []diag.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.Code
	}
	return out
}
