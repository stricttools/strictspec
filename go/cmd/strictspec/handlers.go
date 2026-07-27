package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/emit"
	"github.com/smm-h/strictspec/go/internal/export"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/manifest"
	"github.com/smm-h/strictspec/go/internal/migrate"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
	"github.com/smm-h/strictspec/go/strictspec"
)

// --- gen --------------------------------------------------------------------

func genHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	manifestPath := strictcli.Get[string](kwargs, "manifest")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	dir := filepath.Dir(manifestPath)
	var generatedPaths []string
	for _, se := range m.Schemas {
		schemaPath := filepath.Join(dir, se.Path)
		for _, tgt := range se.Targets {
			if tgt.Lang != "go" {
				ctx.Info(fmt.Sprintf("skipping %s target for %s: not emitted by the Go toolchain (use the %s toolchain)",
					tgt.Lang, se.Path, tgt.Lang))
				continue
			}
			loaded, lerr := emit.LoadForEmit(schemaPath)
			if lerr != nil {
				ctx.Error(lerr.Error())
				return strictcli.Exit(1)
			}
			if len(loaded.Diags) > 0 {
				ctx.Error(fmt.Sprintf("schema %s fails the meta-schema (not emittable):", se.Path))
				printDiags(ctx, loaded.Diags)
				return strictcli.Exit(1)
			}
			pkg := tgt.Package
			if pkg == "" {
				pkg = emitPackageDefault(loaded.Schema.Name)
			}
			src, gerr := emit.GenerateGo(loaded.Schema, loaded.Scalars, emit.GoParams{
				Package:          pkg,
				MainFile:         loaded.MainFile,
				Files:            loaded.Files,
				GeneratorVersion: strictspec.Version,
				RegenCommand:     "strictspec gen --manifest " + filepath.Base(manifestPath),
			})
			if gerr != nil {
				ctx.Error(gerr.Error())
				return strictcli.Exit(1)
			}
			outPath := filepath.Join(dir, tgt.Output)
			if werr := writeGenerated(outPath, src); werr != nil {
				ctx.Error(werr.Error())
				return strictcli.Exit(1)
			}
			ctx.Info("generated " + tgt.Output)
			generatedPaths = append(generatedPaths, tgt.Output)
		}
	}
	if err := ensureGitattributes(dir, generatedPaths); err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

// --- validate ---------------------------------------------------------------

func validateHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	schemaPath := strictcli.Get[string](kwargs, "schema")
	docs := stringSlice(kwargs, "documents")
	structuralOnly, _ := strictcli.GetOpt[bool](kwargs, "structural_only")
	withDomain, _ := strictcli.GetOpt[bool](kwargs, "with_domain_checks")
	if structuralOnly == withDomain {
		// Mutex should prevent this, but guard: exactly one mode is required.
		ctx.Error("validate requires exactly one of --structural-only or --with-domain-checks")
		return strictcli.Exit(2)
	}
	if len(docs) == 0 {
		ctx.Error("validate requires at least one document")
		return strictcli.Exit(2)
	}

	s, sdiags, err := schema.LoadFile(schemaPath)
	if err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) > 0 {
		ctx.Error("schema fails the meta-schema:")
		printDiags(ctx, sdiags)
		return strictcli.Exit(1)
	}
	if withDomain && hasCrossDocumentForms(s) {
		ctx.Error("--with-domain-checks: this build has no evidence resolver for the schema's " +
			"cross-document constraints (count-limit / sum-limit). Cross-document domain checks are " +
			"unavailable here; a resolver that cannot be satisfied is a hard error, never a skip.")
		return strictcli.Exit(1)
	}
	scalars := schema.LoadManifestScalars(s.Dir)
	prog := ir.Compile(s, scalars)

	anyBad := false
	for _, docPath := range docs {
		src, rerr := os.ReadFile(docPath)
		if rerr != nil {
			ctx.Error(rerr.Error())
			anyBad = true
			continue
		}
		diags := validateOne(prog, src, syntaxOf(docPath), structuralOnly)
		if len(diags) == 0 {
			ctx.Info(fmt.Sprintf("%s: OK", docPath))
			continue
		}
		anyBad = true
		ctx.Error(fmt.Sprintf("%s: %d diagnostic(s)", docPath, len(diags)))
		printDiags(ctx, diags)
	}
	if anyBad {
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

func validateOne(prog *ir.Program, src []byte, syntax string, structuralOnly bool) []diag.Diagnostic {
	switch syntax {
	case "jsonl":
		docs, perr := jsondoc.ParseLines(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}
		}
		starts := lineStarts(src)
		var out []diag.Diagnostic
		for i, d := range docs {
			ls := 0
			if i < len(starts) {
				ls = starts[i]
			}
			out = append(out, ir.Execute(prog, d.Root, ir.ExecOptions{
				Format: doc.FormatJSONL, StructuralOnly: structuralOnly,
				JSONL: true, Line: i + 1, LineStart: ls,
			})...)
		}
		return out
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}
		}
		return ir.Execute(prog, d.Root, ir.ExecOptions{Format: doc.FormatTOML, StructuralOnly: structuralOnly})
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}
		}
		return ir.Execute(prog, d.Root, ir.ExecOptions{Format: doc.FormatJSON, StructuralOnly: structuralOnly})
	}
}

