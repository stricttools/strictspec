package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// PyParams configures one Python emission.
type PyParams struct {
	MainFile         string            // key of the schema entry point within Files
	Files            map[string]string // embedded file set (schema + imports + scalars)
	GeneratorVersion string            // strictspec release doing the generation
	RegenCommand     string            // the exact command to regenerate (header + gate remediation)
}

// GeneratedFileNamePython returns the conventional output file name for a schema.
func GeneratedFileNamePython(s *schema.Schema) string {
	return sanitizeLower(s.Name) + "_generated.py"
}

// GeneratePython emits the generated Python validator source for schema s. s must
// be fully resolved (imports merged into s.Types). The emitter is strictspec's
// own canonical formatter: no external formatter is ever involved, and the output
// is byte-identical across regenerations (file keys sorted, declaration-order
// walk), so the `check` drift gate is exact.
func GeneratePython(s *schema.Schema, p PyParams) (string, error) {
	g := &pyEmitter{s: s, p: p}
	return g.build(), nil
}

type pyEmitter struct {
	s *schema.Schema
	p PyParams
	b strings.Builder
}

func (g *pyEmitter) build() string {
	g.header()
	g.embeddedFiles()
	g.programInit()
	g.entryPoints()
	g.types()
	return g.b.String()
}

func (g *pyEmitter) header() {
	w := &g.b
	fmt.Fprintf(w, "# strictspec generated validator. DO NOT EDIT.\n")
	fmt.Fprintf(w, "#\n")
	fmt.Fprintf(w, "# strictspec generator: %s\n", g.p.GeneratorVersion)
	fmt.Fprintf(w, "# schema:              %s (format_version %d)\n", g.s.Name, g.s.FormatVersion)
	fmt.Fprintf(w, "# regenerate:          %s\n", g.p.RegenCommand)
	fmt.Fprintf(w, "#\n")
	fmt.Fprintf(w, "# Released under the MIT license (unencumbered). This file is machine-generated;\n")
	fmt.Fprintf(w, "# edit the schema and regenerate, never this file.\n")
	// The lint-suppression marker (ruff) is pinned by the generator; consumers
	// never hand-silence linters on generated paths.
	fmt.Fprintf(w, "# ruff: noqa\n")
	fmt.Fprintf(w, "from __future__ import annotations\n\n")
	fmt.Fprintf(w, "from dataclasses import dataclass, replace\n\n")
	fmt.Fprintf(w, "import strictspec\n")
	fmt.Fprintf(w, "from strictspec import Diagnostic, Value\n\n")
}

func (g *pyEmitter) embeddedFiles() {
	w := &g.b
	fmt.Fprintf(w, "# GENERATED_BY is the strictspec release that produced this file. The runtime\n")
	fmt.Fprintf(w, "# pairing guard hard-errors unless it matches the linked runtime exactly.\n")
	fmt.Fprintf(w, "GENERATED_BY = %q\n", g.p.GeneratorVersion)
	fmt.Fprintf(w, "SCHEMA_FORMAT_VERSION = %d\n\n", g.s.FormatVersion)

	names := make([]string, 0, len(g.p.Files))
	for k := range g.p.Files {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "# _EMBEDDED_SCHEMA carries the compiled schema (and its imported type-definition\n")
	fmt.Fprintf(w, "# files and scalar manifest) so the validator is self-contained and does no IO.\n")
	fmt.Fprintf(w, "_EMBEDDED_SCHEMA = {\n")
	for _, name := range names {
		fmt.Fprintf(w, "    \"%s\": \"%s\",\n", escapeStringLiteral(name), escapeStringLiteral(g.p.Files[name]))
	}
	fmt.Fprintf(w, "}\n")
	fmt.Fprintf(w, "_EMBEDDED_MAIN_FILE = \"%s\"\n\n", escapeStringLiteral(g.p.MainFile))
}

