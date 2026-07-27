package schema

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/strdecode"
)

// complexKinds maps the `type` spelling of a complex kind to its Kind.
var complexKinds = map[string]Kind{
	"record":              KindRecord,
	"map":                 KindMap,
	"array":               KindArray,
	"tuple":               KindTuple,
	"enum":                KindEnum,
	"literal":             KindLiteral,
	"discriminated-union": KindDiscriminatedUnion,
	"node-kind-union":     KindNodeKindUnion,
	"nullable":            KindNullable,
	"opaque":              KindOpaque,
}

type reader struct {
	diags    diag.Diagnostics
	isType   bool   // role == "type-definitions"
	fileName string // <name>.toml, for IMPORT_* file slots
}

// ReadSchema parses a schema/type-definition document (root record) into a typed
// Schema and returns any authoring diagnostics. dir is the file's directory
// (used later for import resolution). ReadSchema itself does not touch the
// filesystem.
func ReadSchema(root doc.Node, dir string) (*Schema, []diag.Diagnostic) {
	s := &Schema{Types: map[string]*Type{}, Dir: dir}
	r := &reader{}
	if root == nil || root.Kind() != doc.Record {
		return s, r.diags.All()
	}
	r.parseHeader(s, root)
	r.isType = s.Role == "type-definitions"
	r.fileName = s.Name + ".toml"

	// Version presence checks.
	if !s.HasMetaVersion {
		r.diags.EmitCode("STRICTSPEC_SCHEMA_MISSING_META_VERSION",
			diag.NewPath(), map[string]diag.Slot{"schema": diag.SlotIdentifier{Name: s.Name}})
	}
	if s.Role == "schema" && !s.HasFormatVersion {
		r.diags.EmitCode("STRICTSPEC_SCHEMA_MISSING_FORMAT_VERSION",
			diag.NewPath(), map[string]diag.Slot{"schema": diag.SlotIdentifier{Name: s.Name}})
	}

	// Transitive-import rejection: a type-definition file may not import.
	if r.isType && len(s.Imports) > 0 {
		r.diags.EmitCode("STRICTSPEC_IMPORT_TRANSITIVE",
			diag.NewPath(), map[string]diag.Slot{"file": diag.SlotString{S: r.fileName}})
	}

	// Parse named types.
	if typesNode, ok := entryOf(root, "types"); ok && typesNode.Kind() == doc.Record {
		for _, e := range typesNode.Entries() {
			name := e.Key
			sp := diag.NewPath(diag.Key{Name: "types"}, diag.Key{Name: name})
			t := r.parseType(e.Value, sp)
			s.Types[name] = t
			s.TypeOrder = append(s.TypeOrder, name)
			// Cross-file-constraint rejection: a type file may declare types only.
			if r.isType && hasAnyConstraint(t) {
				r.diags.EmitCode("STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
					diag.NewPath(), map[string]diag.Slot{"file": diag.SlotString{S: r.fileName}})
				r.isType = false // emit once
			}
		}
	}
	return s, r.diags.All()
}

func (r *reader) parseHeader(s *Schema, root doc.Node) {
	for _, e := range root.Entries() {
		switch e.Key {
		case "name":
			s.Name = decodeStr(e.Value)
		case "meta_version":
			s.HasMetaVersion = true
			s.MetaVersionKind = e.Value.Kind()
			if v, ok := intOf(e.Value); ok {
				s.MetaVersion = v
			}
		case "format_version":
			s.HasFormatVersion = true
			s.FormatVersionKind = e.Value.Kind()
			if v, ok := intOf(e.Value); ok {
				s.FormatVersion = v
			}
		case "document_syntax":
			s.DocumentSyntax = decodeStr(e.Value)
		case "role":
			s.Role = decodeStr(e.Value)
		case "description":
			s.Description = decodeStr(e.Value)
		case "root":
			s.Root = decodeStr(e.Value)
		case "safe_integers":
			s.SafeIntegers = e.Value.Kind() == doc.Bool && e.Value.Lexeme() == "true"
		case "targets":
			for _, it := range items(e.Value) {
				s.Targets = append(s.Targets, decodeStr(it))
			}
		case "imports":
			for _, it := range items(e.Value) {
				imp := Import{File: decodeStr(child(it, "file"))}
				for _, t := range items(child(it, "types")) {
					imp.Types = append(imp.Types, decodeStr(t))
				}
				s.Imports = append(s.Imports, imp)
			}
		}
	}
}

