package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// TSParams configures one TypeScript emission.
type TSParams struct {
	MainFile         string            // key of the schema entry point within Files
	Files            map[string]string // embedded file set (schema + imports + scalars)
	GeneratorVersion string            // strictspec release doing the generation
	RegenCommand     string            // the exact command to regenerate (header + gate remediation)
}

// GeneratedFileNameTS returns the conventional output file name for a schema.
func GeneratedFileNameTS(s *schema.Schema) string {
	return sanitizeLower(s.Name) + ".generated.ts"
}

// GenerateTypeScript emits the generated TS validator source for schema s. s must
// be fully resolved. Output is ESM, byte-deterministic (sorted file keys,
// declaration-order walk), and formatted by this canonical emitter (no external
// formatter). The safe_integers-for-TS rule is enforced by the gen orchestration
// (a manifest TS target on a schema lacking the declaration is a hard error), not
// here — this function only emits.
func GenerateTypeScript(s *schema.Schema, p TSParams) (string, error) {
	g := &tsEmitter{s: s, p: p}
	return g.build(), nil
}

type tsEmitter struct {
	s *schema.Schema
	p TSParams
	b strings.Builder
}

func (g *tsEmitter) build() string {
	g.header()
	g.embeddedFiles()
	g.programInit()
	g.entryPoints()
	g.types()
	return g.b.String()
}

func (g *tsEmitter) header() {
	w := &g.b
	fmt.Fprintf(w, "// strictspec generated validator. DO NOT EDIT.\n")
	fmt.Fprintf(w, "//\n")
	fmt.Fprintf(w, "// strictspec generator: %s\n", g.p.GeneratorVersion)
	fmt.Fprintf(w, "// schema:              %s (format_version %d)\n", g.s.Name, g.s.FormatVersion)
	fmt.Fprintf(w, "// regenerate:          %s\n", g.p.RegenCommand)
	fmt.Fprintf(w, "//\n")
	fmt.Fprintf(w, "// Released under the MIT license (unencumbered). This file is machine-generated;\n")
	fmt.Fprintf(w, "// edit the schema and regenerate, never this file.\n")
	// Lint-suppression + formatter-ignore markers, pinned by the generator so
	// consumers never hand-silence linters on generated paths (ts/DESIGN.md).
	fmt.Fprintf(w, "/* eslint-disable */\n")
	fmt.Fprintf(w, "// biome-ignore-all lint: generated file\n")
	fmt.Fprintf(w, "// prettier-ignore\n")
	fmt.Fprintf(w, "import { Kind, compileFromSource, loadValue, requireRuntimeVersion } from \"strictspec\";\n")
	fmt.Fprintf(w, "import type { Diagnostic, Program, Value } from \"strictspec\";\n\n")
	// Extract the runtime's exact types without depending on non-exported names.
	fmt.Fprintf(w, "type FileSet = Record<string, string>;\n")
	fmt.Fprintf(w, "type Syntax = \"json\" | \"toml\" | \"jsonl\";\n")
	fmt.Fprintf(w, "type Evidence = Parameters<Program[\"validateWithEvidence\"]>[2];\n\n")
}

func (g *tsEmitter) embeddedFiles() {
	w := &g.b
	fmt.Fprintf(w, "// GENERATED_BY is the strictspec release that produced this file. The runtime\n")
	fmt.Fprintf(w, "// pairing guard hard-errors unless it matches the linked runtime exactly.\n")
	fmt.Fprintf(w, "export const GENERATED_BY = \"%s\";\n", escapeStringLiteral(g.p.GeneratorVersion))
	fmt.Fprintf(w, "export const SCHEMA_FORMAT_VERSION = %d;\n\n", g.s.FormatVersion)

	names := make([]string, 0, len(g.p.Files))
	for k := range g.p.Files {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "// EMBEDDED_SCHEMA carries the compiled schema (and its imported type-definition\n")
	fmt.Fprintf(w, "// files and scalar manifest) so the validator is self-contained and does no IO.\n")
	fmt.Fprintf(w, "const EMBEDDED_SCHEMA: FileSet = {\n")
	for _, name := range names {
		fmt.Fprintf(w, "\t\"%s\": \"%s\",\n", escapeStringLiteral(name), escapeStringLiteral(g.p.Files[name]))
	}
	fmt.Fprintf(w, "};\n")
	fmt.Fprintf(w, "const EMBEDDED_MAIN_FILE = \"%s\";\n\n", escapeStringLiteral(g.p.MainFile))
}

