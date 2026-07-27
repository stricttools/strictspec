package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/diffeng"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/docdiff"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/migrate"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/write"
	"github.com/smm-h/strictspec/go/strictspec"
)

// --- migrate ----------------------------------------------------------------

func migrateHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	schemaPath := strictcli.Get[string](kwargs, "schema")
	to := int64(strictcli.Get[int](kwargs, "to"))
	migDir := strictcli.Get[string](kwargs, "migrations")
	dryRun, _ := strictcli.GetOpt[bool](kwargs, "dry_run")
	docs := stringSlice(kwargs, "documents")
	if len(docs) == 0 {
		ctx.Error("migrate requires at least one document")
		return strictcli.Exit(2)
	}

	prog, err := loadProgram(ctx, schemaPath)
	if err != nil {
		return strictcli.Exit(1)
	}
	if prog.FormatVersion() != to {
		ctx.Error(fmt.Sprintf("--to %d does not match schema %s current format_version %d",
			to, prog.SchemaName(), prog.FormatVersion()))
		return strictcli.Exit(2)
	}
	migs, merr := loadMigrations(ctx, migDir)
	if merr != nil {
		return strictcli.Exit(1)
	}

	// Compute every output in memory first (all-or-nothing atomicity).
	type pending struct {
		path   string
		output []byte
	}
	var outputs []pending
	for _, docPath := range docs {
		src, rerr := os.ReadFile(docPath)
		if rerr != nil {
			ctx.Error(rerr.Error())
			return strictcli.Exit(1)
		}
		format := formatOf(docPath)
		cur, ok := formatVersionOf(format, src)
		if !ok {
			ctx.Error(fmt.Sprintf("%s: cannot read integer format_version", docPath))
			return strictcli.Exit(1)
		}
		if cur == to {
			ctx.Error(renderDiag(diag.Diagnostic{
				Code: "STRICTSPEC_MIGRATE_ON_CURRENT",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"expected": diag.SlotVersion{V: to},
				},
			}))
			return strictcli.Exit(1)
		}
		chain, ok := selectChain(migs, cur, to)
		if !ok {
			// No registered migration set bridges the document's version to the
			// target: the catalogued set-level code (migset named after the schema,
			// matching the version-gate's STRICTSPEC_GATE_UNSUPPORTED convention).
			ctx.Error(fmt.Sprintf("%s: %s", docPath, renderDiag(diag.Diagnostic{
				Code: "STRICTSPEC_MIGRATE_UNKNOWN_SET",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"migset": diag.SlotIdentifier{Name: prog.SchemaName()},
					"schema": diag.SlotIdentifier{Name: prog.SchemaName()},
				},
			})))
			return strictcli.Exit(1)
		}
		var res migrate.Result
		if format == doc.FormatJSONL {
			res = migrate.MigrateJSONL(chain, prog, src)
		} else {
			res = migrate.MigrateDocument(chain, prog, format, src)
		}
		if len(res.Diags) > 0 {
			ctx.Error(fmt.Sprintf("%s: migration failed:", docPath))
			printDiags(ctx, res.Diags)
			return strictcli.Exit(1)
		}
		outputs = append(outputs, pending{path: docPath, output: res.Output})
	}

	if dryRun {
		for _, p := range outputs {
			ctx.Info(fmt.Sprintf("--- %s (dry-run, would write) ---", p.path))
			os.Stdout.Write(p.output)
			if len(p.output) == 0 || p.output[len(p.output)-1] != '\n' {
				os.Stdout.Write([]byte("\n"))
			}
		}
		return strictcli.Exit(0)
	}

	// Rename sweep only after all succeeded: write temp then rename each.
	for _, p := range outputs {
		tmp := p.path + ".strictspec-migrate.tmp"
		if err := os.WriteFile(tmp, p.output, 0o644); err != nil {
			ctx.Error(err.Error())
			return strictcli.Exit(1)
		}
		if err := os.Rename(tmp, p.path); err != nil {
			ctx.Error(err.Error())
			return strictcli.Exit(1)
		}
		ctx.Info(fmt.Sprintf("migrated %s to format_version %d", p.path, to))
	}
	return strictcli.Exit(0)
}