func (g *pyEmitter) programInit() {
	w := &g.b
	fmt.Fprintf(w, "# Version pairing: generated code and runtime must be the same release. This runs\n")
	fmt.Fprintf(w, "# at import, so a skewed runtime hard-errors before any validation is attempted.\n")
	fmt.Fprintf(w, "strictspec.require_runtime_version(GENERATED_BY)\n")
	fmt.Fprintf(w, "_program = strictspec.compile_embedded(_EMBEDDED_SCHEMA, _EMBEDDED_MAIN_FILE)\n\n\n")
}

func (g *pyEmitter) entryPoints() {
	w := &g.b
	rootIsRecord := isRecordType(g.s, g.s.Root)
	ret := "Value"
	bindRoot := "v"
	if rootIsRecord {
		ret = exportName(g.s.Root)
		bindRoot = "_bind_" + exportName(g.s.Root) + "(v)"
	}

	fmt.Fprintf(w, "def validate_bytes(input: bytes, syntax: str) -> tuple[%s | None, tuple[Diagnostic, ...]]:\n", ret)
	fmt.Fprintf(w, "    \"\"\"RAW-BYTES entry point: lossless parse of input in the given syntax\n")
	fmt.Fprintf(w, "    (\"json\" | \"toml\" | \"jsonl\"), then validate. Returns the typed root value\n")
	fmt.Fprintf(w, "    (None when any diagnostic fired) and the ordered diagnostics.\n")
	fmt.Fprintf(w, "    \"\"\"\n")
	fmt.Fprintf(w, "    return validate_bytes_with_evidence(input, syntax, None)\n\n\n")

	fmt.Fprintf(w, "def validate_bytes_with_evidence(input: bytes, syntax: str, evidence: dict | None) -> tuple[%s | None, tuple[Diagnostic, ...]]:\n", ret)
	fmt.Fprintf(w, "    \"\"\"validate_bytes plus cross-document resolver evidence for the phase-2\n")
	fmt.Fprintf(w, "    constraint vocabulary.\n")
	fmt.Fprintf(w, "    \"\"\"\n")
	fmt.Fprintf(w, "    result = _program.validate_with_evidence(input, syntax, evidence)\n")
	fmt.Fprintf(w, "    if not result.valid:\n")
	fmt.Fprintf(w, "        return None, result.diagnostics\n")
	fmt.Fprintf(w, "    v = strictspec.load_value(input, syntax)\n")
	fmt.Fprintf(w, "    return %s, result.diagnostics\n\n\n", bindRoot)

	fmt.Fprintf(w, "def validate_value(v: Value) -> tuple[%s | None, tuple[Diagnostic, ...]]:\n", ret)
	fmt.Fprintf(w, "    \"\"\"TAGGED-VALUE entry point: validate an already-parsed tagged document value\n")
	fmt.Fprintf(w, "    (from strictspec.load_value or a typed constructor). Raw untagged dicts are\n")
	fmt.Fprintf(w, "    never accepted.\n")
	fmt.Fprintf(w, "    \"\"\"\n")
	fmt.Fprintf(w, "    result = _program.validate_value(v)\n")
	fmt.Fprintf(w, "    if not result.valid:\n")
	fmt.Fprintf(w, "        return None, result.diagnostics\n")
	fmt.Fprintf(w, "    return %s, result.diagnostics\n\n\n", bindRoot)
}

// types emits a frozen dataclass + binder + with_* helpers for every named record
// type, in the shared deterministic order.
func (g *pyEmitter) types() {
	order := namedTypeOrder(g.s)
	for _, name := range order {
		t := g.s.Types[name]
		if t == nil || t.Kind != schema.KindRecord {
			continue
		}
		g.recordType(name, t)
	}
}