// parseType parses a type-site record into a *Type, emitting opaque-stance
// authoring diagnostics.
func (r *reader) parseType(node doc.Node, sp diag.Path) *Type {
	t := &Type{SchemaPath: sp}
	if node == nil || node.Kind() != doc.Record {
		return t
	}
	typeName, hasType := strEntry(node, "type")

	if hasType {
		if ck, ok := complexKinds[typeName]; ok {
			t.Kind = ck
		} else {
			t.Kind = KindRef
			t.Ref = typeName
		}
	} else {
		t.Kind = inferKind(node)
	}

	// Refinements apply to scalar sites and scalar-refinement named types.
	r.parseRefinements(t, node)

	switch t.Kind {
	case KindRecord:
		r.parseFields(t, node, sp)
	case KindMap:
		if kp, ok := strEntry(node, "key_pattern"); ok {
			t.KeyPattern = kp
		}
		if o, ok := strEntry(node, "order"); ok {
			t.Order = o
		}
		if v, ok := entryOf(node, "value"); ok {
			t.Value = r.parseType(v, appendKey(sp, "value"))
		}
	case KindArray:
		if v, ok := entryOf(node, "min_len"); ok {
			t.MinLen = intPtr(v)
		}
		if v, ok := entryOf(node, "max_len"); ok {
			t.MaxLen = intPtr(v)
		}
		if it, ok := entryOf(node, "item"); ok {
			t.Item = r.parseType(it, appendKey(sp, "item"))
		}
	case KindTuple:
		for _, el := range items(childOf(node, "elements")) {
			t.Elements = append(t.Elements, decodeStr(el))
		}
	case KindEnum:
		if vs, ok := entryOf(node, "values"); ok {
			for _, v := range items(vs) {
				t.EnumValues = append(t.EnumValues, svalFromNode(v))
			}
		}
		var srcNode doc.Node
		if src, ok := entryOf(node, "source"); ok && src.Kind() == doc.Record {
			t.Sourced = true
			srcNode = src
			t.SourceDoc = strOr(src, "document")
			t.SourceSel = strOr(src, "selector")
		}
		// `baked` may sit at the site level (sibling of `source`) OR, depending on
		// TOML table nesting, inside the `[<site>.source]` table. Accept both.
		if b, ok := entryOf(node, "baked"); ok {
			t.Sourced = true
			for _, v := range items(b) {
				t.Baked = append(t.Baked, decodeStr(v))
			}
		} else if srcNode != nil {
			if b, ok := entryOf(srcNode, "baked"); ok {
				for _, v := range items(b) {
					t.Baked = append(t.Baked, decodeStr(v))
				}
			}
		}
	case KindLiteral:
		if v, ok := entryOf(node, "value"); ok {
			t.Literal = svalFromNode(v)
		}
	case KindDiscriminatedUnion, KindNodeKindUnion:
		if d, ok := strEntry(node, "discriminator"); ok {
			t.Discriminator = d
		}
		if arms, ok := entryOf(node, "arms"); ok && arms.Kind() == doc.Record {
			for _, e := range arms.Entries() {
				armSp := appendKey(appendKey(sp, "arms"), e.Key)
				t.Arms = append(t.Arms, &Arm{Name: e.Key, Type: r.parseType(e.Value, armSp)})
			}
		}
	case KindNullable:
		if in, ok := entryOf(node, "inner"); ok {
			t.Inner = r.parseType(in, appendKey(sp, "inner"))
		}
	case KindOpaque:
		if cc, ok := strEntry(node, "consumer_check"); ok {
			t.ConsumerCheck = cc
			t.HasConsumerCheck = true
		}
		if u, ok := entryOf(node, "unchecked"); ok {
			t.HasUnchecked = true
			t.Unchecked = u.Kind() == doc.Bool && u.Lexeme() == "true"
		}
		if ur, ok := strEntry(node, "unchecked_reason"); ok {
			t.UncheckedReason = ur
			t.HasReason = true
		}
		r.checkOpaqueStance(t)
	}

	r.parseConstraints(t, node)
	return t
}

