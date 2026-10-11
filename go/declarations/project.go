package declarations

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stricttools/strictspec/go/strictspec"
)

const (
	metadataDir  = ".strictmetadata"
	manifestFile = "manifest.toml"
	projectFile  = "project.toml"
	claimsFile   = "claims.toml"
)

var (
	projectNamePattern = `^[a-z0-9][a-z0-9-]*$`
	projectNameRE      = regexp.MustCompile(projectNamePattern)
	kebabRE            = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Project is a repository holding a declaration directory: its root (an
// absolute path) and the name its project.toml gives.
type Project struct {
	Root string
	Name string
}

// stat is os.Stat; the tests that need a directory with no repository above
// it replace it, since their temporary directories may sit inside one.
var stat = os.Stat

// dirRel is the declaration directory relative to a root, slash-separated.
func dirRel(tool Tool) string { return path.Join(metadataDir, tool.Directory) }

// dirAbs is the declaration directory under root.
func dirAbs(tool Tool, root string) string {
	return filepath.Join(root, metadataDir, tool.Directory)
}

// checkTool refuses a Tool the engine cannot work with: a bug in the tool.
func checkTool(tool Tool) error {
	if tool.Name == "" {
		return errors.New("declarations: Tool.Name is empty")
	}
	if !kebabRE.MatchString(tool.Directory) {
		return fmt.Errorf("declarations: Tool.Directory %q is not one kebab-case directory name", tool.Directory)
	}
	if strings.TrimSpace(tool.InitCommand) == "" {
		return errors.New("declarations: Tool.InitCommand is empty")
	}
	seen := map[string]bool{}
	for _, rt := range tool.ResourceTypes {
		n := rt.Name()
		if !kebabRE.MatchString(n) {
			return fmt.Errorf("declarations: resource type name %q is not kebab-case", n)
		}
		if seen[n] {
			return fmt.Errorf("declarations: the tool supplies the resource type %q twice", n)
		}
		seen[n] = true
		if err := checkDescriptor(n, rt.Descriptor()); err != nil {
			return err
		}
	}
	return nil
}

func checkDescriptor(name string, d Descriptor) error {
	bug := func(format string, a ...any) error {
		return fmt.Errorf("declarations: the descriptor of the resource type %s: "+format, append([]any{name}, a...)...)
	}
	for _, f := range d.DerivableFields {
		if !contains(d.Fields, f) {
			return bug("DerivableFields names %s, which is not one of its Fields", f)
		}
	}
	for _, f := range d.CreateOnly {
		if contains(d.Fields, f) {
			return bug("CreateOnly names %s, which is one of its Fields", f)
		}
	}
	seen := map[string]bool{}
	for _, c := range d.Collections {
		if len(c.Key) == 0 {
			return bug("the collection %s has no key", c.Name)
		}
		if seen[c.Name] {
			return bug("names the collection %s twice", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// FindRoot walks up from cwd to the nearest directory holding .strictmetadata/,
// the project's repository root, and returns its absolute path.
func FindRoot(tool Tool, cwd string) (string, error) {
	if err := checkTool(tool); err != nil {
		return "", err
	}
	start, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		info, err := stat(filepath.Join(dir, metadataDir))
		if err == nil && info.IsDir() {
			return dir, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("no %s/ directory in %s or any directory above it: run %s from inside a project repository", metadataDir, start, tool.Name)
		}
	}
}

// FindProject finds the repository holding cwd (FindRoot) and reads its
// declaration directory's manifest.toml, which must name the tool as owner,
// and project.toml.
func FindProject(tool Tool, cwd string) (Project, error) {
	root, err := FindRoot(tool, cwd)
	if err != nil {
		return Project{}, err
	}
	manifest := filepath.Join(dirAbs(tool, root), manifestFile)
	src, err := os.ReadFile(manifest)
	if errors.Is(err, fs.ErrNotExist) {
		return Project{}, fmt.Errorf("%s is missing: %s has no %s/ declaration directory; run %s --project <name> to create it", manifest, root, dirRel(tool), tool.InitCommand)
	}
	if err != nil {
		return Project{}, err
	}
	owner, err := readManifest(manifest, src)
	if err != nil {
		return Project{}, err
	}
	if owner != tool.Name {
		return Project{}, fmt.Errorf("%s names the owner %q: the directory belongs to %s, not %s", manifest, owner, owner, tool.Name)
	}
	pf := filepath.Join(dirAbs(tool, root), projectFile)
	src, err = os.ReadFile(pf)
	if errors.Is(err, fs.ErrNotExist) {
		return Project{}, fmt.Errorf("%s is missing: write it holding format_version = 1 and name = \"<project>\" (the project's name, matching %s)", pf, projectNamePattern)
	}
	if err != nil {
		return Project{}, err
	}
	name, err := readProject(pf, src)
	if err != nil {
		return Project{}, err
	}
	return Project{Root: root, Name: name}, nil
}

// readManifest validates a manifest.toml. The file holds the one key owner;
// format_version is supplied here, so a file writing it is refused.
func readManifest(file string, src []byte) (string, error) {
	v, err := strictspec.LoadValue(src, "toml")
	if err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	if _, ok := v.Field("format_version"); ok {
		return "", fmt.Errorf("%s writes format_version: a manifest holds only owner (the engine supplies the format version); remove it", file)
	}
	builtins()
	withVersion := append([]byte("format_version = 1\n"), src...)
	root, err := validated(manifestProgram, file, withVersion, "toml")
	if err != nil {
		return "", err
	}
	owner, _ := root.Field("owner")
	s, _ := owner.AsString()
	return s, nil
}

func readProject(file string, src []byte) (string, error) {
	builtins()
	root, err := validated(projectProgram, file, src, "toml")
	if err != nil {
		return "", err
	}
	n, _ := root.Field("name")
	s, _ := n.AsString()
	return s, nil
}

// validated validates src against p and returns its root value, or an error
// naming the file and every diagnostic.
func validated(p *strictspec.Program, file string, src []byte, syntax string) (strictspec.Value, error) {
	res := p.Validate(src, syntax)
	if !res.Valid {
		return strictspec.Value{}, errors.New(renderDiagnostics(file, res.Diagnostics))
	}
	v, err := strictspec.LoadValue(src, syntax)
	if err != nil { // unreachable: Validate parsed the same bytes
		return strictspec.Value{}, fmt.Errorf("%s: %w", file, err)
	}
	return v, nil
}

// fieldString is a schema-valid record's string field.
func fieldString(rec strictspec.Value, key string) string {
	f, _ := rec.Field(key)
	s, _ := f.AsString()
	return s
}

func renderDiagnostics(file string, diags []strictspec.Diagnostic) string {
	var b strings.Builder
	b.WriteString(file + " is invalid:")
	for _, d := range diags {
		fmt.Fprintf(&b, "\n  %s at %s: %s", d.Code, d.Path, d.Message)
	}
	return b.String()
}

// Init returns the bytes of a new declaration directory's manifest.toml and
// project.toml, keyed by their absolute paths under root, for the tool to
// write. It refuses when the directory exists.
func Init(tool Tool, root, project string) (map[string][]byte, error) {
	if err := checkTool(tool); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("the root %q is not an absolute path", root)
	}
	if !projectNameRE.MatchString(project) {
		return nil, fmt.Errorf("the project name %q does not match %s", project, projectNamePattern)
	}
	dir := dirAbs(tool, root)
	if _, err := os.Lstat(dir); err == nil {
		return nil, fmt.Errorf("%s exists: a declaration directory is created once", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	manifest := []byte(fmt.Sprintf("owner = %s\n", tomlString(tool.Name)))
	proj := []byte(fmt.Sprintf("format_version = 1\nname = %s\n", tomlString(project)))
	if _, err := readManifest(manifestFile, manifest); err != nil {
		return nil, err
	}
	if _, err := readProject(projectFile, proj); err != nil {
		return nil, err
	}
	return map[string][]byte{
		filepath.Join(dir, manifestFile): manifest,
		filepath.Join(dir, projectFile):  proj,
	}, nil
}
