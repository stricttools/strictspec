package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/smm-h/strictcli/go/strictcli"
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/diffeng"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/emit"
	"github.com/smm-h/strictspec/go/internal/export"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/manifest"
	"github.com/smm-h/strictspec/go/internal/migrate"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/strdecode"
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
	regenCmd := "strictspec gen --manifest " + filepath.Base(manifestPath)
	var generatedPaths []string
	for _, se := range m.Schemas {
		schemaPath := filepath.Join(dir, se.Path)
		if len(se.Targets) == 0 {
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
		for _, tgt := range se.Targets {
			src, gerr := emitTarget(loaded, tgt, regenCmd)
			if gerr != nil {
				ctx.Error(fmt.Sprintf("%s: %s", se.Path, gerr.Error()))
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

// emitTarget dispatches one generation target to its language emitter. The
// manifest's target list drives which emitters run; a non-configured target is
// simply absent (never touched); an UNKNOWN target lang is a hard error. This is
// the single point where `strictspec gen` and `strictspec check` agree on what a
// target produces, so the freshness (drift) gate compares against the exact bytes
// gen would write.
func emitTarget(loaded *emit.Loaded, tgt manifest.TargetEntry, regenCmd string) (string, error) {
	switch tgt.Lang {
	case "go":
		pkg := tgt.Package
		if pkg == "" {
			pkg = emitPackageDefault(loaded.Schema.Name)
		}
		return emit.GenerateGo(loaded.Schema, loaded.Scalars, emit.GoParams{
			Package:          pkg,
			MainFile:         loaded.MainFile,
			Files:            loaded.Files,
			GeneratorVersion: strictspec.Version,
			RegenCommand:     regenCmd,
		})
	case "python":
		return emit.GeneratePython(loaded.Schema, emit.PyParams{
			MainFile:         loaded.MainFile,
			Files:            loaded.Files,
			GeneratorVersion: strictspec.Version,
			RegenCommand:     regenCmd,
		})
	case "ts":
		// A TS target REQUIRES a schema-wide `safe_integers = true` declaration
		// (ts/DESIGN.md — Numbers in TS): a TS target without it is a hard error at
		// generation time, telling the author to add it. This is enforced here (gen
		// orchestration), not in the emitter, so direct emitter callers stay free.
		if !loaded.Schema.SafeIntegers {
			return "", diagError(diag.Diagnostic{
				Code:  "STRICTSPEC_SCHEMA_TS_WITHOUT_SAFE_INTEGERS",
				Path:  diag.NewPath(),
				Slots: map[string]diag.Slot{"schema": diag.SlotIdentifier{Name: loaded.Schema.Name}},
			})
		}
		return emit.GenerateTypeScript(loaded.Schema, emit.TSParams{
			MainFile:         loaded.MainFile,
			Files:            loaded.Files,
			GeneratorVersion: strictspec.Version,
			RegenCommand:     regenCmd,
		})
	default:
		return "", fmt.Errorf("unknown generation target lang %q (known: go, python, ts)", tgt.Lang)
	}
}

// diagError renders a catalogued diagnostic as a CLI hard error, so orchestration
// errors carry the pinned message text rather than an ad-hoc string.
func diagError(d diag.Diagnostic) error {
	return fmt.Errorf("%s", render.Render(d))
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
	// Cross-document domain checks: host each collection-shaped resolver
	// (documents-in(glob)) in-process from the --collection evidence set. A
	// resolver that isn't collection-shaped, or a collection-shaped resolver
	// with no --collection to satisfy it, is a hard error naming the resolver —
	// never a silent skip.
	var evidence map[string][]map[string]any
	if withDomain {
		resolvers := crossDocResolvers(s)
		var collectionShaped []string
		for _, r := range resolvers {
			if _, ok := parseCollectionResolver(r); ok {
				collectionShaped = append(collectionShaped, r)
				continue
			}
			ctx.Error(fmt.Sprintf("--with-domain-checks: cross-document resolver %q is not a "+
				"collection-shaped documents-in(...) resolver; this build has no host for it "+
				"(a resolver that cannot be satisfied is a hard error, never a skip)", r))
			return strictcli.Exit(1)
		}
		if len(collectionShaped) > 0 {
			collections := stringSlice(kwargs, "collection")
			if len(collections) == 0 {
				ctx.Error(fmt.Sprintf("--with-domain-checks: the schema's cross-document "+
					"constraint(s) require evidence resolver(s) %s; pass --collection <glob> to host "+
					"the collection in-process. A collection-shaped resolver with no --collection is a "+
					"hard error, never a skip.", strings.Join(collectionShaped, ", ")))
				return strictcli.Exit(1)
			}
			collRoot, _ := strictcli.GetOpt[string](kwargs, "collection_root")
			if collRoot == "" {
				collRoot = "."
			}
			evDocs, everr := loadCollectionEvidence(collRoot, collections)
			if everr != nil {
				ctx.Error(everr.Error())
				return strictcli.Exit(1)
			}
			evidence = map[string][]map[string]any{}
			for _, r := range collectionShaped {
				evidence[r] = evDocs
			}
		}
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
		diags := validateOne(prog, src, syntaxOf(docPath), structuralOnly, evidence)
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

func validateOne(prog *ir.Program, src []byte, syntax string, structuralOnly bool, evidence map[string][]map[string]any) []diag.Diagnostic {
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
				Format: doc.FormatJSONL, StructuralOnly: structuralOnly, Evidence: evidence,
				JSONL: true, Line: i + 1, LineStart: ls,
			})...)
		}
		return out
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}
		}
		return ir.Execute(prog, d.Root, ir.ExecOptions{Format: doc.FormatTOML, StructuralOnly: structuralOnly, Evidence: evidence})
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			return []diag.Diagnostic{parseDiag(perr)}
		}
		return ir.Execute(prog, d.Root, ir.ExecOptions{Format: doc.FormatJSON, StructuralOnly: structuralOnly, Evidence: evidence})
	}
}

