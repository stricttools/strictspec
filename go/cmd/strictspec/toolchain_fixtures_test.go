package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/internal/fixturetoml"
)

// The toolchain fixtures under testdata/fixtures/_toolchain/<name>/ exercise the
// CLI analyses (migrate, diff, doc-diff): each directory is self-contained (the
// schemas, migration files, documents, corpus, and expected output sit beside
// fixture.toml), and the expected outcome (exit code, output substrings, and
// byte-exact rewritten files) is hand-authored from the spec, never regenerated
// from the tool.

func toolchainFixturesRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures", "_toolchain"))
}

type toolchainFixture struct {
	dir  string
	name string
	kind string
	spec map[string]any
}

func loadToolchainFixtures(t *testing.T) []toolchainFixture {
	t.Helper()
	root := toolchainFixturesRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []toolchainFixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		spec, err := fixturetoml.Load(filepath.Join(dir, "fixture.toml"))
		if err != nil {
			t.Fatalf("toolchain fixture %s: %v", e.Name(), err)
		}
		name, _ := spec["name"].(string)
		kind, _ := spec["kind"].(string)
		if name == "" {
			t.Fatalf("toolchain fixture %s: missing name", e.Name())
		}
		switch kind {
		case "migration", "diff", "doc-diff":
		default:
			t.Fatalf("toolchain fixture %s: kind %q is not migration, diff, or doc-diff", e.Name(), kind)
		}
		if _, ok := spec["expected"].(map[string]any); !ok {
			t.Fatalf("toolchain fixture %s: missing [expected] table", e.Name())
		}
		out = append(out, toolchainFixture{dir: dir, name: name, kind: kind, spec: spec})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func specString(spec map[string]any, key string) string {
	s, _ := spec[key].(string)
	return s
}

func specStrings(t *testing.T, v any) []string {
	t.Helper()
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("expected an array of strings, got %T", v)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("expected an array of strings, got element %T", item)
		}
		out = append(out, s)
	}
	return out
}

func (f toolchainFixture) argv(t *testing.T) []string {
	t.Helper()
	s := f.spec
	switch f.kind {
	case "migration":
		to, ok := s["to"].(int64)
		if !ok {
			t.Fatalf("%s: to must be an integer", f.name)
		}
		migrations := specString(s, "migrations")
		if migrations == "" {
			migrations = "."
		}
		argv := append([]string{"migrate", specString(s, "schema")}, specStrings(t, s["inputs"])...)
		argv = append(argv, "--to", fmt.Sprint(to), "--migrations", migrations)
		if dry, _ := s["dry_run"].(bool); dry {
			argv = append(argv, "--dry-run")
		}
		return argv
	case "diff":
		corpusRoot := specString(s, "corpus_subdir")
		if corpusRoot == "" {
			corpusRoot = "."
		}
		argv := []string{"diff", specString(s, "old_schema"), specString(s, "new_schema"),
			"--corpus", specString(s, "corpus"), "--corpus-root", corpusRoot}
		if m := specString(s, "migration"); m != "" {
			argv = append(argv, "--migration", m)
		}
		if a := specString(s, "adjudication"); a != "" {
			argv = append(argv, "--adjudication", a)
		}
		if same, _ := s["same_version"].(bool); same {
			argv = append(argv, "--same-version")
		}
		return argv
	default: // doc-diff
		return []string{"doc-diff", specString(s, "schema"), specString(s, "old_document"), specString(s, "new_document")}
	}
}

// copyTree copies the fixture directory into dst, so a migration rewriting its
// input in place never touches the committed fixture.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestToolchainFixtures(t *testing.T) {
	fixtures := loadToolchainFixtures(t)
	kinds := map[string]bool{}
	for _, f := range fixtures {
		kinds[f.kind] = true
	}
	if len(fixtures) < 8 || len(kinds) != 3 {
		t.Fatalf("expected the toolchain fixtures of every kind, found %d covering %v", len(fixtures), kinds)
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			work := t.TempDir()
			copyTree(t, f.dir, work)
			t.Chdir(work)
			r := newApp().Test(f.argv(t))

			exp := f.spec["expected"].(map[string]any)
			if code, ok := exp["exit_code"].(int64); ok && int64(r.ExitCode) != code {
				t.Fatalf("exit code %d, want %d\nSTDOUT:\n%s\nSTDERR:\n%s", r.ExitCode, code, r.Stdout, r.Stderr)
			}
			for _, needle := range specStrings(t, exp["stdout_contains"]) {
				if !strings.Contains(r.Stdout, needle) {
					t.Errorf("stdout missing %q\nSTDOUT:\n%s", needle, r.Stdout)
				}
			}
			for _, needle := range specStrings(t, exp["stderr_contains"]) {
				if !strings.Contains(r.Stderr, needle) {
					t.Errorf("stderr missing %q\nSTDERR:\n%s", needle, r.Stderr)
				}
			}
			if eq, ok := exp["output_equals"].(map[string]any); ok {
				for produced, expectedFile := range eq {
					got, err := os.ReadFile(filepath.Join(work, produced))
					if err != nil {
						t.Fatal(err)
					}
					want, err := os.ReadFile(filepath.Join(work, expectedFile.(string)))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Errorf("%s differs from %s:\n--- got ---\n%s\n--- want ---\n%s", produced, expectedFile, got, want)
					}
				}
			}
		})
	}
}
