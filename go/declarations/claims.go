package declarations

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Claims is the account repository's claims.toml: Root is the account
// repository's root, which Compare recognizes the account repository by.
type Claims struct {
	Root      string
	Claims    []Claim
	Unmanaged []UnmanagedResource
}

// Claim assigns an account-level resource a project declares to that
// project, by its project.toml name.
type Claim struct {
	ResourceType string
	Name         string
	Project      string
}

// UnmanagedResource is a live account-level resource nobody declares, and
// why.
type UnmanagedResource struct {
	ResourceType string
	Name         string
	Reason       string
}

// LoadClaims reads and checks the account repository's claims.toml, at the top
// of its declaration directory. accountRoot is an absolute path.
func LoadClaims(tool Tool, accountRoot string) (Claims, error) {
	if err := checkTool(tool); err != nil {
		return Claims{}, err
	}
	if !filepath.IsAbs(accountRoot) {
		return Claims{}, fmt.Errorf("the account repository's path %q is not absolute", accountRoot)
	}
	file := filepath.Join(dirAbs(tool, accountRoot), claimsFile)
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Claims{}, fmt.Errorf("%s is missing: the account repository lists every claim there; write it holding format_version = 1, claim = [], and unmanaged = []", file)
	}
	if err != nil {
		return Claims{}, err
	}
	builtins()
	v, err := validated(claimsProgram, file, src, "toml")
	if err != nil {
		return Claims{}, err
	}
	c := Claims{Root: accountRoot, Claims: []Claim{}, Unmanaged: []UnmanagedResource{}}
	cl, _ := v.Field("claim")
	for _, e := range cl.Items() {
		c.Claims = append(c.Claims, Claim{ResourceType: fieldString(e, "resource_type"), Name: fieldString(e, "name"), Project: fieldString(e, "project")})
	}
	un, _ := v.Field("unmanaged")
	for _, e := range un.Items() {
		c.Unmanaged = append(c.Unmanaged, UnmanagedResource{ResourceType: fieldString(e, "resource_type"), Name: fieldString(e, "name"), Reason: fieldString(e, "reason")})
	}
	types := typesByName(tool)
	var problems []string
	places := map[string][]string{}
	var order []string
	note := func(resourceType, name, place string) {
		if types[resourceType] == nil {
			problems = append(problems, fmt.Sprintf("%s names the resource type %s, which %s does not supply (%s)", place, resourceType, tool.Name, typeNames(tool)))
		}
		k := resourceType + "/" + name
		if places[k] == nil {
			order = append(order, k)
		}
		places[k] = append(places[k], place)
	}
	for i, e := range c.Claims {
		note(e.ResourceType, e.Name, fmt.Sprintf("claim[%d]", i))
	}
	for i, e := range c.Unmanaged {
		note(e.ResourceType, e.Name, fmt.Sprintf("unmanaged[%d]", i))
	}
	for _, k := range order {
		if len(places[k]) > 1 {
			problems = append(problems, fmt.Sprintf("%s appears more than once (%s): list each resource once", k, strings.Join(places[k], ", ")))
		}
	}
	if len(problems) > 0 {
		return Claims{}, refusal(file, problems)
	}
	return c, nil
}
