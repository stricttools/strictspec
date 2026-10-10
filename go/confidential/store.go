package confidential

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tomledit "github.com/stricttools/go-toml-edit"
)

// The resolutions file: a repository's record of how each confidential-term
// hit in what it publishes was resolved. strictspec owns the directory (its
// manifest names strictspec); the release tool and the documentation tool
// write entries to it through their own commands and read it before they
// publish.
const (
	// StoreDir is the resolutions' directory, relative to the repository
	// root.
	StoreDir = ".strictmetadata/confidential-hits"
	// StoreFile is the resolutions file, relative to the repository root.
	StoreFile = StoreDir + "/resolutions.toml"
	// StoreManifest is the directory's ownership manifest, relative to the
	// repository root.
	StoreManifest = StoreDir + "/manifest.toml"
	// StoreFormatVersion is the resolutions format this package reads and
	// writes.
	StoreFormatVersion = 1
	// MinimumCertainty is the lowest certainty, in percent, at which a hit
	// may be judged a false positive by the agent; every other hit needs the
	// owner's approval or a fix.
	MinimumCertainty = 70
)

const storeManifestContent = "owner = \"strictspec\"\n"

// Decision is how a hit was resolved.
type Decision string

const (
	// FalsePositive is the agent's judgment, at least MinimumCertainty
	// percent certain, that the hit is not confidential.
	FalsePositive Decision = "false-positive"
	// Approved is the owner's approval to publish the hit anyway, with the
	// owner's reason.
	Approved Decision = "approved"
)

// Resolution is one recorded resolution of a hit.
type Resolution struct {
	// Hit is the hit's id.
	Hit string
	// Location is where the hit was when it was resolved, redacted so it
	// carries no confidential text.
	Location string
	Decision Decision
	// Certainty is the agent's certainty, in percent, that the hit is a
	// false positive; zero for an approval.
	Certainty int
	// Reason is one line: the agent's reason for a false positive, the
	// owner's for an approval.
	Reason string
	// Recorded is the day the resolution was recorded, as YYYY-MM-DD.
	Recorded string
}

// Writer performs the file writes the store needs. Callers back it with their
// own effects handle, so a dry run records the writes instead of making them.
type Writer interface {
	// WriteFile replaces the file at path with data.
	WriteFile(path string, data []byte) error
	// MkdirAll creates the directory at path and its parents; an existing
	// directory is not an error.
	MkdirAll(path string) error
}

type rawStore struct {
	FormatVersion int64           `toml:"format_version,required"`
	Resolutions   []rawResolution `toml:"resolutions"`
}

type rawResolution struct {
	Hit        string             `toml:"hit,required"`
	Location   string             `toml:"location,required"`
	Resolution string             `toml:"resolution,required"`
	Certainty  *int64             `toml:"certainty"`
	Reason     string             `toml:"reason,required"`
	Recorded   tomledit.LocalDate `toml:"recorded,required"`
}

// LoadResolutions reads the resolutions file of the repository at root. A
// missing file holds no resolutions.
func LoadResolutions(root string) ([]Resolution, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(StoreFile)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	rs, err := ParseResolutions(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", StoreFile, err)
	}
	return rs, nil
}

// ParseResolutions reads a resolutions file. Unknown keys, a wrong format
// version, a malformed hit id, an unknown resolution, a false positive
// without a certainty of MinimumCertainty to 100, an approval with a
// certainty, an empty or multi-line reason or location, and a hit resolved
// twice are refused, every problem at once.
func ParseResolutions(data []byte) ([]Resolution, error) {
	raw, err := tomledit.Unmarshal[rawStore](data)
	if err != nil {
		return nil, err
	}
	if raw.FormatVersion != StoreFormatVersion {
		return nil, fmt.Errorf("format_version is %d; this reader reads %d", raw.FormatVersion, StoreFormatVersion)
	}
	var problems []string
	var out []Resolution
	seen := map[string]bool{}
	for i, r := range raw.Resolutions {
		res := Resolution{
			Hit:      r.Hit,
			Location: r.Location,
			Decision: Decision(r.Resolution),
			Reason:   r.Reason,
			Recorded: fmt.Sprintf("%04d-%02d-%02d", r.Recorded.Year, r.Recorded.Month, r.Recorded.Day),
		}
		if r.Certainty != nil {
			res.Certainty = int(*r.Certainty)
		}
		for _, p := range res.problems(r.Certainty != nil) {
			problems = append(problems, fmt.Sprintf("resolution %d: %s", i+1, p))
		}
		if seen[r.Hit] {
			problems = append(problems, fmt.Sprintf("resolution %d: the hit %s is resolved more than once; keep one entry", i+1, r.Hit))
		}
		seen[r.Hit] = true
		out = append(out, res)
	}
	if len(problems) > 0 {
		return nil, errors.New("the resolutions are refused:\n  " + strings.Join(problems, "\n  "))
	}
	return out, nil
}

// problems are what is wrong with r on its own; certaintySet says whether
// the certainty was given at all.
func (r Resolution) problems(certaintySet bool) []string {
	var problems []string
	if !ValidID(r.Hit) {
		problems = append(problems, fmt.Sprintf("hit %q is not a hit id (%d lowercase hexadecimal digits)", r.Hit, IDLength))
	}
	switch r.Decision {
	case FalsePositive:
		if !certaintySet || r.Certainty < MinimumCertainty || r.Certainty > 100 {
			problems = append(problems, fmt.Sprintf("a false positive carries a certainty from %d to 100 (percent); a hit judged less certain goes to the owner", MinimumCertainty))
		}
	case Approved:
		if certaintySet {
			problems = append(problems, "an approval carries no certainty: the owner approved publishing the hit")
		}
	default:
		problems = append(problems, fmt.Sprintf("resolution %q is not one of %q and %q", r.Decision, FalsePositive, Approved))
	}
	if strings.TrimSpace(r.Reason) == "" || strings.ContainsAny(r.Reason, "\r\n") {
		problems = append(problems, "reason is one non-empty line")
	}
	if strings.TrimSpace(r.Location) == "" || strings.ContainsAny(r.Location, "\r\n") {
		problems = append(problems, "location is one non-empty line")
	}
	return problems
}

