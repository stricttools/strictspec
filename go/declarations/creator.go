package declarations

import (
	"errors"
	"fmt"
	"path/filepath"

	tomledit "github.com/stricttools/go-toml-edit"
)

// ClaimFile is the path of the account repository's claims.toml, which
// ClaimCreated, UnclaimCreated, and RenameClaim return the new bytes of.
func ClaimFile(tool Tool, accountRoot string) string {
	return filepath.Join(dirAbs(tool, accountRoot), claimsFile)
}

// ClaimCreated returns src, the bytes of the account repository's
// claims.toml as the tool holds them (read under its lock, or as a dry run
// left them), with a claim of the resource for creator, the command that
// created it and owns it (nothing declares it), and whether they differ: a
// resource already claimed for creator changes nothing, so a creator resuming
// its work may claim again. A resource src lists otherwise (claimed for
// a project or another creator, or listed unmanaged) is refused, naming the
// entry. The edit keeps the comments and layout of src. The
// tool writes the bytes, which the account repository's apply needs
// committed.
func ClaimCreated(tool Tool, accountRoot string, src []byte, resourceType, name, creator string) ([]byte, bool, error) {
	c, err := creatorClaims(tool, accountRoot, src, resourceType, creator)
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
	} else if err := doc.AppendToArray("claim", entry); err != nil {
		// Above, claim is written as an inline array (claim = [] when
		// empty): the new entry is appended to it as an inline table, in
		// place, so the comments and entries in the array stay as written.
		return nil, false, fmt.Errorf("%s: %w", file, err)
	}
	return checkedClaims(tool, accountRoot, file, doc.Bytes())
}

// UnclaimCreated returns src, the bytes of the account repository's
// claims.toml as the tool holds them, without the claim of the resource for
// creator, and whether they differ: a resource src does not claim changes
// nothing (the
// creator deleting what it created removes its claim, and may run again). A
// claim of the resource for a project or another creator is refused, naming
// it. The edit keeps the comments and layout of src.
func UnclaimCreated(tool Tool, accountRoot string, src []byte, resourceType, name, creator string) ([]byte, bool, error) {
	c, err := creatorClaims(tool, accountRoot, src, resourceType, creator)
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
		// Deleting the only [[claim]] table deletes the key, which the file
		// requires (claim = [] when empty); an inline claim = [...] keeps
		// it. Has answers false for an array of tables, so it is asked only
		// once no claim remains.
		if len(c.Claims) == 1 && !doc.Has("claim") {
			if err := doc.Set("claim", []any{}); err != nil {
				return nil, false, fmt.Errorf("%s: %w", file, err)
			}
		}
		return checkedClaims(tool, accountRoot, file, doc.Bytes())
	}
	return src, false, nil
}

// CreatorClaim reports whether src, the bytes of the account repository's
// claims.toml as the tool holds them, claims the resource for creator: true
// for a claim for creator, false when src lists the resource nowhere. A
// claim for a project or another creator, or an unmanaged entry, is
// refused, naming the entry: a creator checks this before deleting what it
// created, so it never deletes a resource the claims assign to another
// owner or leave alone.
func CreatorClaim(tool Tool, accountRoot string, src []byte, resourceType, name, creator string) (bool, error) {
	c, err := creatorClaims(tool, accountRoot, src, resourceType, creator)
	if err != nil {
		return false, err
	}
	file := ClaimFile(tool, accountRoot)
	for i, cl := range c.Claims {
		if cl.ResourceType != resourceType || cl.Name != name {
			continue
		}
		if cl.Creator == creator {
			return true, nil
		}
		return false, fmt.Errorf("%s: claim[%d] assigns %s/%s to %s, not to %s, so %s does not delete it", file, i, resourceType, name, claimOwner(cl), creator, creator)
	}
	for i, u := range c.Unmanaged {
		if u.ResourceType == resourceType && u.Name == name {
			return false, fmt.Errorf("%s: unmanaged[%d] lists %s/%s as nobody's (%s), so %s does not delete it", file, i, resourceType, name, u.Reason, creator)
		}
	}
	return false, nil
}

// creatorClaims checks src, claims.toml's bytes, for a creator's edit.
func creatorClaims(tool Tool, accountRoot string, src []byte, resourceType, creator string) (Claims, error) {
	if creator == "" {
		return Claims{}, errors.New("a creator's claim names its creator: the creator is empty")
	}
	c, err := givenClaims(tool, accountRoot, src)
	if err != nil {
		return Claims{}, err
	}
	if typesByName(tool)[resourceType] == nil {
		return Claims{}, fmt.Errorf("%s is not a resource type %s supplies (%s)", resourceType, tool.Name, typeNames(tool))
	}
	return c, nil
}

// givenClaims checks src as LoadClaims checks the file.
func givenClaims(tool Tool, accountRoot string, src []byte) (Claims, error) {
	if err := checkTool(tool); err != nil {
		return Claims{}, err
	}
	if !filepath.IsAbs(accountRoot) {
		return Claims{}, fmt.Errorf("the account repository's path %q is not absolute", accountRoot)
	}
	return parseClaims(tool, accountRoot, ClaimFile(tool, accountRoot), src)
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

// RenameClaim returns src, the bytes of the account repository's
// claims.toml as the tool holds them, with the claim or unmanaged entry of a
// resource renamed from oldName to newName (a command renaming the live
// resource keeps its name, the resource's key, the same everywhere), and
// whether they differ: a resource src does not list changes nothing. A
// newName src lists already is refused. The edit keeps the comments and
// layout of src.
func RenameClaim(tool Tool, accountRoot string, src []byte, resourceType, oldName, newName string) ([]byte, bool, error) {
	c, err := givenClaims(tool, accountRoot, src)
	if err != nil {
		return nil, false, err
	}
	file := ClaimFile(tool, accountRoot)
	path := ""
	for i, cl := range c.Claims {
		switch {
		case cl.ResourceType == resourceType && cl.Name == newName:
			return nil, false, fmt.Errorf("%s: claim[%d] already lists %s/%s: a resource has one name", file, i, resourceType, newName)
		case cl.ResourceType == resourceType && cl.Name == oldName:
			path = fmt.Sprintf("claim[%d].name", i)
		}
	}
	for i, u := range c.Unmanaged {
		switch {
		case u.ResourceType == resourceType && u.Name == newName:
			return nil, false, fmt.Errorf("%s: unmanaged[%d] already lists %s/%s: a resource has one name", file, i, resourceType, newName)
		case u.ResourceType == resourceType && u.Name == oldName:
			path = fmt.Sprintf("unmanaged[%d].name", i)
		}
	}
	if path == "" {
		return src, false, nil
	}
	doc, err := tomledit.Parse(src)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", file, err)
	}
	if err := doc.Set(path, newName); err != nil {
		return nil, false, fmt.Errorf("%s: %w", file, err)
	}
	return checkedClaims(tool, accountRoot, file, doc.Bytes())
}
