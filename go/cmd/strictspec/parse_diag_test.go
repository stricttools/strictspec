package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateMalformedJSONRendersMessage and TestValidateMalformedTOMLRendersMessage
// are the CLI-path regression guards for the line-slot binding bug. parseDiag
// must bind the {line} slot ONLY for the JSONL template; binding it on a JSON
// or TOML parse error makes render.Render fail on an unknown slot. These drive a
// malformed document through `strictspec validate` and assert the rendered
// diagnostic carries the parse-error template text (not a bare code fallback).

func TestValidateMalformedJSONRendersMessage(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(fixturesSchemas(t), "shared-canvas.toml")
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newApp().Test([]string{"validate", schema, badJSON, "--structural-only"})
	if r.ExitCode == 0 {
		t.Fatalf("validate of malformed JSON must fail, exit = 0\nstderr: %s", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "JSON parse error") {
		t.Fatalf("stderr missing rendered parse message %q:\n%s", "JSON parse error", r.Stderr)
	}
}

func TestValidateMalformedTOMLRendersMessage(t *testing.T) {
	dir := t.TempDir()
	schema := filepath.Join(fixturesSchemas(t), "shared-canvas.toml")
	badTOML := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badTOML, []byte("key = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newApp().Test([]string{"validate", schema, badTOML, "--structural-only"})
	if r.ExitCode == 0 {
		t.Fatalf("validate of malformed TOML must fail, exit = 0\nstderr: %s", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "TOML parse error") {
		t.Fatalf("stderr missing rendered parse message %q:\n%s", "TOML parse error", r.Stderr)
	}
}