func (g *tsEmitter) programInit() {
	w := &g.b
	fmt.Fprintf(w, "// Version pairing: generated code and runtime must be the same release. This runs\n")
	fmt.Fprintf(w, "// at module init, so a skewed runtime throws before any validation is attempted.\n")
	fmt.Fprintf(w, "requireRuntimeVersion(GENERATED_BY);\n")
	fmt.Fprintf(w, "const program: Program = compileFromSource(EMBEDDED_SCHEMA, EMBEDDED_MAIN_FILE);\n\n")
}

func (g *tsEmitter) entryPoints() {
	w := &g.b
	rootIsRecord := isRecordType(g.s, g.s.Root)
	ret := "Value"
	bindRoot := "v"
	if rootIsRecord {
		ret = exportName(g.s.Root)
		bindRoot = "bind" + exportName(g.s.Root) + "(v)"
	}

	fmt.Fprintf(w, "// validateBytes is the RAW-TEXT entry point: lossless parse of rawText in the\n")
	fmt.Fprintf(w, "// given syntax, then validate. Returns the typed root value (null when any\n")
	fmt.Fprintf(w, "// diagnostic fired) and the ordered diagnostics.\n")
	fmt.Fprintf(w, "export function validateBytes(rawText: string, syntax: Syntax): [%s | null, readonly Diagnostic[]] {\n", ret)
	fmt.Fprintf(w, "\treturn validateBytesWithEvidence(rawText, syntax, null);\n")
	fmt.Fprintf(w, "}\n\n")

	fmt.Fprintf(w, "// validateBytesWithEvidence is validateBytes plus cross-document resolver\n")
	fmt.Fprintf(w, "// evidence for the phase-2 constraint vocabulary.\n")
	fmt.Fprintf(w, "export function validateBytesWithEvidence(rawText: string, syntax: Syntax, evidence: Evidence): [%s | null, readonly Diagnostic[]] {\n", ret)
	fmt.Fprintf(w, "\tconst result = program.validateWithEvidence(rawText, syntax, evidence);\n")
	fmt.Fprintf(w, "\tif (!result.valid) {\n\t\treturn [null, result.diagnostics];\n\t}\n")
	fmt.Fprintf(w, "\tconst v = loadValue(rawText, syntax);\n")
	fmt.Fprintf(w, "\treturn [%s, result.diagnostics];\n", bindRoot)
	fmt.Fprintf(w, "}\n\n")

	fmt.Fprintf(w, "// validateValue is the TAGGED-VALUE entry point: validate an already-parsed\n")
	fmt.Fprintf(w, "// tagged document value (from loadValue or a typed constructor).\n")
	fmt.Fprintf(w, "export function validateValue(v: Value): [%s | null, readonly Diagnostic[]] {\n", ret)
	fmt.Fprintf(w, "\tconst result = program.validateValue(v);\n")
	fmt.Fprintf(w, "\tif (!result.valid) {\n\t\treturn [null, result.diagnostics];\n\t}\n")
	fmt.Fprintf(w, "\treturn [%s, result.diagnostics];\n", bindRoot)
	fmt.Fprintf(w, "}\n\n")
}

// types emits a readonly interface + binder for every named record type, in the
// shared deterministic order.
func (g *tsEmitter) types() {
	order := namedTypeOrder(g.s)
	for _, name := range order {
		t := g.s.Types[name]
		if t == nil || t.Kind != schema.KindRecord {
			continue
		}
		g.recordType(name, t)
	}
}

func (g *tsEmitter) recordType(name string, t *schema.Type) {
	w := &g.b
	iface := exportName(name)
	fmt.Fprintf(w, "// %s is the readonly typed binding of the %q record.\n", iface, name)
	fmt.Fprintf(w, "export interface %s {\n", iface)
	for _, f := range t.Fields {
		// Optional (required=false) fields bind to an ABSENT property when the
		// document omits them (the binder sets out[key] only when present), so the
		// interface marks them optional. Under exactOptionalPropertyTypes (the
		// strict tsconfig) `key?: T` means "may be absent, else exactly T" — which
		// is precisely what the binder produces (never present-as-undefined), so
		// `?` is correct and `| undefined` would be wrong. This is TS's uniform
		// convention for every optional field (scalar and record alike): unlike
		// Python/Go there is no zero-value written for absent scalars, so all
		// optional fields need the marker, not just record refs.
		opt := ""
		if !f.Required {
			opt = "?"
		}
		fmt.Fprintf(w, "\treadonly %s%s: %s;\n", g.tsKey(f.Name), opt, g.tsType(f.Type))
	}
	fmt.Fprintf(w, "}\n\n")

	// Binder. `out` is built incrementally, so it is typed loosely and cast at the
	// end (the interface is readonly and cannot be assembled field by field).
	fmt.Fprintf(w, "export function bind%s(v: Value): %s {\n", iface, iface)
	fmt.Fprintf(w, "\tconst out: Record<string, unknown> = {};\n")
	fmt.Fprintf(w, "\tif (v.kind() !== Kind.Record) {\n\t\treturn out as unknown as %s;\n\t}\n", iface)
	for _, f := range t.Fields {
		local := "f_" + identSafe(f.Name)
		present := g.tsBindExpr(f.Type, local)
		fmt.Fprintf(w, "\tconst [%s, %sOk] = v.field(\"%s\");\n", local, local, escapeStringLiteral(f.Name))
		fmt.Fprintf(w, "\tif (%sOk) {\n\t\tout[\"%s\"] = %s;\n\t}\n", local, escapeStringLiteral(f.Name), present)
	}
	fmt.Fprintf(w, "\treturn out as unknown as %s;\n", iface)
	fmt.Fprintf(w, "}\n\n")

	// with* copy helpers (per-field copy-on-write).
	for _, f := range t.Fields {
		fn := exportName(f.Name)
		fmt.Fprintf(w, "export function with%s%s(x: %s, v: %s): %s {\n", iface, fn, iface, g.tsType(f.Type), iface)
		fmt.Fprintf(w, "\treturn { ...x, [\"%s\"]: v };\n", escapeStringLiteral(f.Name))
		fmt.Fprintf(w, "}\n\n")
	}
}

