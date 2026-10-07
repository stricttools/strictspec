package index

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// scpOrigin is git's scp-like remote syntax, [user@]host:path, which git
// reads when no slash comes before the first colon: the host may be an SSH
// alias from ~/.ssh/config.
var scpOrigin = regexp.MustCompile(`^(?:[^/@:]+@)?([^:/@]+):(.+)$`)

// NormalizeOrigin reduces a git remote URL to one form, so the spellings of
// one remote agree:
//
//   - https://github.com/owner/repo.git, ssh://git@github.com/owner/repo, and
//     git@github.com:owner/repo.git all become github.com/owner/repo (the host
//     lowercased, user and port dropped, a trailing .git and slashes removed),
//     and an SSH alias's gp:owner/repo becomes gp/owner/repo;
//   - file:///srv/repo.git and the absolute path /srv/repo.git become
//     file:///srv/repo.
//
// A relative path, an empty origin, and an unsupported scheme are refused.
func NormalizeOrigin(origin string) (string, error) {
	o := strings.TrimSpace(origin)
	if o == "" {
		return "", fmt.Errorf("the origin URL is empty")
	}
	if m := scpOrigin.FindStringSubmatch(o); m != nil && !strings.Contains(o, "://") {
		return joinHostPath(m[1], m[2])
	}
	if strings.HasPrefix(o, "/") {
		return "file://" + trimRepoPath(path.Clean(o)), nil
	}
	u, err := url.Parse(o)
	if err != nil {
		return "", fmt.Errorf("origin %q is not a git remote URL: %w", origin, err)
	}
	switch u.Scheme {
	case "https", "http", "ssh", "git", "git+ssh":
		return joinHostPath(u.Hostname(), u.Path)
	case "file":
		if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
			return "", fmt.Errorf("origin %q names no absolute path", origin)
		}
		return "file://" + trimRepoPath(path.Clean(u.Path)), nil
	case "":
		return "", fmt.Errorf("origin %q is a relative path; use the remote's URL or absolute path", origin)
	default:
		return "", fmt.Errorf("origin %q has the scheme %q, which the index does not read; use https, ssh, git, or file", origin, u.Scheme)
	}
}

func joinHostPath(host, p string) (string, error) {
	host = strings.ToLower(host)
	p = trimRepoPath(strings.Trim(p, "/"))
	if host == "" || p == "" {
		return "", fmt.Errorf("origin host %q path %q names no repository", host, p)
	}
	return host + "/" + p, nil
}

func trimRepoPath(p string) string {
	p = strings.TrimRight(p, "/")
	return strings.TrimSuffix(p, ".git")
}