func (r *reader) checkOpaqueStance(t *Type) {
	if t.HasConsumerCheck {
		return
	}
	if t.HasUnchecked && t.Unchecked {
		if !t.HasReason {
			r.diags.EmitCode("STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON", t.SchemaPath, nil)
		}
		return
	}
	// Neither consumer_check nor a (true) unchecked stance.
	r.diags.EmitCode("STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE", t.SchemaPath, nil)
}

func (r *reader) parseFields(t *Type, node doc.Node, sp diag.Path) {
	fnode, ok := entryOf(node, "fields")
	if !ok || fnode.Kind() != doc.Record {
		return
	}
	for _, e := range fnode.Entries() {
		fsp := appendKey(appendKey(sp, "fields"), e.Key)
		ft := r.parseType(e.Value, fsp)
		f := &Field{Name: e.Key, Type: ft}
		if req, ok := entryOf(e.Value, "required"); ok {
			f.Required = req.Kind() == doc.Bool && req.Lexeme() == "true"
		}
		for _, a := range items(childOf(e.Value, "aliases")) {
			f.Aliases = append(f.Aliases, decodeStr(a))
		}
		t.Fields = append(t.Fields, f)
	}
}

func (r *reader) parseRefinements(t *Type, node doc.Node) {
	if v, ok := entryOf(node, "min"); ok {
		s := svalFromNode(v)
		t.Min = &s
	}
	if v, ok := entryOf(node, "max"); ok {
		s := svalFromNode(v)
		t.Max = &s
	}
	if v, ok := entryOf(node, "exclusive_min"); ok {
		s := svalFromNode(v)
		t.ExclusiveMin = &s
	}
	if v, ok := entryOf(node, "exclusive_max"); ok {
		s := svalFromNode(v)
		t.ExclusiveMax = &s
	}
	if v, ok := entryOf(node, "min_length"); ok {
		t.MinLength = intPtr(v)
	}
	if v, ok := entryOf(node, "max_length"); ok {
		t.MaxLength = intPtr(v)
	}
	if v, ok := entryOf(node, "non_empty"); ok {
		t.NonEmpty = v.Kind() == doc.Bool && v.Lexeme() == "true"
	}
	if v, ok := strEntry(node, "regex"); ok {
		t.Regex = v
		t.HasRegex = true
	}
	if v, ok := strEntry(node, "datetime_kind"); ok {
		t.DatetimeKind = v
	}
}

func (r *reader) parseConstraints(t *Type, node doc.Node) {
	cnode, ok := entryOf(node, "constraints")
	if !ok {
		return
	}
	for _, c := range items(cnode) {
		if c.Kind() != doc.Record {
			continue
		}
		con := &Constraint{Form: strOr(c, "form")}
		con.Field = strOr(c, "field")
		con.Left = strOr(c, "left")
		con.Right = strOr(c, "right")
		con.Collection = strOr(c, "collection")
		con.UniqField = strOr(c, "field")
		con.Normalization = strOr(c, "normalization")
		con.Start = strOr(c, "start")
		con.Length = strOr(c, "length")
		con.Less = strOr(c, "less")
		con.Than = strOr(c, "than")
		con.Reference = strOr(c, "reference")
		con.ResolvesInto = strOr(c, "resolves_into")
		con.ResolvesBy = strOr(c, "resolves_by")
		con.Source = strOr(c, "source")
		con.Selection = strOr(c, "selection")
		con.Compare = strOr(c, "compare")
		con.SumField = strOr(c, "sum_field")
		if lim, ok := entryOf(c, "limit"); ok {
			con.Limit = svalFromNode(lim)
			con.HasLimit = true
		}
		if el, ok := entryOf(c, "equals_literal"); ok {
			con.EqualsLiteral = svalFromNode(el)
			con.HasEquals = true
		}
		for _, f := range items(childOf(c, "fields")) {
			con.Fields = append(con.Fields, decodeStr(f))
		}
		if w, ok := entryOf(c, "when"); ok && w.Kind() == doc.Record {
			con.When = parseCondition(w)
		}
		t.Constraints = append(t.Constraints, con)
	}
}

