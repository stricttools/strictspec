package strictspec

import (
	_ "embed"
	"sync"
)

// LifecycleAndLicenseSchema is the source of the built-in lifecycle-and-license
// schema: the shape of a repository's
// .strictmetadata/lifecycle-and-license/lifecycle-and-license.toml. The
// lifecycle package reads and writes that record and adds the checks the schema
// cannot state.
//
//go:embed builtin/lifecycle-and-license.schema.toml
var LifecycleAndLicenseSchema string

var (
	lifecycleOnce    sync.Once
	lifecycleProgram *Program
)

// LifecycleAndLicenseProgram is the compiled built-in lifecycle-and-license
// schema.
func LifecycleAndLicenseProgram() *Program {
	lifecycleOnce.Do(func() {
		lifecycleProgram = compileBuiltin("lifecycle-and-license.schema.toml", LifecycleAndLicenseSchema)
	})
	return lifecycleProgram
}
