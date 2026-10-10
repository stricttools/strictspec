// Command strictspec is the toolchain CLI, built on strictcli (Go): flag
// conventions are enforced at registration, and the `help` and `version`
// commands are framework-provided (`strictspec help --json` prints the help document).
// Subcommands: gen (file-driven codegen from strictspec.toml), validate
// (interpreter-backed document validation), check (schema-authoring +
// generated-code freshness), init (scaffold a manifest), export (JSON Schema),
// and the migrate/diff/doc-diff analyses.
//
// Every command declares its effect classification. The four writing commands
// (gen, init, export, migrate) route every disk mutation through
// `ctx.Effects()`, so `--dry-run` -- which is framework-owned, never declared --
// records what would happen instead of performing it. A mutating command may
// not declare a value default for any flag or arg (strictcli's mutating-default
// ban): --manifest and --migrations are required, so the invocation states every
// path they write from; export's --output is optional, its absence meaning stdout.
package main

import (
	"github.com/stricttools/strictcli/go/strictcli"
	"github.com/stricttools/strictspec/go/strictspec"
)

func main() {
	newApp().Run()
}

// The two validate modes. They were a required mutex of two bools; the mutex
// group is gone from strictcli, and the site's truth is a member-spelled
// selector: each mode is still typed as its own flag (`--structural-only`,
// `--with-domain-checks`), the selector's own name `mode` is never typed, and
// exactly one election is enforced by the framework rather than by a hand guard
// in the handler.
//
// The evidence flags sit INSIDE the domain-checks scope, which is where they
// were always true: `--collection` and `--collection-root` mean nothing under
// `--structural-only`, and until now that combination was accepted and silently
// ignored. Under the scope it is a parse error naming both sides.
var (
	ModeStructuralOnly = strictcli.MemberChoice(
		strictcli.BoolFlag("structural-only", "Run the structural checks only",
			strictcli.Required()),
		"Run the structural checks only",
	)

	// Cross-document evidence: each --collection glob hosts the in-process
	// `documents-in(...)` resolver -- its matching documents become the
	// evidence set for collection-shaped cross-document forms (count-limit,
	// sum-limit, ...). Anchored at --collection-root, resolved lexicographically
	// (appendix-surface-syntax §5.1). Repeatable. Without it, a schema carrying
	// a collection-shaped cross-document form is a hard error (a resolver that
	// cannot be satisfied is never a skip).
	ModeWithDomainChecks = strictcli.MemberChoice(
		strictcli.BoolFlag("with-domain-checks", "Also run the constraint vocabulary checks",
			strictcli.Required()),
		"Also run the constraint vocabulary checks",
		strictcli.StringFlag("collection",
			"Glob hosting an in-process documents-in(...) evidence collection (repeatable)",
			strictcli.Repeatable(), strictcli.Unique(false), strictcli.Optional()),
		strictcli.StringFlag("collection-root",
			"Root the --collection globs are anchored at",
			strictcli.Default(".")),
	)
)

// newApp builds the CLI (used by main and by the registration/behavior tests).
// strictcli validates every flag and argument at registration time, so building
// the app is itself the registration-time conformance check.
func newApp() *strictcli.App {
	app := strictcli.NewApp("strictspec", strictspec.Version,
		"strictspec: strict, byte-identical schema validation and code generation")

	app.Command("gen", "Generate validator code from the manifest (strictspec.toml)",
		genHandler,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.StringFlag("manifest",
				"Path to the strictspec.toml manifest, such as strictspec.toml",
				strictcli.Required()),
		),
	)

	app.Command("validate", "Validate document(s) against a schema and render diagnostics",
		validateHandler,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file", strictcli.ArgRequired()),
			strictcli.NewArg("documents", "One or more document files to validate",
				strictcli.Variadic(), strictcli.ArgRequired()),
		),
		// Exactly one mode; the validate contract forces an explicit
		// structural-vs-domain choice, and there is no default.
		strictcli.WithFlags(
			strictcli.MemberChoiceFlag("mode", "Which check phases to run",
				strictcli.Required(), ModeStructuralOnly, ModeWithDomainChecks),
		),
	)

	app.Command("check", "Check schema authoring validity and generated-code freshness",
		checkHandler,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithFlags(
			strictcli.StringFlag("manifest", "Path to the strictspec.toml manifest",
				strictcli.Default("strictspec.toml")),
		),
	)

	app.Command("init", "Scaffold a minimal strictspec.toml manifest and .gitattributes",
		initHandler,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithFlags(
			strictcli.StringFlag("manifest",
				"Path to the strictspec.toml manifest to create, such as strictspec.toml",
				strictcli.Required()),
		),
	)

	app.Command("export", "Export a schema to JSON Schema (advisory; lossy per the constitution)",
		exportHandler,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file", strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("output", "Output path; omitted means stdout",
				strictcli.Optional()),
		),
	)

	// --- migrate / diff / doc-diff ------------------------------------------

	app.Command("migrate", "Migrate document(s) up to the schema's current format_version",
		migrateHandler,
		strictcli.WithEffect(strictcli.EffectMutating),
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the target (current-version) schema file",
				strictcli.ArgRequired()),
			strictcli.NewArg("documents", "One or more document files to migrate",
				strictcli.Variadic(), strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.IntFlag("to", "Target format_version (must equal the schema's current format_version)",
				strictcli.Required()),
			strictcli.StringFlag("migrations",
				"Directory of *.migration.toml files forming the chain (. for the current directory)",
				strictcli.Required()),
		),
	)

	app.Command("diff", "Compare a schema at two format_versions over a corpus; emit a certificate",
		diffHandler,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithArgs(
			strictcli.NewArg("old-schema", "Path to the schema at format_version N",
				strictcli.ArgRequired()),
			strictcli.NewArg("new-schema", "Path to the schema at format_version N+1",
				strictcli.ArgRequired()),
		),
		strictcli.WithFlags(
			strictcli.StringFlag("corpus", "Corpus glob (anchored at --corpus-root, lexicographic)",
				strictcli.Required()),
			strictcli.StringFlag("corpus-root", "Root the corpus glob is anchored at (. for the current directory)",
				strictcli.Required()),
			strictcli.StringFlag("migration", "Path to the migration file between the two versions",
				strictcli.Optional()),
			strictcli.StringFlag("adjudication", "Path to a committed adjudication file to acknowledge",
				strictcli.Optional()),
			strictcli.BoolFlag("same-version", "Force same-version narrowing scan (no migration)",
				strictcli.Default(false)),
		),
	)

	app.Command("doc-diff", "Structural per-path delta of two documents of one schema",
		docDiffHandler,
		strictcli.WithEffect(strictcli.EffectReadOnly),
		strictcli.WithArgs(
			strictcli.NewArg("schema", "Path to the schema file", strictcli.ArgRequired()),
			strictcli.NewArg("old-document", "Path to the OLD document", strictcli.ArgRequired()),
			strictcli.NewArg("new-document", "Path to the NEW document", strictcli.ArgRequired()),
		),
	)

	return app
}