// --- check ------------------------------------------------------------------

func checkHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	manifestPath := strictcli.Get[string](kwargs, "manifest")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	dir := filepath.Dir(manifestPath)
	failed := false
	for _, se := range m.Schemas {
		schemaPath := filepath.Join(dir, se.Path)
		loaded, lerr := emit.LoadForEmit(schemaPath)
		if lerr != nil {
			ctx.Error(lerr.Error())
			failed = true
			continue
		}
		// Schema authoring validation.
		if len(loaded.Diags) > 0 {
			ctx.Error(fmt.Sprintf("%s: fails the meta-schema:", se.Path))
			printDiags(ctx, loaded.Diags)
			failed = true
			continue
		}
		// Blind-spot inventory.
		printBlindSpots(ctx, se.Path, loaded.Schema)
		// Generated-code freshness (drift gate).
		for _, tgt := range se.Targets {
			if tgt.Lang != "go" {
				continue
			}
			pkg := tgt.Package
			if pkg == "" {
				pkg = emitPackageDefault(loaded.Schema.Name)
			}
			want, gerr := emit.GenerateGo(loaded.Schema, loaded.Scalars, emit.GoParams{
				Package:          pkg,
				MainFile:         loaded.MainFile,
				Files:            loaded.Files,
				GeneratorVersion: strictspec.Version,
				RegenCommand:     "strictspec gen --manifest " + filepath.Base(manifestPath),
			})
			if gerr != nil {
				ctx.Error(gerr.Error())
				failed = true
				continue
			}
			got, rerr := os.ReadFile(filepath.Join(dir, tgt.Output))
			if rerr != nil {
				ctx.Error(fmt.Sprintf("%s: generated file %s is missing — run `strictspec gen`", se.Path, tgt.Output))
				failed = true
				continue
			}
			if string(got) != want {
				ctx.Error(fmt.Sprintf("%s: generated file %s is STALE (drift) — run `strictspec gen`", se.Path, tgt.Output))
				failed = true
				continue
			}
			ctx.Info(fmt.Sprintf("%s -> %s: fresh", se.Path, tgt.Output))
		}
	}
	// Migration files are documents of a toolchain-shipped schema; `check`
	// validates their authoring (op vocabulary, required keys, restricted
	// predicates) exactly as it validates schemas.
	if checkMigrationFiles(ctx, dir) {
		failed = true
	}
	if failed {
		return strictcli.Exit(1)
	}
	return strictcli.Exit(0)
}