// --- diff -------------------------------------------------------------------

func diffHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	oldSchemaPath := strictcli.Get[string](kwargs, "old-schema")
	newSchemaPath := strictcli.Get[string](kwargs, "new-schema")
	corpus := strictcli.Get[string](kwargs, "corpus")
	migPath, _ := strictcli.GetOpt[string](kwargs, "migration")
	adjPath, _ := strictcli.GetOpt[string](kwargs, "adjudication")
	sameVersionFlag, _ := strictcli.GetOpt[bool](kwargs, "same_version")
	root, _ := strictcli.GetOpt[string](kwargs, "corpus_root")
	if root == "" {
		root = "."
	}

	oldProg, err := loadProgram(ctx, oldSchemaPath)
	if err != nil {
		return strictcli.Exit(1)
	}
	newProg, err := loadProgram(ctx, newSchemaPath)
	if err != nil {
		return strictcli.Exit(1)
	}
	var mig *migrate.Migration
	if migPath != "" {
		m, mdiags, merr := loadMigrationFile(migPath)
		if merr != nil {
			ctx.Error(merr.Error())
			return strictcli.Exit(1)
		}
		if len(mdiags) > 0 {
			ctx.Error(fmt.Sprintf("migration %s has authoring errors:", migPath))
			printDiags(ctx, mdiags)
			return strictcli.Exit(1)
		}
		mig = m
	}
	sameVersion := sameVersionFlag || (mig == nil && oldProg.FormatVersion() == newProg.FormatVersion())

	files, ferr := diffeng.ResolveGlob(root, corpus)
	if ferr != nil {
		ctx.Error(ferr.Error())
		return strictcli.Exit(1)
	}
	cert, violations := diffeng.Run(diffeng.Inputs{
		SchemaID:    newProg.SchemaName(),
		OldProg:     oldProg,
		NewProg:     newProg,
		OldFV:       oldProg.FormatVersion(),
		NewFV:       newProg.FormatVersion(),
		Migration:   mig,
		Glob:        corpus,
		Files:       files,
		Release:     strictspec.Version,
		SameVersion: sameVersion,
	})
	if cert == nil {
		printDiags(ctx, violations)
		return strictcli.Exit(1)
	}
	if err := diffeng.SelfValidate(cert); err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	if adjPath != "" {
		adjSrc, aerr := os.ReadFile(adjPath)
		if aerr != nil {
			ctx.Error(aerr.Error())
			return strictcli.Exit(1)
		}
		if adiags := diffeng.ParseAdjudication(adjSrc, adjPath); len(adiags) > 0 {
			printDiags(ctx, adiags)
			return strictcli.Exit(1)
		}
		ctx.Info("adjudication acknowledged: " + adjPath)
	}
	out, _ := json.MarshalIndent(cert, "", "  ")
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
	if len(violations) > 0 {
		ctx.Error("diff found violated claims (blocks release):")
		printDiags(ctx, violations)
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

// --- doc-diff ---------------------------------------------------------------

func docDiffHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	schemaPath := strictcli.Get[string](kwargs, "schema")
	oldDoc := strictcli.Get[string](kwargs, "old-document")
	newDoc := strictcli.Get[string](kwargs, "new-document")

	prog, err := loadProgram(ctx, schemaPath)
	if err != nil {
		return strictcli.Exit(1)
	}
	oldRoot, oformat, oerr := parseDocFile(oldDoc)
	if oerr != nil {
		ctx.Error(oerr.Error())
		return strictcli.Exit(1)
	}
	newRoot, _, nerr := parseDocFile(newDoc)
	if nerr != nil {
		ctx.Error(nerr.Error())
		return strictcli.Exit(1)
	}
	res, diags := docdiff.Compute(prog, prog.Schema, oformat, oldDoc, oldRoot, newDoc, newRoot)
	if len(diags) > 0 {
		printDiags(ctx, diags)
		return strictcli.Exit(1)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
	return strictcli.Exit(0)
}

// --- shared helpers ---------------------------------------------------------

func loadProgram(ctx *strictcli.Context, schemaPath string) (*ir.Program, error) {
	s, sdiags, err := schema.LoadFile(schemaPath)
	if err != nil {
		ctx.Error(err.Error())
		return nil, err
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) > 0 {
		ctx.Error("schema fails the meta-schema:")
		printDiags(ctx, sdiags)
		return nil, fmt.Errorf("schema invalid")
	}
	scalars := schema.LoadManifestScalars(s.Dir)
	return ir.Compile(s, scalars), nil
}

func loadMigrations(ctx *strictcli.Context, dir string) ([]*migrate.Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		ctx.Error(err.Error())
		return nil, err
	}
	var migs []*migrate.Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".migration.toml") {
			continue
		}
		m, mdiags, merr := loadMigrationFile(filepath.Join(dir, e.Name()))
		if merr != nil {
			ctx.Error(merr.Error())
			return nil, merr
		}
		if len(mdiags) > 0 {
			ctx.Error(fmt.Sprintf("%s has authoring errors:", e.Name()))
			printDiags(ctx, mdiags)
			return nil, fmt.Errorf("migration authoring error")
		}
		migs = append(migs, m)
	}
	return migs, nil
}

