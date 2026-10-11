package declarations

import (
	"embed"
	"sync"

	"github.com/stricttools/strictspec/go/strictspec"
)

//go:embed schemas/*.toml
var schemaFiles embed.FS

// typesFile is the type-definitions file every resource type's schema
// imports; the engine adds it to the resource type's schema files.
const typesFile = "declarations.types.toml"

func schemaSource(file string) string {
	b, err := schemaFiles.ReadFile("schemas/" + file)
	if err != nil {
		panic("declarations: built-in schema " + file + " is not embedded: " + err.Error())
	}
	return string(b)
}

var (
	builtinOnce     sync.Once
	manifestProgram *strictspec.Program
	projectProgram  *strictspec.Program
	claimsProgram   *strictspec.Program
	reportProgram   *strictspec.Program
)

func compileBuiltin(file string) *strictspec.Program {
	p, err := strictspec.CompileEmbedded(map[string]string{file: schemaSource(file)}, file)
	if err != nil {
		// The built-ins are part of this package; one failing the meta-schema
		// is a bug here, never a tool's condition.
		panic("declarations: built-in schema " + file + " fails the meta-schema: " + err.Error())
	}
	return p
}

func builtins() {
	builtinOnce.Do(func() {
		manifestProgram = compileBuiltin("declaration-manifest.schema.toml")
		projectProgram = compileBuiltin("declaration-project.schema.toml")
		claimsProgram = compileBuiltin("declaration-claims.schema.toml")
		reportProgram = compileBuiltin("declaration-report.schema.toml")
	})
}
