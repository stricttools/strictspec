package declarations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tomledit "github.com/stricttools/go-toml-edit"
)

// ClaimFile is the path of the account repository's claims.toml, which
// ClaimCreated and UnclaimCreated return the new bytes of.
func ClaimFile(tool Tool, accountRoot string) string {
	return filepath.Join(dirAbs(tool, accountRoot), claimsFile)
}

// ClaimCreated returns the bytes of the account repository's claims.toml
// with a claim of the resource for creator, the command that created it and
// owns it (nothing declares it), and whether they differ from the file's: a
// resource already claimed for creator changes nothing, so a creator resuming
// its work may claim again. A resource the file lists otherwise (claimed for
// a project or another creator, or listed unmanaged) is refused, naming the
// entry. The file is edited in place: its comments and layout are kept. The
// tool writes the bytes, which the account repository's apply needs
// committed.
func ClaimCreated(tool Tool, accountRoot, resourceType, name, creator string) ([]byte, bool, error) {
	c, src, err := readClaims(tool, accountRoot, resourceType, creator)
	if err != nil {
		return nil, false, err
	}
	file := ClaimFile(tool, accountRoot)
	for i, cl := range c.Claims {
		if cl.ResourceType != resourceType || cl.Name != name {
			continue
		}
		if cl.Creator == creator {
			return src, false, nil
		}
		return nil, false, fmt.Errorf("%s: claim[%d] already claims %s/%s for %s, so %s cannot claim it as its creator: a resource has one owner", file, i, resourceType, name, claimOwner(cl), creator)
	}
	for i, u := range c.Unmanaged {
		if u.ResourceType == resourceType && u.Name == name {
			return nil, false, fmt.Errorf("%s: unmanaged[%d] lists %s/%s as nobody's (%s), so %s cannot claim it as its creator: remove that entry if %s owns it", file, i, resourceType, name, u.Reason, creator, creator)
		}
	}
	doc, err := tomledit.Parse(src)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", file, err)
	}
	entry := []tomledit.Pair{{Key: "resource_type", Value: resourceType}, {Key: "name", Value: name}, {Key: "creator", Value: creator}}
	if err := doc.NewArrayTable("claim"); err == nil {
		for _, p := range entry {
			if err := doc.Set("claim[-1]."+p.Key, p.Value); err != nil {
				return nil, false, fmt.Errorf("%s: %w", file, err)
			}
		}
	} else {
		// claim is written as an inline array (claim = [] when empty): the
		// new entry is appended to it as an inline table.
		var list []any
		for _, cl := range c.Claims {
			owner := tomledit.Pair{Key: "project", Value: cl.Project}
			if cl.Creator != "" {
				owner = tomledit.Pair{Key: "creator", Value: cl.Creator}
			}
			list = append(list, []tomledit.Pair{{Key: "resource_type", Value: cl.ResourceType}, {Key: "name", Value: cl.Name}, owner})
		}
		list = append(list, entry)
		if err := doc.Set("claim", list); err != nil {
			return nil, false, fmt.Errorf("%s: %w", file, err)
		}
	}
	return checkedClaims(tool, accountRoot, file, doc.Bytes())
}

// UnclaimCreated returns the bytes of the account repository's claims.toml
// without the claim of the resource for creator, and whether they differ
// from the file's: a resource the file does not claim changes nothing (the
// creator deleting what it created removes its claim, and may run again). A
// claim of the resource for a project or another creator is refused, naming
// it. The file is edited in place: its comments and layout are kept.
func UnclaimCreated(tool Tool, accountRoot, resourceType, name, creator string) ([]byte, bool, error) {
	c, src, err := readClaims(tool, accountRoot, resourceType, creator)
	if err != nil {
		return nil, false, err
	}
	file := ClaimFile(tool, accountRoot)
	for i, cl := range c.Claims {
		if cl.ResourceType != resourceType || cl.Name != name {
			continue
		}
		if cl.Creator != creator {
			return nil, false, fmt.Errorf("%s: claim[%d] claims %s/%s for %s, not for %s, so %s does not remove it", file, i, resourceType, name, claimOwner(cl), creator, creator)
		}
		doc, err := tomledit.Parse(src)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", file, err)
		}
		if err := doc.Delete(fmt.Sprintf("claim[%d]", i)); err != nil {
			return nil, false, fmt.Errorf("%s: %w", file, err)
		}
		return checkedClaims(tool, accountRoot, file, doc.Bytes())
	}
	return src, false, nil
}

// readClaims reads and checks claims.toml for a creator's edit.
func readClaims(tool Tool, accountRoot, resourceType, creator string) (Claims, []byte, error) {
	if creator == "" {
		return Claims{}, nil, errors.New("a creator's claim names its creator: the creator is empty")
	}
	c, err := LoadClaims(tool, accountRoot)
	if err != nil {
		return Claims{}, nil, err
	}
	if typesByName(tool)[resourceType] == nil {
		return Claims{}, nil, fmt.Errorf("%s is not a resource type %s supplies (%s)", resourceType, tool.Name, typeNames(tool))
	}
	src, err := os.ReadFile(ClaimFile(tool, accountRoot))
	if err != nil {
		return Claims{}, nil, err
	}
	return c, src, nil
}

// checkedClaims checks the edited bytes as LoadClaims checks the file.
func checkedClaims(tool Tool, accountRoot, file string, out []byte) ([]byte, bool, error) {
	if _, err := parseClaims(tool, accountRoot, file, out); err != nil {
		return nil, false, fmt.Errorf("the edit of %s would leave it invalid, a bug: %w", file, err)
	}
	return out, true, nil
}

// claimOwner names whom a claim assigns its resource to.
func claimOwner(cl Claim) string {
	if cl.Creator != "" {
		return "its creator " + cl.Creator
	}
	return "the project " + cl.Project
}
