package strictspec

// The options built-ins (stricttools/docs/appendix-options.md): three
// toolchain-shipped built-in schemas — options-entries (a subject document under
// .strictmetadata/options/), options-registry (a tool's registry of the options
// it offers), and upstream (.strictmetadata/upstream/upstream.toml) — plus the
// readers that validate a document's SHAPE against them and bind it to typed
// values. Shape diagnostics are ordinary catalogued STRICTSPEC_* diagnostics from
// the shared executor, so they are identical across the Go, Python, and
// TypeScript runtimes.

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/stricttools/strictspec/go/internal/doc"
	"github.com/stricttools/strictspec/go/internal/tomldoc"
)

// OptionsEntriesSchema is the source of the built-in options-entries schema.
//
//go:embed builtin/options-entries.schema.toml
var OptionsEntriesSchema string

// OptionsRegistrySchema is the source of the built-in options-registry schema.
//
//go:embed builtin/options-registry.schema.toml
var OptionsRegistrySchema string

// UpstreamSchema is the source of the built-in upstream schema.
//
//go:embed builtin/upstream.schema.toml
var UpstreamSchema string

const (
	// OptionsDir is where a repository's option subject documents live,
	// relative to the repository root.
	OptionsDir = ".strictmetadata/options"
	// UpstreamFile is where a fork declares its upstream, relative to the
	// repository root.
	UpstreamFile = ".strictmetadata/upstream/upstream.toml"
	// optionsManifestFile is the directory's ownership manifest; it is not a
	// subject document.
	optionsManifestFile = "manifest.toml"
)

var (
	builtinOnce     sync.Once
	entriesProgram  *Program
	registryProgram *Program
	upstreamProgram *Program
)

func compileBuiltin(fileName, src string) *Program {
	p, err := CompileEmbedded(map[string]string{fileName: src}, fileName)
	if err != nil {
		// The built-ins are part of this runtime; one failing the meta-schema is
		// a strictspec bug, never a consumer condition.
		panic("strictspec: built-in schema " + fileName + " fails the meta-schema: " + err.Error())
	}
	return p
}

func builtins() {
	builtinOnce.Do(func() {
		entriesProgram = compileBuiltin("options-entries.schema.toml", OptionsEntriesSchema)
		registryProgram = compileBuiltin("options-registry.schema.toml", OptionsRegistrySchema)
		upstreamProgram = compileBuiltin("upstream.schema.toml", UpstreamSchema)
	})
}

// OptionsEntriesProgram is the compiled built-in options-entries schema.
func OptionsEntriesProgram() *Program { builtins(); return entriesProgram }

// OptionsRegistryProgram is the compiled built-in options-registry schema.
func OptionsRegistryProgram() *Program { builtins(); return registryProgram }

// UpstreamProgram is the compiled built-in upstream schema.
func UpstreamProgram() *Program { builtins(); return upstreamProgram }

// OptionDeclaration is one [[option]] of a tool's options registry. Requires
// names the other options of the same registry the option depends on (empty,
// never nil, when read from a registry document that requires none).
type OptionDeclaration struct {
	Name        string
	Subject     string
	Values      string
	Default     string
	Scope       string
	Requires    []string
	Description string
}

// OptionsRegistry is a tool's shape-valid options registry.
type OptionsRegistry struct {
	Options []OptionDeclaration
}

// OptionsEntry is one [[entry]] of a subject document. File is the subject
// document's file name (for example "changelog.toml") and Index the entry's
// position in it; HasScope reports whether the entry carries a scope.
type OptionsEntry struct {
	File     string
	Index    int
	ID       string
	Scope    string
	HasScope bool
	Current  string
	Ideal    string
	Reason   string
}

// Upstream is a fork's shape-valid upstream declaration.
type Upstream struct {
	Host   string
	Owner  string
	Repo   string
	Branch string
}

// FileDiagnostics are the shape diagnostics of one document, attributed to the
// file they were read from.
type FileDiagnostics struct {
	File        string
	Diagnostics []Diagnostic
}

// OptionsEntriesLoad is every subject document of a repository's options
// directory: the entries of the shape-valid documents, and the diagnostics of
// every document that is not. A document with diagnostics contributes no
// entries.
type OptionsEntriesLoad struct {
	Entries []OptionsEntry
	Invalid []FileDiagnostics
}

// validateTOML validates input against p and returns the parsed root when valid.
func validateTOML(p *Program, input []byte) (doc.Node, []Diagnostic) {
	res := p.Validate(input, "toml")
	if !res.Valid {
		return nil, res.Diagnostics
	}
	d, perr := tomldoc.Parse(input)
	if perr != nil { // unreachable: Validate parsed the same bytes
		panic("strictspec: reparse of a validated document failed: " + perr.Error())
	}
	return d.Root, nil
}

