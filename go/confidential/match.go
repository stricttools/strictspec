package confidential

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Matcher matches the entries of a list that apply to one repository.
type Matcher struct {
	list    *List
	entries []Entry
}

// List is the list the matcher was made from.
func (m *Matcher) List() *List { return m.list }

// Len is the number of entries that apply.
func (m *Matcher) Len() int { return len(m.entries) }

// Hit is one occurrence of an entry in a text.
type Hit struct {
	// ID identifies the hit across renderings of the same text: it is
	// derived from the entry, the matched text, and the words that follow
	// it on its line, ignoring case and punctuation, so the same phrase has
	// the same id in a file, a commit message, a changelog, and a Release
	// body. Resolutions name hits by it.
	ID string
	// Where is the text the hit is in, as the caller named it (a file at a
	// commit, a commit's message, a Release body). It may itself contain
	// the matched text; Location redacts it.
	Where string
	// Line and Column are 1-based; the column counts characters.
	Line, Column int
	// Matched is the text the entry matched, as written.
	Matched string
	// Context is the line around the match, the match marked >>like this<<.
	Context string
	// Entry is the entry that matched.
	Entry Entry
}

// followingWords is how many words after a match enter its hit id.
const followingWords = 6

var word = regexp.MustCompile(`[\p{L}\p{N}]+`)

// contextWidth is how many characters of the line Context keeps on each side
// of the match.
const contextWidth = 40

// Scan finds every hit in text, which where names, in line, column, and
// entry order.
func (m *Matcher) Scan(where, text string) []Hit {
	var out []Hit
	for li, line := range strings.Split(text, "\n") {
		for _, e := range m.entries {
			for _, loc := range e.re.FindAllStringIndex(line, -1) {
				if loc[0] == loc[1] {
					continue
				}
				out = append(out, Hit{
					ID:      hitID(e, line, loc[0], loc[1]),
					Where:   where,
					Line:    li + 1,
					Column:  utf8.RuneCountInString(line[:loc[0]]) + 1,
					Matched: line[loc[0]:loc[1]],
					Context: context(line, loc[0], loc[1]),
					Entry:   e,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Column < out[j].Column
	})
	return out
}

// Contains reports whether text holds any hit.
func (m *Matcher) Contains(text string) bool {
	for _, e := range m.entries {
		if e.re.MatchString(text) {
			return true
		}
	}
	return false
}

// Redact replaces every match in text with "[confidential]", so the text can
// be recorded in a committed file.
func (m *Matcher) Redact(text string) string {
	for _, e := range m.entries {
		text = e.re.ReplaceAllLiteralString(text, "[confidential]")
	}
	return text
}

// hitID derives a hit's id: the entry, the lowered matched text, and up to
// followingWords lowered words after it on the line.
func hitID(e Entry, line string, from, to int) string {
	after := word.FindAllString(line[to:], followingWords)
	for i, w := range after {
		after[i] = strings.ToLower(w)
	}
	sum := sha256.Sum256([]byte(e.key() + "\x00" + strings.ToLower(line[from:to]) + "\x00" + strings.Join(after, " ")))
	return hex.EncodeToString(sum[:])[:IDLength]
}

// IDLength is the number of hexadecimal digits of a hit id.
const IDLength = 16

var idForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidID reports whether id has the form of a hit id.
func ValidID(id string) bool { return idForm.MatchString(id) }

// context is line around the match from..to, trimmed to contextWidth
// characters on each side and the match marked.
func context(line string, from, to int) string {
	before := []rune(line[:from])
	after := []rune(line[to:])
	pre, post := "", ""
	if len(before) > contextWidth {
		before = before[len(before)-contextWidth:]
		pre = "..."
	}
	if len(after) > contextWidth {
		after = after[:contextWidth]
		post = "..."
	}
	return pre + string(before) + ">>" + line[from:to] + "<<" + string(after) + post
}