func loadMigrationFile(path string) (*migrate.Migration, []diag.Diagnostic, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return migrate.ParseMigration(src, path)
}

// selectChain greedily follows migrations from -> ... -> to.
func selectChain(migs []*migrate.Migration, from, to int64) ([]*migrate.Migration, bool) {
	cur := from
	var chain []*migrate.Migration
	guard := 0
	for cur != to {
		found := false
		for _, m := range migs {
			if m.From == cur {
				chain = append(chain, m)
				cur = m.To
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
		guard++
		if guard > len(migs)+1 {
			return nil, false
		}
	}
	return chain, true
}

func formatOf(path string) doc.Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return doc.FormatTOML
	case ".jsonl":
		return doc.FormatJSONL
	default:
		return doc.FormatJSON
	}
}

func parseDocFile(path string) (doc.Node, doc.Format, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	format := formatOf(path)
	root, perr := parseRoot(format, src)
	if perr != nil {
		return nil, "", perr
	}
	return root, format, nil
}

func renderDiag(d diag.Diagnostic) string {
	return d.Code + " at " + d.Path.Render() + ": " + safeRender(d)
}

// parseRoot parses source into a root node for the given format (JSONL parses
// the first line as JSON — per-line handling lives in the migration engine).
func parseRoot(format doc.Format, src []byte) (doc.Node, error) {
	if format == doc.FormatJSONL {
		if i := indexByte(src, '\n'); i >= 0 {
			src = src[:i]
		}
	}
	fmtToUse := format
	if fmtToUse == doc.FormatJSONL {
		fmtToUse = doc.FormatJSON
	}
	wd, err := write.New(fmtToUse, src)
	if err != nil {
		return nil, err
	}
	return wd.Root(), nil
}

// formatVersionOf reads a document's integer format_version (the first line's for
// JSONL).
func formatVersionOf(format doc.Format, src []byte) (int64, bool) {
	root, err := parseRoot(format, src)
	if err != nil || root == nil || root.Kind() != doc.Record {
		return 0, false
	}
	for _, e := range root.Entries() {
		if e.Key == "format_version" && e.Value.Kind() == doc.Integer {
			return parseIntLexeme(e.Value.Lexeme())
		}
	}
	return 0, false
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func parseIntLexeme(s string) (int64, bool) {
	var n int64
	neg := false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}
