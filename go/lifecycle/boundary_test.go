package lifecycle_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sourceFiles parses every non-test Go file of this package.
func sourceFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	out := map[string]*ast.File{}
	fset := token.NewFileSet()
	for _, dir := range []string{"."} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			out[path] = f
		}
	}
	if len(out) == 0 {
		t.Fatal("no source files found")
	}
	return out
}

// The library is linked by rlsbl, selfdoc, and safegit, so it imports none of
// them: only the standard library, this module, and go-toml-edit.
func TestTheLibraryImportsNoToolThatLinksIt(t *testing.T) {
	for path, f := range sourceFiles(t) {
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			switch {
			case !strings.Contains(strings.SplitN(p, "/", 2)[0], "."):
				// standard library
			case strings.HasPrefix(p, "github.com/stricttools/strictspec/go/"):
			case p == "github.com/stricttools/go-toml-edit":
			default:
				t.Errorf("%s imports %s", path, p)
			}
		}
	}
}

// Every write goes through the injected FileWriter: no source file calls a
// function of package os that writes, creates, removes, or renames.
func TestTheLibraryWritesNoFileItself(t *testing.T) {
	forbidden := map[string]bool{
		"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true,
		"Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "Remove": true,
		"RemoveAll": true, "Rename": true, "Chmod": true, "Symlink": true,
		"Link": true, "Truncate": true,
	}
	for path, f := range sourceFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && forbidden[sel.Sel.Name] {
				t.Errorf("%s calls os.%s; write through the injected FileWriter", path, sel.Sel.Name)
			}
			return true
		})
	}
}
