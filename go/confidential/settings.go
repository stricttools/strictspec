package confidential

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	tomledit "github.com/stricttools/go-toml-edit"
)

// The family's settings file is a TOML file in the user's configuration
// directory (os.UserConfigDir), shared by every tool of the family:
//
//	format_version = 1
//	confidential_term_list = "/absolute/path/of/terms.toml.age"
//
// confidential_term_list names the confidential-term list. Nothing is filled
// in for a missing file or a missing setting: each is an error naming what to
// write. A list that does not exist at the path the setting names is a list
// with no terms (see Load).
const (
	// SettingsRel is the settings file's path relative to the user's
	// configuration directory.
	SettingsRel = "stricttools/config.toml"
	// SettingsFormatVersion is the settings format this package reads.
	SettingsFormatVersion = 1
	// TermListSetting is the setting naming the list's absolute path.
	TermListSetting = "confidential_term_list"
)

type rawSettings struct {
	FormatVersion *int64  `toml:"format_version"`
	TermList      *string `toml:"confidential_term_list"`
}

// SettingsPath is the family's settings file in the user's configuration
// directory.
func SettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the family's settings file, which names the confidential-term list: %w", err)
	}
	return filepath.Join(dir, filepath.FromSlash(SettingsRel)), nil
}

// ConfiguredLocation is the list the family's settings file names, and the
// identity under the user's home directory.
func ConfiguredLocation() (Location, error) {
	settings, err := SettingsPath()
	if err != nil {
		return Location{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Location{}, fmt.Errorf("locating the age identity that decrypts the confidential-term list: %w", err)
	}
	return LocationFromSettings(settings, home)
}

// LocationFromSettings is the list the settings file at settings names, and
// the identity under home. A missing file, a file without the setting, a
// path that is not absolute, an unknown key, and another format version are
// refused, each naming the file and what to write.
func LocationFromSettings(settings, home string) (Location, error) {
	example := fmt.Sprintf("    format_version = %d\n    %s = \"<the list's absolute path>\"", SettingsFormatVersion, TermListSetting)
	data, err := os.ReadFile(settings)
	if errors.Is(err, fs.ErrNotExist) {
		return Location{}, fmt.Errorf("the confidential-term list's location is a setting, and the family's settings file %s does not exist. Create it holding:\n%s", settings, example)
	}
	if err != nil {
		return Location{}, fmt.Errorf("reading the family's settings file: %w", err)
	}
	raw, err := tomledit.Unmarshal[rawSettings](data)
	if err != nil {
		return Location{}, fmt.Errorf("the family's settings file %s is refused: %w. It holds:\n%s", settings, err, example)
	}
	if raw.FormatVersion == nil || *raw.FormatVersion != SettingsFormatVersion {
		return Location{}, fmt.Errorf("the family's settings file %s must hold format_version = %d, the format this reader reads. It holds:\n%s", settings, SettingsFormatVersion, example)
	}
	if raw.TermList == nil {
		return Location{}, fmt.Errorf("the family's settings file %s sets no %s, which names the confidential-term list. Add:\n    %s = \"<the list's absolute path>\"", settings, TermListSetting, TermListSetting)
	}
	if !filepath.IsAbs(*raw.TermList) {
		return Location{}, fmt.Errorf("%s in the family's settings file %s is %q; it must be an absolute path (no ~, nothing relative)", TermListSetting, settings, *raw.TermList)
	}
	return Location{
		List:     filepath.Clean(*raw.TermList),
		Identity: filepath.Join(home, filepath.FromSlash(IdentityRel)),
	}, nil
}
