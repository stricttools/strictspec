package strictspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The shape cases are shared with the Python and TypeScript runtimes' tests
// (they read this same file), so every runtime asserts the identical bound
// values and the identical ordered code + path + message diagnostics.
const shapeCasesFile = "testdata/options/shape-cases.json"

type shapeCase struct {
	Name   string          `json:"name"`
	Schema string          `json:"schema"`
	File   string          `json:"file,omitempty"`
	Input  string          `json:"input"`
	Expect json.RawMessage `json:"expect"`
}

type jsonDiag struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type jsonEntry struct {
	File    string  `json:"file"`
	Index   int     `json:"index"`
	ID      string  `json:"id"`
	Scope   *string `json:"scope"`
	Current string  `json:"current"`
	Ideal   string  `json:"ideal"`
	Reason  string  `json:"reason"`
}

type jsonOption struct {
	Name        string `json:"name"`
	Subject     string `json:"subject"`
	Values      string `json:"values"`
	Default     string `json:"default"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
}

type jsonUpstream struct {
	Host   string `json:"host"`
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

type shapeExpect struct {
	Entries     []jsonEntry   `json:"entries,omitempty"`
	Options     []jsonOption  `json:"options,omitempty"`
	Upstream    *jsonUpstream `json:"upstream,omitempty"`
	Diagnostics []jsonDiag    `json:"diagnostics,omitempty"`
}

func toJSONDiags(ds []Diagnostic) []jsonDiag {
	var out []jsonDiag
	for _, d := range ds {
		out = append(out, jsonDiag{Code: d.Code, Path: d.Path, Message: d.Message})
	}
	return out
}

func toJSONEntries(es []OptionsEntry) []jsonEntry {
	var out []jsonEntry
	for _, e := range es {
		je := jsonEntry{File: e.File, Index: e.Index, ID: e.ID, Current: e.Current, Ideal: e.Ideal, Reason: e.Reason}
		if e.HasScope {
			s := e.Scope
			je.Scope = &s
		}
		out = append(out, je)
	}
	return out
}

func runShapeCase(c shapeCase) shapeExpect {
	var got shapeExpect
	switch c.Schema {
	case "entries":
		es, ds := ReadOptionsEntries(c.File, []byte(c.Input))
		got.Entries, got.Diagnostics = toJSONEntries(es), toJSONDiags(ds)
	case "registry":
		reg, ds := ReadOptionsRegistry([]byte(c.Input))
		got.Diagnostics = toJSONDiags(ds)
		if reg != nil {
			for _, o := range reg.Options {
				got.Options = append(got.Options, jsonOption(o))
			}
		}
	case "upstream":
		u, ds := ReadUpstream([]byte(c.Input))
		got.Diagnostics = toJSONDiags(ds)
		if u != nil {
			ju := jsonUpstream(*u)
			got.Upstream = &ju
		}
	default:
		panic("unknown schema " + c.Schema)
	}
	return got
}

func TestOptionsShapeCases(t *testing.T) {
	raw, err := os.ReadFile(shapeCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	var cases []shapeCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no shape cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var want shapeExpect
			if err := json.Unmarshal(c.Expect, &want); err != nil {
				t.Fatalf("case expect: %v", err)
			}
			got := runShapeCase(c)
			if !reflect.DeepEqual(got, want) {
				gj, _ := json.MarshalIndent(got, "", " ")
				wj, _ := json.MarshalIndent(want, "", " ")
				t.Fatalf("got\n%s\nwant\n%s", gj, wj)
			}
		})
	}
}

// TestBuiltinSchemasCompile: each built-in compiles against the meta-schema and
// accepts only format_version 1, naming the schema by its appendix name.
func TestBuiltinSchemasCompile(t *testing.T) {
	for name, p := range map[string]*Program{
		"options-entries":  OptionsEntriesProgram(),
		"options-registry": OptionsRegistryProgram(),
		"upstream":         UpstreamProgram(),
	} {
		if p.prog.FormatVersion() != 1 {
			t.Errorf("%s: format_version %d, want 1", name, p.prog.FormatVersion())
		}
		res := p.Validate([]byte("format_version = 7\n"), "toml")
		if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "STRICTSPEC_GATE_UNSUPPORTED" ||
			!strings.Contains(res.Diagnostics[0].Message, "schema "+name+" ") {
			t.Errorf("%s: format_version diagnostics %+v", name, res.Diagnostics)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const oneEntry = "format_version = 1\n[[entry]]\nid = \"%s\"\ncurrent = \"off\"\nideal = \"on\"\nreason = \"r\"\n"

func TestLoadOptionsEntriesMissingDirectory(t *testing.T) {
	load, err := LoadOptionsEntries(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(load.Entries) != 0 || len(load.Invalid) != 0 {
		t.Fatalf("missing directory must mean no entries, got %+v", load)
	}
}

func TestLoadOptionsEntriesDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".strictmetadata", "options")
	writeFile(t, filepath.Join(dir, "manifest.toml"), "owner = \"strictspec\"\n")
	writeFile(t, filepath.Join(dir, "release.toml"), strings.Replace(oneEntry, "%s", "rlsbl:b", 1))
	writeFile(t, filepath.Join(dir, "changelog.toml"), strings.Replace(oneEntry, "%s", "rlsbl:a", 1))
	writeFile(t, filepath.Join(dir, "broken.toml"), "format_version = 1\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a subject document")
	load, err := LoadOptionsEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(load.Entries) != 2 || load.Entries[0].File != "changelog.toml" || load.Entries[0].ID != "rlsbl:a" ||
		load.Entries[1].File != "release.toml" || load.Entries[1].ID != "rlsbl:b" {
		t.Fatalf("entries %+v", load.Entries)
	}
	if len(load.Invalid) != 1 || load.Invalid[0].File != "broken.toml" ||
		load.Invalid[0].Diagnostics[0].Code != "STRICTSPEC_TYPE_MISSING_REQUIRED" {
		t.Fatalf("invalid %+v", load.Invalid)
	}
}

func TestLoadOptionsRegistryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.toml")
	writeFile(t, path, "format_version = 1\n[[option]]\nname = \"a\"\nsubject = \"s\"\nvalues = \"on > off\"\ndefault = \"on\"\nscope = \"none\"\ndescription = \"d\"\n")
	reg, diags, err := LoadOptionsRegistry(path)
	if err != nil || diags != nil || len(reg.Options) != 1 || reg.Options[0].Values != "on > off" {
		t.Fatalf("reg %+v diags %+v err %v", reg, diags, err)
	}
	if _, _, err := LoadOptionsRegistry(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
		t.Fatal("a missing registry file is an error")
	}
}

func TestLoadUpstream(t *testing.T) {
	root := t.TempDir()
	u, found, diags, err := LoadUpstream(root)
	if err != nil || found || u != nil || diags != nil {
		t.Fatalf("no upstream file: %+v %v %+v %v", u, found, diags, err)
	}
	writeFile(t, filepath.Join(root, ".strictmetadata", "upstream", "upstream.toml"),
		"format_version = 1\nhost = \"github.com\"\nowner = \"ncruces\"\nrepo = \"wasm2go\"\nbranch = \"main\"\n")
	u, found, diags, err = LoadUpstream(root)
	if err != nil || !found || diags != nil || *u != (Upstream{"github.com", "ncruces", "wasm2go", "main"}) {
		t.Fatalf("upstream: %+v %v %+v %v", u, found, diags, err)
	}
}
