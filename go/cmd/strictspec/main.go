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
		// Cross-document evidence: each --collection glob hosts the in-process
		// `documents-in(...)` resolver — its matching documents become the
		// evidence set for collection-shaped cross-document forms (count-limit,
		// sum-limit, ...). Anchored at --collection-root, resolved lexicographically
		// (appendix-surface-syntax §5.1). Repeatable. Without it, a schema carrying
		// a collection-shaped cross-document form under --with-domain-checks is a
		// hard error (a resolver that cannot be satisfied is never a skip).
		strictcli.WithFlags(
			strictcli.StringFlag("collection",
				"Glob hosting an in-process documents-in(...) evidence collection (repeatable)",
				strictcli.Repeatable(), strictcli.Unique(false)),
			strictcli.StringFlag("collection-root",
				"Root the --collection globs are anchored at (default: cwd)",
				strictcli.Default(".")),
		),
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

	// --- Phase 6: migrate / diff / doc-diff ---------------------------------

	app.Command("migrate", "Migrate document(s) up to the schema's current format_version",
		migrateHandler,
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the target (current-version) schema file"),
			strictcli.NewArg("documents", "One or more document files to migrate", strictcli.Variadic()),
		),
		strictcli.WithFlags(
			strictcli.IntFlag("to", "Target format_version (must equal the schema's current format_version)"),
			strictcli.StringFlag("migrations", "Directory of *.migration.toml files forming the chain",
				strictcli.Default(".")),
			strictcli.BoolFlag("dry-run", "Render the would-be output and diagnostics without writing",
				strictcli.Default(false)),
		),
	)

	app.Command("diff", "Compare a schema at two format_versions over a corpus; emit a certificate",
		diffHandler,
		strictcli.WithArgs(
			strictcli.NewArg("old-schema", "Path to the schema at format_version N"),
			strictcli.NewArg("new-schema", "Path to the schema at format_version N+1"),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("corpus", "REQUIRED corpus glob (anchored at --corpus-root, lexicographic)"),
			strictcli.StringFlag("corpus-root", "Root the corpus glob is anchored at (default: cwd)",
				strictcli.Default(".")),
			strictcli.StringFlag("migration", "Path to the migration file between the two versions",
				strictcli.Default("")),
			strictcli.StringFlag("adjudication", "Path to a committed adjudication file to acknowledge",
				strictcli.Default("")),
			strictcli.BoolFlag("same-version", "Force same-version narrowing scan (no migration)",
				strictcli.Default(false)),
		),
	)

	app.Command("doc-diff", "Structural per-path delta of two documents of one schema",
		docDiffHandler,
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file"),
			strictcli.NewArg("old-document", "Path to the OLD document"),
			strictcli.NewArg("new-document", "Path to the NEW document"),
		),
	)

	return app
}
