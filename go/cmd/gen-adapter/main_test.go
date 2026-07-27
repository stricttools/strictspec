package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smm-h/strictspec/go/internal/render"
)

// TestMetaValidateMalformedTOMLRenders is the regression guard for the
// line-slot binding bug: parseDiag must bind the {line} slot ONLY for the
// JSONL template (which declares it), never for the JSON or TOML templates
// (which do not). Binding it on a TOML parse error makes render.Render panic
// on an unknown slot. This drives a malformed TOML document through the
// gen-adapter meta-validate path and asserts the diagnostic renders cleanly.
func TestMetaValidateMalformedTOMLRenders(t *testing.T) {
	dir := t.TempDir()
	badTOML := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badTOML, []byte("key = \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags := metaValidate(request{InputPath: badTOML})
	if len(diags) == 0 {
		t.Fatal("expected a parse diagnostic for malformed TOML, got none")
	}
	for _, d := range diags {
		// render.Render panics on an unknown slot binding; a clean render
		// proves parseDiag did not bind {line} on the TOML template.
		msg := render.Render(d)
		if d.Code == "STRICTSPEC_PARSE_TOML_SYNTAX" && !strings.Contains(msg, "TOML parse error") {
			t.Fatalf("rendered message = %q, want it to contain %q", msg, "TOML parse error")
		}
	}
}