// NewFalsePositive is the agent's judgment that hit is a false positive, at
// certainty percent, for reason, recorded on the day of on. A certainty below
// MinimumCertainty is refused: that hit goes to the owner. A reason carrying
// a hit of m is refused, since the resolution is committed.
func NewFalsePositive(m *Matcher, hit Hit, certainty int, reason string, on time.Time) (Resolution, error) {
	r := Resolution{Hit: hit.ID, Location: m.Redact(hit.location()), Decision: FalsePositive, Certainty: certainty, Reason: strings.TrimSpace(reason), Recorded: on.Format("2006-01-02")}
	if certainty < MinimumCertainty || certainty > 100 {
		return Resolution{}, fmt.Errorf("a hit is judged a false positive only at a certainty from %d to 100 percent, not %d; a hit you are less certain of goes to the owner, who has it fixed or approves publishing it", MinimumCertainty, certainty)
	}
	return r, r.newProblems(m)
}

// NewApproval is the owner's approval to publish hit anyway, for the owner's
// reason, recorded on the day of on. A reason carrying a hit of m is refused,
// since the approval is committed.
func NewApproval(m *Matcher, hit Hit, reason string, on time.Time) (Resolution, error) {
	r := Resolution{Hit: hit.ID, Location: m.Redact(hit.location()), Decision: Approved, Reason: strings.TrimSpace(reason), Recorded: on.Format("2006-01-02")}
	return r, r.newProblems(m)
}

func (r Resolution) newProblems(m *Matcher) error {
	problems := r.problems(r.Decision == FalsePositive)
	if m.Contains(r.Reason) {
		problems = append(problems, "the reason carries a confidential term, and the resolution is committed; reword it without the term")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// location is where the hit is, for a resolution.
func (h Hit) location() string {
	return fmt.Sprintf("%s, line %d, column %d", h.Where, h.Line, h.Column)
}

// AddResolution writes the resolutions file of the repository at root with r
// added to existing (as LoadResolutions read them), through w, writing the
// directory's ownership manifest when it is missing. A hit resolved already
// is refused.
func AddResolution(w Writer, root string, existing []Resolution, r Resolution) error {
	for _, e := range existing {
		if e.Hit == r.Hit {
			return fmt.Errorf("the hit %s is resolved already (%s, recorded %s); %s holds one resolution per hit", r.Hit, e.Decision, e.Recorded, StoreFile)
		}
	}
	all := append(append([]Resolution(nil), existing...), r)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Hit < all[j].Hit })
	data := RenderResolutions(all)
	if _, err := ParseResolutions(data); err != nil {
		return err
	}
	dir := filepath.Join(root, filepath.FromSlash(StoreDir))
	manifest := filepath.Join(root, filepath.FromSlash(StoreManifest))
	writeManifest, err := storeManifestNeeded(manifest)
	if err != nil {
		return err
	}
	if err := w.MkdirAll(dir); err != nil {
		return err
	}
	if writeManifest {
		if err := w.WriteFile(manifest, []byte(storeManifestContent)); err != nil {
			return err
		}
	}
	return w.WriteFile(filepath.Join(root, filepath.FromSlash(StoreFile)), data)
}

// RenderResolutions renders a resolutions file holding rs, in order.
func RenderResolutions(rs []Resolution) []byte {
	var b strings.Builder
	b.WriteString("# How each confidential-term hit in what this repository publishes was\n")
	b.WriteString("# resolved: a false positive judged by the agent, or the owner's approval.\n")
	b.WriteString("# Written by the release and documentation tools' commands; never by hand.\n")
	fmt.Fprintf(&b, "format_version = %d\n", StoreFormatVersion)
	for _, r := range rs {
		b.WriteString(RenderResolution("resolutions", r))
	}
	return []byte(b.String())
}

// RenderResolution renders r as one [[table]] entry of the array named
// table, for the resolutions file and for any record that keeps resolutions
// in the same shape.
func RenderResolution(table string, r Resolution) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[[%s]]\n", table)
	fmt.Fprintf(&b, "hit = %s\n", tomledit.QuoteString(r.Hit))
	fmt.Fprintf(&b, "location = %s\n", tomledit.QuoteString(r.Location))
	fmt.Fprintf(&b, "resolution = %s\n", tomledit.QuoteString(string(r.Decision)))
	if r.Decision == FalsePositive {
		fmt.Fprintf(&b, "certainty = %d\n", r.Certainty)
	}
	fmt.Fprintf(&b, "reason = %s\n", tomledit.QuoteString(r.Reason))
	fmt.Fprintf(&b, "recorded = %s\n", r.Recorded)
	return b.String()
}

func storeManifestNeeded(path string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	d, err := tomledit.Parse(src)
	if err != nil {
		return false, fmt.Errorf("%s: %w", StoreManifest, err)
	}
	owner, err := d.GetString("owner")
	if err != nil || owner != "strictspec" {
		return false, fmt.Errorf("%s must hold owner = \"strictspec\" (strictspec owns the directory); fix the manifest by hand", StoreManifest)
	}
	return false, nil
}
