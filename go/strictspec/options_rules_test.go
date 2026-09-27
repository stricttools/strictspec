package strictspec

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The rules cases are shared with the Python and TypeScript runtimes' tests,
// so every runtime asserts the identical diagnostics (code, path, and rendered
// message) and classifications.
const rulesCasesFile = "testdata/options/rules-cases.json"

type jsonDiagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type jsonClassified struct {
	File  string `json:"file"`
	Index int    `json:"index"`
	Class string `json:"class"`
}

// jsonNamespaceInput is a namespace case's input: subject documents read
// through the shape reader (Files), or entries handed to the validator
// directly, bypassing the shape reader (Entries).
type jsonNamespaceInput struct {
	Files   map[string]string `json:"files"`
	Entries []jsonEntry       `json:"entries"`
}

// jsonRegistryInput is a registry case's input: a registry document read
// through the shape reader (Registry), or options handed over directly
// (Options).
type jsonRegistryInput struct {
	Registry string              `json:"registry"`
	Options  []OptionDeclaration `json:"options"`
}

type jsonFixInput struct {
	Input *string `json:"input"`
	jsonRegistryInput
	jsonNamespaceInput
}

type rulesCases struct {
	Ranking []struct {
		Name        string           `json:"name"`
		Input       string           `json:"input"`
		Levels      [][]string       `json:"levels"`
		Diagnostics []jsonDiagnostic `json:"diagnostics"`
	} `json:"ranking"`
	Registry []struct {
		Name string `json:"name"`
		jsonRegistryInput
		Diagnostics []jsonDiagnostic `json:"diagnostics"`
	} `json:"registry"`
	Namespace struct {
		Tool     string `json:"tool"`
		Registry string `json:"registry"`
		Cases    []struct {
			Name string `json:"name"`
			jsonNamespaceInput
			Classified  []jsonClassified `json:"classified"`
			Diagnostics []jsonDiagnostic `json:"diagnostics"`
		} `json:"cases"`
	} `json:"namespace"`
	Fixes []struct {
		Name   string       `json:"name"`
		Kind   string       `json:"kind"`
		Code   string       `json:"code"`
		Fix    string       `json:"fix"`
		Before jsonFixInput `json:"before"`
		After  jsonFixInput `json:"after"`
	} `json:"fixes"`
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

func toJSONDiagnostics(ds []Diagnostic) []jsonDiagnostic {
	out := []jsonDiagnostic{}
	for _, d := range ds {
		out = append(out, jsonDiagnostic{Code: d.Code, Path: d.Path, Message: d.Message})
	}
	return out
}

func orEmpty(ds []jsonDiagnostic) []jsonDiagnostic {
	if ds == nil {
		return []jsonDiagnostic{}
	}
	return ds
}

func rankingLevels(r *OptionsRanking) [][]string {
	if r == nil {
		return nil
	}
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
			r, diags := ParseOptionsRanking(c.Input)
			sameJSON(t, toJSONDiagnostics(diags), orEmpty(c.Diagnostics))
			sameJSON(t, rankingLevels(r), c.Levels)
			if (r == nil) != (len(diags) > 0) {
				t.Fatalf("a ranking is returned iff it has no diagnostics; ranking=%v", r)
			}
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

func registryInput(t *testing.T, in jsonRegistryInput) *OptionsRegistry {
	t.Helper()
	if in.Options != nil {
		return &OptionsRegistry{Options: in.Options}
	}
	return mustRegistry(t, in.Registry)
}

func TestOptionsRegistryRules(t *testing.T) {
	for _, c := range loadRulesCases(t).Registry {
		t.Run(c.Name, func(t *testing.T) {
			checked, diags := ValidateOptionsRegistry(registryInput(t, c.jsonRegistryInput))
			sameJSON(t, toJSONDiagnostics(diags), orEmpty(c.Diagnostics))
			if (checked == nil) != (len(c.Diagnostics) > 0) {
				t.Fatalf("a registry is usable iff it has no diagnostics; checked=%v", checked)
			}
		})
	}
}

func namespaceEntries(t *testing.T, in jsonNamespaceInput) []OptionsEntry {
	t.Helper()
	var entries []OptionsEntry
	if in.Entries != nil {
		for _, e := range in.Entries {
			en := OptionsEntry{File: e.File, Index: e.Index, ID: e.ID, Current: e.Current, Ideal: e.Ideal, Reason: e.Reason}
			if e.Scope != nil {
				en.Scope, en.HasScope = *e.Scope, true
			}
			entries = append(entries, en)
		}
		return entries
	}
	var names []string
	for n := range in.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		es, diags := ReadOptionsEntries(n, []byte(in.Files[n]))
		if diags != nil {
			t.Fatalf("%s is not shape-valid: %+v", n, diags)
		}
		entries = append(entries, es...)
	}
	return entries
}

func namespaceRegistry(t *testing.T, ns string) *CheckedOptionsRegistry {
	t.Helper()
	reg, diags := ValidateOptionsRegistry(mustRegistry(t, ns))
	if diags != nil {
		t.Fatalf("namespace registry refused: %+v", diags)
	}
	return reg
}

func TestOptionsNamespaceRules(t *testing.T) {
	ns := loadRulesCases(t).Namespace
	reg := namespaceRegistry(t, ns.Registry)
	for _, c := range ns.Cases {
		t.Run(c.Name, func(t *testing.T) {
			accepted, diags := ValidateOptionsNamespace(ns.Tool, reg, namespaceEntries(t, c.jsonNamespaceInput))
			classified := []jsonClassified{}
			for _, a := range accepted {
				classified = append(classified, jsonClassified{File: a.Entry.File, Index: a.Entry.Index, Class: string(a.Class)})
			}
			sameJSON(t, toJSONDiagnostics(diags), orEmpty(c.Diagnostics))
			sameJSON(t, classified, c.Classified)
		})
	}
}

// TestOptionsFixInstructions proves every fix a message names: the "before"
// input draws a diagnostic with the case's code whose message names the fix,
// and the "after" input, which applies that fix and nothing else, draws no
// diagnostic at all.
func TestOptionsFixInstructions(t *testing.T) {
	cases := loadRulesCases(t)
	reg := namespaceRegistry(t, cases.Namespace.Registry)
	run := func(t *testing.T, kind string, in jsonFixInput) []Diagnostic {
		switch kind {
		case "ranking":
			_, diags := ParseOptionsRanking(*in.Input)
			return diags
		case "registry":
			_, diags := ValidateOptionsRegistry(registryInput(t, in.jsonRegistryInput))
			return diags
		case "namespace":
			_, diags := ValidateOptionsNamespace(cases.Namespace.Tool, reg, namespaceEntries(t, in.jsonNamespaceInput))
			return diags
		}
		t.Fatalf("unknown fix kind %q", kind)
		return nil
	}
	for _, c := range cases.Fixes {
		t.Run(c.Name, func(t *testing.T) {
			named := false
			for _, d := range run(t, c.Kind, c.Before) {
				if d.Code == c.Code && strings.Contains(d.Message, c.Fix) {
					named = true
				}
			}
			if !named {
				t.Fatalf("before: no %s diagnostic names the fix %q", c.Code, c.Fix)
			}
			if after := run(t, c.Kind, c.After); len(after) != 0 {
				t.Fatalf("after applying the fix: %+v", after)
			}
		})
	}
}