// checkMigrationFiles validates every *.migration.toml under dir and returns
// whether any failed authoring validation.
func checkMigrationFiles(ctx *strictcli.Context, dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	anyFailed := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".migration.toml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			ctx.Error(rerr.Error())
			anyFailed = true
			continue
		}
		_, mdiags, perr := migrate.ParseMigration(src, path)
		if perr != nil {
			ctx.Error(fmt.Sprintf("%s: unparseable: %v", e.Name(), perr))
			anyFailed = true
			continue
		}
		if len(mdiags) > 0 {
			ctx.Error(fmt.Sprintf("%s: fails migration-file authoring validation:", e.Name()))
			printDiags(ctx, mdiags)
			anyFailed = true
			continue
		}
		ctx.Info(fmt.Sprintf("%s: valid migration file", e.Name()))
	}
	return anyFailed
}

// --- init -------------------------------------------------------------------

func initHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	manifestPath := strictcli.Get[string](kwargs, "manifest")
	if _, err := os.Stat(manifestPath); err == nil {
		ctx.Error(fmt.Sprintf("%s already exists (init refuses to overwrite)", manifestPath))
		return strictcli.Exit(1)
	}
	if err := os.WriteFile(manifestPath, []byte(manifestSkeleton), 0o644); err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	dir := filepath.Dir(manifestPath)
	if err := ensureGitattributes(dir, nil); err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	ctx.Info("wrote " + manifestPath)
	return strictcli.Exit(0)
}

// --- export -----------------------------------------------------------------

func exportHandler(ctx *strictcli.Context, kwargs map[string]interface{}) strictcli.Outcome {
	schemaPath := strictcli.Get[string](kwargs, "schema")
	output, _ := strictcli.GetOpt[string](kwargs, "output")

	s, sdiags, err := schema.LoadFile(schemaPath)
	if err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	sdiags = append(sdiags, schema.ResolveImports(s)...)
	if len(sdiags) > 0 {
		ctx.Error("schema fails the meta-schema:")
		printDiags(ctx, sdiags)
		return strictcli.Exit(1)
	}
	scalars := schema.LoadManifestScalars(s.Dir)
	out, eerr := export.ToJSONSchema(s, scalars)
	if eerr != nil {
		ctx.Error(eerr.Error())
		return strictcli.Exit(1)
	}
	if output == "" {
		os.Stdout.Write(out)
		os.Stdout.Write([]byte("\n"))
		return strictcli.Exit(0)
	}
	if err := os.WriteFile(output, append(out, '\n'), 0o644); err != nil {
		ctx.Error(err.Error())
		return strictcli.Exit(1)
	}
	ctx.Info("wrote " + output)
	return strictcli.Exit(0)
}

// --- helpers ----------------------------------------------------------------

const manifestSkeleton = `# strictspec manifest (strictspec.toml).
# Generation is file-driven: declare each schema and its targets, then run
#   strictspec gen
format_version = 1

# One [[schemas]] block per schema file. Each names its generation targets.
# [[schemas]]
# path = "schemas/example.schema.toml"
#
#   [[schemas.targets]]
#   lang    = "go"
#   output  = "internal/example/example_gen.go"
#   package = "example"
`

func printDiags(ctx *strictcli.Context, diags []diag.Diagnostic) {
	for _, d := range diags {
		ctx.Error(fmt.Sprintf("  %s at %s: %s", d.Code, d.Path.Render(), safeRender(d)))
	}
}

// safeRender renders a diagnostic message, tolerating authoring diagnostics that
// carry no catalogue template (e.g. reader-only slots) by falling back to code.
func safeRender(d diag.Diagnostic) (msg string) {
	defer func() {
		if recover() != nil {
			msg = d.Code
		}
	}()
	return render.Render(d)
}

