// Command strictspec is the toolchain CLI, built on strictcli (Go): flag
// conventions are enforced at registration, and `--dump-schema` is auto-injected.
// Phase 5.5 subcommands: gen (file-driven codegen from strictspec.toml),
// validate (interpreter-backed document validation), check (schema-authoring +
// generated-code freshness), init (scaffold a manifest), and export (JSON Schema).
// migrate/diff/doc-diff are Phase 6 — absent, not stubbed.
package main

import (
	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/strictspec/go/strictspec"
)

func main() {
	newApp().Run()
}

// newApp builds the CLI (used by main and by the registration/behavior tests).
// strictcli validates every flag and argument at registration time, so building
// the app is itself the registration-time conformance check.
func newApp() *strictcli.App {
	app := strictcli.NewApp("strictspec", strictspec.Version,
		"strictspec: strict, byte-identical schema validation and code generation")

	app.Command("gen", "Generate validator code from the manifest (strictspec.toml)",
		genHandler,
		strictcli.WithFlags(
			strictcli.StringFlag("manifest", "Path to the strictspec.toml manifest",
				strictcli.Default("strictspec.toml")),
		),
	)

	app.Command("validate", "Validate document(s) against a schema and render diagnostics",
		validateHandler,
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file"),
			strictcli.NewArg("documents", "One or more document files to validate",
				strictcli.Variadic()),
		),
		// The two modes are a required mutex (exactly one; no default) — the
		// validate contract forces an explicit structural-vs-domain choice.
		strictcli.WithMutex(strictcli.MutexGroup{Flags: []strictcli.Flag{
			strictcli.BoolFlag("structural-only", "Run phase-1 structural checks only"),
			strictcli.BoolFlag("with-domain-checks", "Also run the phase-2 constraint vocabulary"),
		}}),
	)

	app.Command("check", "Check schema authoring validity and generated-code freshness",
		checkHandler,
		strictcli.WithFlags(
			strictcli.StringFlag("manifest", "Path to the strictspec.toml manifest",
				strictcli.Default("strictspec.toml")),
		),
	)

	app.Command("init", "Scaffold a minimal strictspec.toml manifest and .gitattributes",
		initHandler,
		strictcli.WithFlags(
			strictcli.StringFlag("manifest", "Path to the strictspec.toml manifest to create",
				strictcli.Default("strictspec.toml")),
		),
	)

	app.Command("export", "Export a schema to JSON Schema (advisory; lossy per the constitution)",
		exportHandler,
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file"),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("output", "Output path (default: stdout)",
				strictcli.Default("")),
		),
	)

	return app
}