func tomlStr(rec Value, key string) (string, bool) {
	f, ok := rec.Field(key)
	if !ok {
		return "", false
	}
	return f.AsString()
}

// ReadOptionsRegistry validates a registry document's shape against the
// built-in options-registry schema and binds it. Non-empty diagnostics mean
// the registry is nil.
func ReadOptionsRegistry(input []byte) (*OptionsRegistry, []Diagnostic) {
	root, diags := validateTOML(OptionsRegistryProgram(), input)
	if diags != nil {
		return nil, diags
	}
	opts, _ := Value{node: root, format: doc.FormatTOML}.Field("option")
	reg := &OptionsRegistry{Options: []OptionDeclaration{}}
	for _, o := range opts.Items() {
		d := OptionDeclaration{}
		d.Name, _ = tomlStr(o, "name")
		d.Subject, _ = tomlStr(o, "subject")
		d.Values, _ = tomlStr(o, "values")
		d.Default, _ = tomlStr(o, "default")
		d.Scope, _ = tomlStr(o, "scope")
		reqs, _ := o.Field("requires")
		d.Requires = []string{}
		for _, r := range reqs.Items() {
			n, _ := r.AsString()
			d.Requires = append(d.Requires, n)
		}
		d.Description, _ = tomlStr(o, "description")
		reg.Options = append(reg.Options, d)
	}
	return reg, nil
}

// LoadOptionsRegistry reads and shape-validates the registry file at path. An
// unreadable file is an error.
func LoadOptionsRegistry(path string) (*OptionsRegistry, []Diagnostic, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	reg, diags := ReadOptionsRegistry(src)
	return reg, diags, nil
}

// ReadOptionsEntries validates one subject document's shape against the
// built-in options-entries schema and binds its entries, attributing each to
// file (the document's file name, for example "changelog.toml"). Non-empty
// diagnostics mean no entries.
func ReadOptionsEntries(file string, input []byte) ([]OptionsEntry, []Diagnostic) {
	root, diags := validateTOML(OptionsEntriesProgram(), input)
	if diags != nil {
		return nil, diags
	}
	items, _ := Value{node: root, format: doc.FormatTOML}.Field("entry")
	out := []OptionsEntry{}
	for i, e := range items.Items() {
		en := OptionsEntry{File: file, Index: i}
		en.ID, _ = tomlStr(e, "id")
		en.Scope, en.HasScope = tomlStr(e, "scope")
		en.Current, _ = tomlStr(e, "current")
		en.Ideal, _ = tomlStr(e, "ideal")
		en.Reason, _ = tomlStr(e, "reason")
		out = append(out, en)
	}
	return out, nil
}

// LoadOptionsEntries reads every subject document of repoRoot's
// .strictmetadata/options/ directory, in file-name order: each *.toml file
// other than the directory's manifest.toml. A missing directory means no
// entries. An unreadable directory or file is an error.
func LoadOptionsEntries(repoRoot string) (OptionsEntriesLoad, error) {
	out := OptionsEntriesLoad{Entries: []OptionsEntry{}, Invalid: []FileDiagnostics{}}
	dir := filepath.Join(repoRoot, filepath.FromSlash(OptionsDir))
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".toml") || n == optionsManifestFile {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return out, err
		}
		entries, diags := ReadOptionsEntries(n, src)
		if diags != nil {
			out.Invalid = append(out.Invalid, FileDiagnostics{File: n, Diagnostics: diags})
			continue
		}
		out.Entries = append(out.Entries, entries...)
	}
	return out, nil
}

// ReadUpstream validates an upstream document's shape against the built-in
// upstream schema and binds it. Non-empty diagnostics mean a nil Upstream.
func ReadUpstream(input []byte) (*Upstream, []Diagnostic) {
	root, diags := validateTOML(UpstreamProgram(), input)
	if diags != nil {
		return nil, diags
	}
	v := Value{node: root, format: doc.FormatTOML}
	u := &Upstream{}
	u.Host, _ = tomlStr(v, "host")
	u.Owner, _ = tomlStr(v, "owner")
	u.Repo, _ = tomlStr(v, "repo")
	u.Branch, _ = tomlStr(v, "branch")
	return u, nil
}

// LoadUpstream reads repoRoot's .strictmetadata/upstream/upstream.toml. found
// is false when the repository declares no upstream (the file is absent). An
// unreadable file is an error.
func LoadUpstream(repoRoot string) (u *Upstream, found bool, diags []Diagnostic, err error) {
	src, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(UpstreamFile)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil, nil
		}
		return nil, false, nil, err
	}
	u, diags = ReadUpstream(src)
	return u, true, diags, nil
}
