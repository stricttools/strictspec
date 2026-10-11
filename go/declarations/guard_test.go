package declarations_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The engine performs no write and starts no process: file writes and git
// go through the tool (Init and RenderImport return bytes; Git runs git).
func TestThePackageWritesNoFileAndStartsNoProcess(t *testing.T) {
	forbidden := map[string]bool{
		"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true,
		"Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "Remove": true,
		"RemoveAll": true, "Rename": true, "Chmod": true, "Chown": true, "Chtimes": true,
		"Symlink": true, "Link": true, "Truncate": true, "StartProcess": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" || p == "syscall" {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" && forbidden[sel.Sel.Name] {
				t.Errorf("%s calls os.%s", name, sel.Sel.Name)
			}
			return true
		})
	}
}