func (g *pyEmitter) recordType(name string, t *schema.Type) {
	w := &g.b
	cls := exportName(name)
	// kw_only so an optional field's `= None` default never constrains
	// required-field ordering (a required field may follow an optional one). The
	// binder and with_* both construct by keyword, so this is transparent.
	fmt.Fprintf(w, "@dataclass(frozen=True, kw_only=True)\n")
	fmt.Fprintf(w, "class %s:\n", cls)
	fmt.Fprintf(w, "    \"\"\"Frozen typed binding of the %q record. Immutable; use with_* for\n", name)
	fmt.Fprintf(w, "    copy-on-write.\n")
	fmt.Fprintf(w, "    \"\"\"\n\n")
	if len(t.Fields) == 0 {
		fmt.Fprintf(w, "    pass\n\n")
	}
	for _, f := range t.Fields {
		if g.isOptionalRecord(f) {
			// Absent binds None (pyZero), so the type admits None and the field
			// carries a None default for ergonomic hand-construction.
			fmt.Fprintf(w, "    %s: %s = None\n", identSafe(f.Name), g.pyFieldType(f))
		} else {
			fmt.Fprintf(w, "    %s: %s\n", identSafe(f.Name), g.pyFieldType(f))
		}
	}
	if len(t.Fields) > 0 {
		fmt.Fprintf(w, "\n")
	}
	// with_* helpers (per-field copy-on-write).
	for _, f := range t.Fields {
		fn := identSafe(f.Name)
		fmt.Fprintf(w, "    def with_%s(self, v: %s) -> %s:\n", fn, g.pyFieldType(f), cls)
		fmt.Fprintf(w, "        return replace(self, %s=v)\n\n", fn)
	}
	fmt.Fprintf(w, "\n")

	// Binder.
	fmt.Fprintf(w, "def _bind_%s(v: Value) -> %s | None:\n", cls, cls)
	fmt.Fprintf(w, "    if v.kind() != strictspec.Kind.RECORD:\n")
	fmt.Fprintf(w, "        return None\n")
	for _, f := range t.Fields {
		fmt.Fprintf(w, "    f_%s = v.field(\"%s\")\n", identSafe(f.Name), escapeStringLiteral(f.Name))
	}
	fmt.Fprintf(w, "    return %s(\n", cls)
	for _, f := range t.Fields {
		local := "f_" + identSafe(f.Name)
		present := g.pyBindExpr(f.Type, local+"[0]")
		fmt.Fprintf(w, "        %s=(%s if %s[1] else %s),\n",
			identSafe(f.Name), present, local, g.pyZero(f.Type))
	}
	fmt.Fprintf(w, "    )\n\n\n")
}

// pyType maps a schema type site to its generated Python type annotation. Because
// the module uses `from __future__ import annotations`, every annotation is a
// string at runtime, so forward references to later-declared records need no
// quoting.
func (g *pyEmitter) pyType(t *schema.Type) string {
	if t == nil {
		return "Value"
	}
	switch t.Kind {
	case schema.KindRef:
		switch t.Ref {
		case "string":
			return "str"
		case "integer":
			return "int"
		case "float", "number":
			return "float"
		case "boolean":
			return "bool"
		case "date", "time", "datetime":
			// Bound to the retained lexeme string (appendix item 11: no
			// normalization on read).
			return "str"
		default:
			if named, ok := g.s.Types[t.Ref]; ok {
				if named.Kind == schema.KindRecord {
					return exportName(t.Ref)
				}
				return g.pyType(named)
			}
			return "str" // custom scalar (base string) or unresolved leaf
		}
	case schema.KindArray:
		return "list[" + g.pyType(t.Item) + "]"
	case schema.KindEnum:
		if isIntEnum(t) {
			return "int"
		}
		return "str"
	case schema.KindLiteral:
		return pyTypeOfKind(t.Literal.Kind)
	case schema.KindNullable:
		return g.pyType(t.Inner) + " | None"
	default:
		// record (inline), map, tuple, union, opaque.
		return "Value"
	}
}