// crossDocResolvers collects the evidence-resolver names of every cross-document
// constraint in the schema (Selection for count-/sum-limit; Source for
// set-coverage / cross-collection-unique / named-reference-must-resolve),
// deduplicated in first-seen order.
func crossDocResolvers(s *schema.Schema) []string {
	seen := map[string]bool{}
	var out []string
	add := func(r string) {
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		out = append(out, r)
	}
	var walk func(t *schema.Type)
	walk = func(t *schema.Type) {
		if t == nil {
			return
		}
		for _, c := range t.Constraints {
			switch c.Form {
			case "count-limit", "sum-limit":
				add(c.Selection)
			case "set-coverage", "cross-collection-unique", "named-reference-must-resolve":
				add(c.Source)
			}
		}
		for _, f := range t.Fields {
			walk(f.Type)
		}
		for _, a := range t.Arms {
			walk(a.Type)
		}
		walk(t.Item)
		walk(t.Value)
		walk(t.Inner)
	}
	for _, name := range s.TypeOrder {
		walk(s.Types[name])
	}
	return out
}

// parseCollectionResolver reports whether a resolver name is collection-shaped —
// the closed documents-in(glob) form (appendix-surface-syntax §5.1) — and returns
// its embedded glob. Only collection-shaped resolvers can be hosted in-process by
// --collection; any other resolver form is a hard error at the call site.
func parseCollectionResolver(resolver string) (string, bool) {
	const prefix = "documents-in("
	if strings.HasPrefix(resolver, prefix) && strings.HasSuffix(resolver, ")") {
		return resolver[len(prefix) : len(resolver)-1], true
	}
	return "", false
}