func parseCondition(w doc.Node) *Condition {
	c := &Condition{Field: strOr(w, "field"), Predicate: strOr(w, "predicate")}
	if v, ok := entryOf(w, "value"); ok {
		c.Value = svalFromNode(v)
		c.HasValue = true
	}
	for _, v := range items(childOf(w, "values")) {
		c.Values = append(c.Values, svalFromNode(v))
	}
	return c
}

// inferKind derives a kind from structure when `type` is omitted (e.g. an inline
// union-arm record declared only via `.fields.*`).
func inferKind(node doc.Node) Kind {
	if _, ok := entryOf(node, "fields"); ok {
		return KindRecord
	}
	if _, ok := entryOf(node, "arms"); ok {
		return KindDiscriminatedUnion
	}
	if _, ok := entryOf(node, "item"); ok {
		return KindArray
	}
	if _, ok := entryOf(node, "value"); ok {
		return KindMap
	}
	if _, ok := entryOf(node, "inner"); ok {
		return KindNullable
	}
	if _, ok := entryOf(node, "elements"); ok {
		return KindTuple
	}
	return KindRecord
}

func hasAnyConstraint(t *Type) bool {
	if t == nil {
		return false
	}
	if len(t.Constraints) > 0 {
		return true
	}
	for _, f := range t.Fields {
		if hasAnyConstraint(f.Type) {
			return true
		}
	}
	for _, a := range t.Arms {
		if hasAnyConstraint(a.Type) {
			return true
		}
	}
	if hasAnyConstraint(t.Item) || hasAnyConstraint(t.Value) || hasAnyConstraint(t.Inner) {
		return true
	}
	return false
}

// --- small node accessors ---------------------------------------------------

func entryOf(rec doc.Node, key string) (doc.Node, bool) {
	if rec == nil || rec.Kind() != doc.Record {
		return nil, false
	}
	for _, e := range rec.Entries() {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func child(rec doc.Node, key string) doc.Node {
	n, _ := entryOf(rec, key)
	return n
}

func childOf(rec doc.Node, key string) doc.Node { return child(rec, key) }

func strEntry(rec doc.Node, key string) (string, bool) {
	n, ok := entryOf(rec, key)
	if !ok || n.Kind() != doc.String {
		return "", false
	}
	return strdecode.TOML(n.Lexeme()), true
}

func strOr(rec doc.Node, key string) string {
	s, _ := strEntry(rec, key)
	return s
}

func decodeStr(n doc.Node) string {
	if n == nil || n.Kind() != doc.String {
		return ""
	}
	return strdecode.TOML(n.Lexeme())
}

func intOf(n doc.Node) (int64, bool) {
	if n == nil || n.Kind() != doc.Integer {
		return 0, false
	}
	s := svalFromNode(n)
	return s.Int, s.IsInt
}

func intPtr(n doc.Node) *int {
	if v, ok := intOf(n); ok {
		i := int(v)
		return &i
	}
	return nil
}

func items(n doc.Node) []doc.Node {
	if n == nil || n.Kind() != doc.Array {
		return nil
	}
	return n.Items()
}

func appendKey(p diag.Path, key string) diag.Path {
	steps := make([]diag.Step, len(p.Steps)+1)
	copy(steps, p.Steps)
	steps[len(p.Steps)] = diag.Key{Name: key}
	return diag.Path{Steps: steps, Anchor: p.Anchor}
}