func printBlindSpots(ctx *strictcli.Context, schemaName string, s *schema.Schema) {
	var unchecked, consumerChecks []string
	for _, name := range s.TypeOrder {
		walkOpaque(s.Types[name], func(path, kind, detail string) {
			if kind == "unchecked" {
				unchecked = append(unchecked, fmt.Sprintf("    %s (reason: %s)", path, detail))
			} else {
				consumerChecks = append(consumerChecks, fmt.Sprintf("    %s (check: %s)", path, detail))
			}
		})
	}
	if len(unchecked) == 0 && len(consumerChecks) == 0 {
		return
	}
	ctx.Info(fmt.Sprintf("%s: blind-spot inventory", schemaName))
	if len(unchecked) > 0 {
		ctx.Info("  unchecked opaque leaves:")
		for _, u := range unchecked {
			ctx.Info(u)
		}
	}
	if len(consumerChecks) > 0 {
		ctx.Info("  consumer-check declarations:")
		for _, c := range consumerChecks {
			ctx.Info(c)
		}
	}
}

func walkOpaque(t *schema.Type, emit func(path, kind, detail string)) {
	if t == nil {
		return
	}
	if t.Kind == schema.KindOpaque {
		p := t.SchemaPath.Render()
		if t.HasConsumerCheck {
			emit(p, "consumer_check", t.ConsumerCheck)
		} else if t.Unchecked {
			emit(p, "unchecked", t.UncheckedReason)
		}
	}
	for _, f := range t.Fields {
		walkOpaque(f.Type, emit)
	}
	for _, a := range t.Arms {
		walkOpaque(a.Type, emit)
	}
	walkOpaque(t.Item, emit)
	walkOpaque(t.Value, emit)
	walkOpaque(t.Inner, emit)
}

func hasCrossDocumentForms(s *schema.Schema) bool {
	found := false
	for _, name := range s.TypeOrder {
		walkConstraints(s.Types[name], func(form string) {
			switch form {
			case "count-limit", "sum-limit", "set-coverage",
				"cross-collection-unique", "named-reference-must-resolve":
				found = true
			}
		})
	}
	return found
}

func walkConstraints(t *schema.Type, emit func(form string)) {
	if t == nil {
		return
	}
	for _, c := range t.Constraints {
		emit(c.Form)
	}
	for _, f := range t.Fields {
		walkConstraints(f.Type, emit)
	}
	for _, a := range t.Arms {
		walkConstraints(a.Type, emit)
	}
	walkConstraints(t.Item, emit)
	walkConstraints(t.Value, emit)
	walkConstraints(t.Inner, emit)
}

func writeGenerated(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// chmod before overwrite (generated files land 0444).
	if _, err := os.Stat(path); err == nil {
		_ = os.Chmod(path, 0o644)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o444)
}

// ensureGitattributes appends LF rules for the generated paths (byte-compare
// dies under autocrlf) if not already present.
func ensureGitattributes(dir string, paths []string) error {
	gaPath := filepath.Join(dir, ".gitattributes")
	existing := ""
	if b, err := os.ReadFile(gaPath); err == nil {
		existing = string(b)
	}
	var add []string
	for _, p := range paths {
		line := p + " text eol=lf linguist-generated=true"
		if !strings.Contains(existing, p+" ") {
			add = append(add, line)
		}
	}
	if len(add) == 0 {
		return nil
	}
	f, err := os.OpenFile(gaPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		f.WriteString("\n")
	}
	for _, line := range add {
		f.WriteString(line + "\n")
	}
	return nil
}

func syntaxOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return "toml"
	case ".jsonl":
		return "jsonl"
	default:
		return "json"
	}
}

func stringSlice(kwargs map[string]interface{}, name string) []string {
	v, ok := kwargs[name]
	if !ok || v == nil {
		return nil
	}
	switch xs := v.(type) {
	case []string:
		return xs
	case []interface{}:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func emitPackageDefault(schemaName string) string {
	var b strings.Builder
	for _, r := range schemaName {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		return "generated"
	}
	return out
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func parseDiag(pe *doc.ParseError) diag.Diagnostic {
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
	}
	return diag.Diagnostic{
		Code: code,
		Path: diag.NewPath(),
		Slots: map[string]diag.Slot{
			"detail": diag.SlotString{S: pe.Message},
			"line":   diag.SlotInt{N: int64(pe.Position.Line)},
		},
	}
}
