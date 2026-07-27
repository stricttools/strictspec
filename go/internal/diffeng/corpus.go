package diffeng

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
)

// ResolveGlob resolves a corpus glob ANCHORED AT root (the manifest root, per
// spec/DESIGN.md — aggregate selection determinism) and returns the matched
// files in LEXICOGRAPHIC order. A CWD-relative anchor would make verdicts depend
// on the invocation directory (banned), so the anchor is explicit.
func ResolveGlob(root, glob string) ([]string, error) {
	pattern := glob
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(root, glob)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// ContentHash is an order-independent aggregate hash over the corpus: sha256 of
// the sorted per-file content hashes, so the corpus a certificate speaks for is
// pinned and re-checkable (A.2).
func ContentHash(files []string) (string, error) {
	perFile := make([]string, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		perFile = append(perFile, hex.EncodeToString(sum[:]))
	}
	sort.Strings(perFile)
	h := sha256.New()
	for _, s := range perFile {
		h.Write([]byte(s))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