// loadCollectionEvidence resolves the --collection globs (anchored at root,
// lexicographic) and loads each matching document into an evidence record. This
// is the CLI's in-process host for documents-in(...) resolvers; it mirrors the
// evidence set the conformance fixtures pass to the adapter (a resolver name ->
// list of document field maps).
func loadCollectionEvidence(root string, globs []string) ([]map[string]any, error) {
	var out []map[string]any
	seen := map[string]bool{}
	for _, g := range globs {
		files, err := diffeng.ResolveGlob(root, g)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if seen[f] {
				continue
			}
			seen[f] = true
			ed, lerr := loadEvidenceDoc(f)
			if lerr != nil {
				return nil, lerr
			}
			out = append(out, ed)
		}
	}
	return out, nil
}

// loadEvidenceDoc parses one collection document into a flat field map. Only
// top-level scalar fields are lifted — the cross-document forms consume `name`
// (identity) and the summed field, both scalars; nested structure is irrelevant
// to count/sum evidence.
func loadEvidenceDoc(path string) (map[string]any, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	syntax := syntaxOf(path)
	var root doc.Node
	switch syntax {
	case "toml":
		d, perr := tomldoc.Parse(src)
		if perr != nil {
			return nil, fmt.Errorf("collection document %s: %w", path, perr)
		}
		root = d.Root
	case "jsonl":
		ds, perr := jsondoc.ParseLines(src)
		if perr != nil {
			return nil, fmt.Errorf("collection document %s: %w", path, perr)
		}
		if len(ds) == 0 {
			return nil, fmt.Errorf("collection document %s: empty JSONL stream", path)
		}
		root = ds[0].Root
	default:
		d, perr := jsondoc.Parse(src)
		if perr != nil {
			return nil, fmt.Errorf("collection document %s: %w", path, perr)
		}
		root = d.Root
	}
	m := map[string]any{}
	if root == nil || root.Kind() != doc.Record {
		return m, nil
	}
	for _, e := range root.Entries() {
		if v, ok := nativeScalar(e.Value, syntax); ok {
			m[e.Key] = v
		}
	}
	return m, nil
}

// nativeScalar lifts a scalar document node to a Go value matching the evidence
// contract the IR engine consumes (int64 / float64 / string / bool). Non-scalar
// nodes report ok=false and are omitted.
func nativeScalar(n doc.Node, syntax string) (any, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Kind() {
	case doc.String:
		if syntax == "toml" {
			return strdecode.TOML(n.Lexeme()), true
		}
		return strdecode.JSON(n.Lexeme()), true
	case doc.Integer:
		i, err := strconv.ParseInt(n.Lexeme(), 10, 64)
		if err != nil {
			return nil, false
		}
		return i, true
	case doc.Float:
		f, err := strconv.ParseFloat(n.Lexeme(), 64)
		if err != nil {
			return nil, false
		}
		return f, true
	case doc.Bool:
		return n.Lexeme() == "true", true
	default:
		return nil, false
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
		regenCmd := "strictspec gen --manifest " + filepath.Base(manifestPath)
		for _, tgt := range se.Targets {
			want, gerr := emitTarget(loaded, tgt, regenCmd)
			if gerr != nil {
				ctx.Error(fmt.Sprintf("%s: %s", se.Path, gerr.Error()))
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
		// render.Render panics on programmer errors (unknown code/slot, missing
		// required slot) by contract — a Diagnostic is slot-correct by
		// construction, so a panic here is a bug to fix loudly, never masked.
		ctx.Error(fmt.Sprintf("  %s at %s: %s", d.Code, d.Path.Render(), render.Render(d)))
	}
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
	// Only the JSONL parse template carries a {line} slot; the JSON and TOML
	// templates do not, so binding `line` there makes render.Render panic on an
	// unknown slot (render's slot-coverage invariant).
	code := "STRICTSPEC_PARSE_JSON_SYNTAX"
	slots := map[string]diag.Slot{"detail": diag.SlotString{S: pe.Message}}
	switch pe.Format {
	case doc.FormatTOML:
		code = "STRICTSPEC_PARSE_TOML_SYNTAX"
	case doc.FormatJSONL:
		code = "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX"
		slots["line"] = diag.SlotInt{N: int64(pe.Position.Line)}
	}
	return diag.Diagnostic{
		Code:  code,
		Path:  diag.NewPath(),
		Slots: slots,
	}
}