func (g *tsEmitter) tsKey(name string) string {
	if isJSIdent(name) {
		return name
	}
	return "\"" + escapeStringLiteral(name) + "\""
}

// tsType maps a schema type site to its generated TS type.
func (g *tsEmitter) tsType(t *schema.Type) string {
	if t == nil {
		return "Value"
	}
	switch t.Kind {
	case schema.KindRef:
		switch t.Ref {
		case "string":
			return "string"
		case "integer", "float", "number":
			// safe_integers is mandatory for a TS target, so integers bind plain
			// number with no BigInt (ts/DESIGN.md — Numbers in TS).
			return "number"
		case "boolean":
			return "boolean"
		case "date", "time", "datetime":
			return "string"
		default:
			if named, ok := g.s.Types[t.Ref]; ok {
				if named.Kind == schema.KindRecord {
					return exportName(t.Ref)
				}
				return g.tsType(named)
			}
			return "string" // custom scalar or unresolved leaf
		}
	case schema.KindArray:
		return "readonly " + g.tsType(t.Item) + "[]"
	case schema.KindEnum:
		if isIntEnum(t) {
			return "number"
		}
		return "string"
	case schema.KindLiteral:
		return tsTypeOfKind(t.Literal.Kind)
	case schema.KindNullable:
		return g.tsType(t.Inner) + " | null"
	default:
		return "Value"
	}
}

// tsBindExpr returns a TS expression binding expr (a Value) to the type tsType(t)
// produces.
func (g *tsEmitter) tsBindExpr(t *schema.Type, expr string) string {
	if t == nil {
		return expr
	}
	switch t.Kind {
	case schema.KindRef:
		switch t.Ref {
		case "string":
			return expr + ".string()[0]"
		case "integer":
			return expr + ".int()[0]"
		case "float":
			return expr + ".float()[0]"
		case "number":
			return expr + ".number()[0]"
		case "boolean":
			return expr + ".bool()[0]"
		case "date", "time", "datetime":
			return expr + ".datetime()[0]"
		default:
			if named, ok := g.s.Types[t.Ref]; ok {
				if named.Kind == schema.KindRecord {
					return fmt.Sprintf("bind%s(%s)", exportName(t.Ref), expr)
				}
				return g.tsBindExpr(named, expr)
			}
			return expr + ".string()[0]"
		}
	case schema.KindArray:
		return fmt.Sprintf("%s.items().map((e) => %s)", expr, g.tsBindExpr(t.Item, "e"))
	case schema.KindEnum:
		if isIntEnum(t) {
			return expr + ".int()[0]"
		}
		return expr + ".string()[0]"
	case schema.KindLiteral:
		return expr + tsCoercerOfKind(t.Literal.Kind)
	case schema.KindNullable:
		return fmt.Sprintf("(%s.isNull() ? null : %s)", expr, g.tsBindExpr(t.Inner, expr))
	default:
		return expr
	}
}

func tsTypeOfKind(k doc.Kind) string {
	switch k {
	case doc.Integer, doc.Float:
		return "number"
	case doc.Bool:
		return "boolean"
	default:
		return "string"
	}
}

func tsCoercerOfKind(k doc.Kind) string {
	switch k {
	case doc.Integer:
		return ".int()[0]"
	case doc.Float:
		return ".float()[0]"
	case doc.Bool:
		return ".bool()[0]"
	default:
		return ".string()[0]"
	}
}
