package confidential_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stricttools/strictspec/go/confidential"
)

// writeSettings writes the family's settings file at path.
func writeSettings(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheSettingsFileIsUnderTheUserConfigurationDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	got, err := confidential.SettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "stricttools", "config.toml"); got != want {
		t.Fatalf("settings path %q, want %q", got, want)
	}
}

func TestAMissingSettingsFileIsAnErrorNamingWhatToSetAndTheFixClearsIt(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(t.TempDir(), "stricttools", "config.toml")
	_, err := confidential.LocationFromSettings(settings, home)
	if err == nil {
		t.Fatal("a missing settings file gave a location")
	}
	for _, want := range []string{settings, "does not exist", "format_version = 1", `confidential_term_list = "<the list's absolute path>"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q:\n%v", want, err)
		}
	}
	// The fix the error names: write the file it shows.
	list := filepath.Join(home, "lists", "terms.toml.age")
	writeSettings(t, settings, "format_version = 1\nconfidential_term_list = \""+list+"\"\n")
	loc, err := confidential.LocationFromSettings(settings, home)
	if err != nil {
		t.Fatalf("the fix did not clear the error: %v", err)
	}
	if loc.List != list || loc.Identity != filepath.Join(home, confidential.IdentityRel) {
		t.Fatalf("location %+v", loc)
	}
	// The list the setting names does not exist: a list with no terms.
	l, err := confidential.Load(loc)
	if err != nil || !l.Missing() {
		t.Fatalf("a missing list at the configured path: missing %v, %v", l != nil && l.Missing(), err)
	}
}

func TestASettingsFileWithoutTheSettingIsAnErrorNamingIt(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "config.toml")
	writeSettings(t, settings, "format_version = 1\n")
	_, err := confidential.LocationFromSettings(settings, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "sets no confidential_term_list") || !strings.Contains(err.Error(), settings) {
		t.Fatalf("want an error naming the missing setting, got %v", err)
	}
	writeSettings(t, settings, "format_version = 1\nconfidential_term_list = \"/lists/terms.toml.age\"\n")
	if _, err := confidential.LocationFromSettings(settings, t.TempDir()); err != nil {
		t.Fatalf("adding the setting did not clear the error: %v", err)
	}
}

func TestMalformedSettingsAreRefused(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"a relative path":        {"format_version = 1\nconfidential_term_list = \"lists/terms.toml.age\"\n", "absolute path"},
		"a home-relative path":   {"format_version = 1\nconfidential_term_list = \"~/terms.toml.age\"\n", "absolute path"},
		"an empty path":          {"format_version = 1\nconfidential_term_list = \"\"\n", "absolute path"},
		"an unknown key":         {"format_version = 1\nconfidential_term_list = \"/t.age\"\nother = 1\n", "unknown key"},
		"another format":         {"format_version = 2\nconfidential_term_list = \"/t.age\"\n", "format_version"},
		"no format version":      {"confidential_term_list = \"/t.age\"\n", "format_version"},
		"a path that is no text": {"format_version = 1\nconfidential_term_list = 3\n", "confidential_term_list"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			settings := filepath.Join(t.TempDir(), "config.toml")
			writeSettings(t, settings, c.text)
			_, err := confidential.LocationFromSettings(settings, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), settings) {
				t.Fatalf("want an error naming %q and the file, got %v", c.want, err)
			}
		})
	}
}

func TestConfiguredLocationReadsTheUsersSettings(t *testing.T) {
	home := t.TempDir()
	config := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	writeSettings(t, filepath.Join(config, "stricttools", "config.toml"), "format_version = 1\nconfidential_term_list = \"/elsewhere/terms.toml.age\"\n")
	loc, err := confidential.ConfiguredLocation()
	if err != nil {
		t.Fatal(err)
	}
	if loc.List != "/elsewhere/terms.toml.age" || loc.Identity != filepath.Join(home, confidential.IdentityRel) {
		t.Fatalf("location %+v", loc)
	}
}