// pyFieldType is the annotation for a record field SITE: pyType(f.Type), widened
// to `| None` when the field is an optional record ref. That is the sole site
// where the absent-zero (pyZero) is None while pyType is not already nullable —
// so the bare annotation over-promises presence. Optional SCALAR fields keep the
// zero-value convention (absent binds "", 0, False, ...), matching Go's zeroed
// struct field, and are NOT widened. KindNullable sites are already `| None`.
func (g *pyEmitter) pyFieldType(f *schema.Field) string {
	ft := g.pyType(f.Type)
	if g.isOptionalRecord(f) {
		ft += " | None"
	}
	return ft
}

// isOptionalRecord reports whether f is a required=false field whose type binds
// None when absent because it resolves to a named record (directly or through a
// type-alias chain). This is exactly the over-promise the pointer-typed Go
// emitter avoids by making every record ref a pointer.
func (g *pyEmitter) isOptionalRecord(f *schema.Field) bool {
	return !f.Required && g.bindsNilRecord(f.Type)
}

// bindsNilRecord reports whether pyType(t) is a bare record class (so pyZero(t)
// is None). Mirrors pyType's record-detection, following ref-alias chains.
func (g *pyEmitter) bindsNilRecord(t *schema.Type) bool {
	if t == nil || t.Kind != schema.KindRef {
		return false
	}
	switch t.Ref {
	case "string", "integer", "float", "number", "boolean", "date", "time", "datetime":
		return false
	default:
		if named, ok := g.s.Types[t.Ref]; ok {
			if named.Kind == schema.KindRecord {
				return true
			}
			return g.bindsNilRecord(named)
		}
		return false
	}
}

// pyBindExpr returns a Python expression binding expr (a Value) to the type
// pyType(t) produces.
func (g *pyEmitter) pyBindExpr(t *schema.Type, expr string) string {
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
					return fmt.Sprintf("_bind_%s(%s)", exportName(t.Ref), expr)
				}
				return g.pyBindExpr(named, expr)
			}
			return expr + ".string()[0]" // custom scalar
		}
	case schema.KindArray:
		return fmt.Sprintf("[%s for e in %s.items()]", g.pyBindExpr(t.Item, "e"), expr)
	case schema.KindEnum:
		if isIntEnum(t) {
			return expr + ".int()[0]"
		}
		return expr + ".string()[0]"
	case schema.KindLiteral:
		return expr + pyCoercerOfKind(t.Literal.Kind)
	case schema.KindNullable:
		return fmt.Sprintf("(None if %s.is_null() else %s)", expr, g.pyBindExpr(t.Inner, expr))
	default:
		return expr // record/map/tuple/union/opaque: hold the tagged value verbatim
	}
}

// pyZero returns the zero-value expression for a field the binder found absent
// (mirrors the Go binder leaving a zero struct field).
func (g *pyEmitter) pyZero(t *schema.Type) string {
	if t == nil {
		return "Value(None, \"json\")"
	}
	switch t.Kind {
	case schema.KindRef:
		switch t.Ref {
		case "integer":
			return "0"
		case "float", "number":
			return "0.0"
		case "boolean":
			return "False"
		case "string", "date", "time", "datetime":
			return "\"\""
		default:
			if named, ok := g.s.Types[t.Ref]; ok {
				if named.Kind == schema.KindRecord {
					return "None"
				}
				return g.pyZero(named)
			}
			return "\"\""
		}
	case schema.KindArray:
		return "[]"
	case schema.KindEnum:
		if isIntEnum(t) {
			return "0"
		}
		return "\"\""
	case schema.KindLiteral:
		return pyZeroOfKind(t.Literal.Kind)
	case schema.KindNullable:
		return "None"
	default:
		return "Value(None, \"json\")"
	}
}

func pyTypeOfKind(k doc.Kind) string {
	switch k {
	case doc.Integer:
		return "int"
	case doc.Float:
		return "float"
	case doc.Bool:
		return "bool"
	default:
		return "str"
	}
}

func pyCoercerOfKind(k doc.Kind) string {
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

func pyZeroOfKind(k doc.Kind) string {
	switch k {
	case doc.Integer:
		return "0"
	case doc.Float:
		return "0.0"
	case doc.Bool:
		return "False"
	default:
		return "\"\""
	}
}
