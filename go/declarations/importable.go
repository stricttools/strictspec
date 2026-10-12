package declarations

import (
	"fmt"
	"sort"
)

// Importable is a live resource no declaration covers, which the tool's
// import can declare.
type Importable struct {
	ResourceType string
	Name         string
}

// ListImportable lists the live resources of the resource types named that
// no declaration covers: declared neither in the project (set) nor in the
// account repository (account, set itself when the project is the account
// repository), neither claimed nor listed unmanaged in claims, and owned
// (Owning) by no live resource, which they go with. They are sorted by
// resource type and name. A resource type the tool does not supply is
// refused, and a failed list or read is an error.
func ListImportable(set, account Set, live LiveReader, claims Claims, resourceTypes []string) ([]Importable, error) {
	if err := checkTool(set.Tool); err != nil {
		return nil, err
	}
	if err := checkNotImporting(set); err != nil {
		return nil, err
	}
	if err := checkNotImporting(account); err != nil {
		return nil, err
	}
	same, err := samePath(account.Project.Root, claims.Root)
	if err != nil {
		return nil, err
	}
	if !same {
		return nil, fmt.Errorf("the account set's root %s is not the claims' account repository %s: load both from the account repository", account.Project.Root, claims.Root)
	}
	types := typesByName(set.Tool)
	for _, t := range resourceTypes {
		if types[t] == nil {
			return nil, fmt.Errorf("%s is not a resource type %s supplies (%s)", t, set.Tool.Name, typeNames(set.Tool))
		}
	}
	covered := map[string]bool{}
	for _, s := range []Set{set, account} {
		for _, r := range s.Resources {
			covered[r.Type+"/"+r.Name] = true
		}
	}
	for _, c := range claims.Claims {
		covered[c.ResourceType+"/"+c.Name] = true
	}
	for _, u := range claims.Unmanaged {
		covered[u.ResourceType+"/"+u.Name] = true
	}
	listed := map[string]map[string]bool{}
	list := func(t string) (map[string]bool, error) {
		if m, ok := listed[t]; ok {
			return m, nil
		}
		names, err := live.List(t)
		if err != nil {
			return nil, fmt.Errorf("listing the live %s resources: %w", t, err)
		}
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		listed[t] = m
		return m, nil
	}
	var out []Importable
	for _, t := range resourceTypes {
		names, err := list(t)
		if err != nil {
			return nil, err
		}
		for n := range names {
			if covered[t+"/"+n] {
				continue
			}
			if o, ok := types[t].(Owning); ok {
				r, ok, err := live.Read(t, n)
				if err != nil {
					return nil, fmt.Errorf("reading %s/%s: %w", t, n, err)
				}
				if !ok {
					continue
				}
				if ot, on, owned := o.Owner(r); owned {
					owners, err := list(ot)
					if err != nil {
						return nil, err
					}
					if owners[on] {
						continue
					}
				}
			}
			out = append(out, Importable{ResourceType: t, Name: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceType != out[j].ResourceType {
			return out[i].ResourceType < out[j].ResourceType
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
