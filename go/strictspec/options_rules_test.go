package strictspec

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// The rules cases are shared with the Python and TypeScript runtimes' tests,
// so every runtime asserts the identical refusals and classifications.
const rulesCasesFile = "testdata/options/rules-cases.json"

type jsonRefusal struct {
	Rule   string `json:"rule"`
	File   string `json:"file"`
	Index  int    `json:"index"`
	Value  string `json:"value"`
	Detail string `json:"detail"`
}

type jsonClassified struct {
	File  string `json:"file"`
	Index int    `json:"index"`
	Class string `json:"class"`
}

type rulesCases struct {
	Ranking []struct {
		Name     string        `json:"name"`
		Input    string        `json:"input"`
		Levels   [][]string    `json:"levels"`
		Refusals []jsonRefusal `json:"refusals"`
	} `json:"ranking"`
	Registry []struct {
		Name     string        `json:"name"`
		Registry string        `json:"registry"`
		Refusals []jsonRefusal `json:"refusals"`
	} `json:"registry"`
	Namespace struct {
		Tool     string `json:"tool"`
		Registry string `json:"registry"`
		Cases    []struct {
			Name       string            `json:"name"`
			Files      map[string]string `json:"files"`
			Classified []jsonClassified  `json:"classified"`
			Refusals   []jsonRefusal     `json:"refusals"`
		} `json:"cases"`
	} `json:"namespace"`
}

func loadRulesCases(t *testing.T) rulesCases {
	t.Helper()
	raw, err := os.ReadFile(rulesCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	var c rulesCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func toJSONRefusals(rs []optionsRefusal) []jsonRefusal {
	out := []jsonRefusal{}
	for _, r := range rs {
		out = append(out, jsonRefusal{Rule: string(r.Rule), File: r.File, Index: r.Index, Value: r.Value, Detail: r.Detail})
	}
	return out
}

func rankingLevels(r optionsRanking) [][]string {
	var out [][]string
	for _, v := range r.Values {
		l := r.Level[v]
		for len(out) <= l {
			out = append(out, nil)
		}
		out[l] = append(out[l], v)
	}
	return out
}

func sameJSON(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", " ")
		wj, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("got\n%s\nwant\n%s", gj, wj)
	}
}

func TestOptionsRanking(t *testing.T) {
	for _, c := range loadRulesCases(t).Ranking {
		t.Run(c.Name, func(t *testing.T) {
			r, refusals := parseOptionsRanking(c.Input)
			if c.Refusals == nil {
				c.Refusals = []jsonRefusal{}
			}
			sameJSON(t, toJSONRefusals(refusals), c.Refusals)
			sameJSON(t, rankingLevels(r), c.Levels)
		})
	}
}

func mustRegistry(t *testing.T, src string) *OptionsRegistry {
	t.Helper()
	reg, diags := ReadOptionsRegistry([]byte(src))
	if diags != nil {
		t.Fatalf("registry is not shape-valid: %+v", diags)
	}
	return reg
}

func TestOptionsRegistryRules(t *testing.T) {
	for _, c := range loadRulesCases(t).Registry {
		t.Run(c.Name, func(t *testing.T) {
			checked, refusals := checkOptionsRegistry(mustRegistry(t, c.Registry))
			sameJSON(t, toJSONRefusals(refusals), c.Refusals)
			if (checked == nil) != (len(c.Refusals) > 0) {
				t.Fatalf("a registry is usable iff it has no refusals; checked=%v", checked)
			}
		})
	}
}

func TestOptionsNamespaceRules(t *testing.T) {
	ns := loadRulesCases(t).Namespace
	reg, refusals := checkOptionsRegistry(mustRegistry(t, ns.Registry))
	if refusals != nil {
		t.Fatalf("namespace registry refused: %+v", refusals)
	}
	for _, c := range ns.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var names []string
			for n := range c.Files {
				names = append(names, n)
			}
			sort.Strings(names)
			var entries []OptionsEntry
			for _, n := range names {
				es, diags := ReadOptionsEntries(n, []byte(c.Files[n]))
				if diags != nil {
					t.Fatalf("%s is not shape-valid: %+v", n, diags)
				}
				entries = append(entries, es...)
			}
			accepted, refusals := validateOptionsNamespace(ns.Tool, reg, entries)
			classified := []jsonClassified{}
			for _, a := range accepted {
				classified = append(classified, jsonClassified{File: a.Entry.File, Index: a.Entry.Index, Class: string(a.Class)})
			}
			sameJSON(t, toJSONRefusals(refusals), c.Refusals)
			sameJSON(t, classified, c.Classified)
		})
	}
}
