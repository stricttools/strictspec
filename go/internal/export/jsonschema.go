// Package export implements `strictspec export`: JSON Schema (Draft 2020-12) for
// editor tooling. Per the constitution (spec/DESIGN.md — Export), JSON Schema is
// an ADVISORY export target with a normative LOSSINESS TABLE: the structural core
// (records, maps, arrays, tuples, unions, scalars, enums, literals, nullable,
// custom scalars) maps to JSON Schema; the cross-field and cross-document
// constraint vocabulary, alias both-present, consumer-check declarations, and
// per-line versioning are DROPPED (named here as the lossy set) — editors are
// advisory, strictspec is the enforcement. An unmappable construct is a hard
// error, never a silent omission.
package export

import (
	"encoding/json"
	"fmt"

	"github.com/smm-h/strictspec/go/internal/schema"
)

// DroppedSemantics names what JSON Schema export cannot carry (the lossiness
// table's dropped column). Emitted as a comment-like field so a reviewer sees
// the blind spots.
var DroppedSemantics = []string{
	"cross-field constraint vocabulary (conditional-*, exactly-one-of, unique-by, ...)",
	"cross-document constraint vocabulary (references, coverage, count-limit, sum-limit)",
	"alias both-present rule (aliases export as permissive alternatives are not modeled)",
	"consumer-check / unchecked opaque-leaf declarations",
	"per-line (JSONL) versioning",
}

// ToJSONSchema exports a resolved schema to a JSON Schema document (bytes). The
// schema's imports must already be resolved into s.Types.
func ToJSONSchema(s *schema.Schema, scalars map[string]*schema.Scalar) ([]byte, error) {
	e := &exporter{s: s, scalars: scalars, defs: map[string]any{}}
	root, ok := s.Types[s.Root]
	if !ok {
		return nil, fmt.Errorf("export: root type %q not found", s.Root)
	}
	// Populate $defs for every named type.
	for name, t := range s.Types {
		js, err := e.typeSchema(t)
		if err != nil {
			return nil, err
		}
		e.defs[exportKey(name)] = js
	}
	rootSchema, err := e.typeSchema(root)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       s.Name,
		"description": s.Description,
		"$defs":       e.defs,
		"x-strictspec-format-version": s.FormatVersion,
		"x-strictspec-dropped":        DroppedSemantics,
	}
	// Merge the root type's schema at the top level.
	for k, v := range rootSchema {
		out[k] = v
	}
	return json.MarshalIndent(out, "", "  ")
}

type exporter struct {
	s       *schema.Schema
	scalars map[string]*schema.Scalar
	defs    map[string]any
}

func (e *exporter) typeSchema(t *schema.Type) (map[string]any, error) {
	if t == nil {
		return map[string]any{}, nil
	}
	switch t.Kind {
	case schema.KindRef:
		return e.refSchema(t)
	case schema.KindRecord:
		return e.recordSchema(t)
	case schema.KindMap:
		return e.mapSchema(t)
	case schema.KindArray:
		return e.arraySchema(t)
	case schema.KindTuple:
		return e.tupleSchema(t)
	case schema.KindEnum:
		return e.enumSchema(t)
	case schema.KindLiteral:
		return map[string]any{"const": svalToAny(t.Literal)}, nil
	case schema.KindDiscriminatedUnion, schema.KindNodeKindUnion:
		return e.unionSchema(t)
	case schema.KindNullable:
		return e.nullableSchema(t)
	case schema.KindOpaque:
		return map[string]any{"description": "opaque JSON leaf (strictspec-blind)"}, nil
	default:
		return nil, fmt.Errorf("export: unsupported construct (kind %d)", t.Kind)
	}
}

func (e *exporter) refSchema(t *schema.Type) (map[string]any, error) {
	switch t.Ref {
	case "string":
		m := map[string]any{"type": "string"}
		if t.MinLength != nil {
			m["minLength"] = *t.MinLength
		}
		if t.MaxLength != nil {
			m["maxLength"] = *t.MaxLength
		}
		if t.NonEmpty {
			m["minLength"] = 1
		}
		if t.HasRegex {
			m["pattern"] = t.Regex
		}
		return m, nil
	case "integer":
		return withNumBounds(map[string]any{"type": "integer"}, t), nil
	case "float", "number":
		return withNumBounds(map[string]any{"type": "number"}, t), nil
	case "boolean":
		return map[string]any{"type": "boolean"}, nil
	case "date":
		return map[string]any{"type": "string", "format": "date"}, nil
	case "time":
		return map[string]any{"type": "string", "format": "time"}, nil
	case "datetime":
		return map[string]any{"type": "string", "format": "date-time"}, nil
	default:
		if _, ok := e.s.Types[t.Ref]; ok {
			return map[string]any{"$ref": "#/$defs/" + exportKey(t.Ref)}, nil
		}
		if cs, ok := e.scalars[t.Ref]; ok {
			m := map[string]any{"type": "string"}
			if cs.LexemeRule != "" {
				m["pattern"] = cs.LexemeRule
			}
			if cs.LenMin != nil {
				m["minLength"] = *cs.LenMin
			}
			if cs.LenMax != nil {
				m["maxLength"] = *cs.LenMax
			}
			if cs.NonEmpty {
				m["minLength"] = 1
			}
			return m, nil
		}
		return nil, fmt.Errorf("export: unresolved type reference %q", t.Ref)
	}
}

func (e *exporter) recordSchema(t *schema.Type) (map[string]any, error) {
	props := map[string]any{}
	var required []string
	for _, f := range t.Fields {
		fs, err := e.typeSchema(f.Type)
		if err != nil {
			return nil, err
		}
		props[f.Name] = fs
		if f.Required {
			required = append(required, f.Name)
		}
	}
	m := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false, // unknown keys are always a hard error
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m, nil
}

func (e *exporter) mapSchema(t *schema.Type) (map[string]any, error) {
	m := map[string]any{"type": "object"}
	if t.Value != nil {
		vs, err := e.typeSchema(t.Value)
		if err != nil {
			return nil, err
		}
		m["additionalProperties"] = vs
	}
	if t.KeyPattern != "" {
		m["propertyNames"] = map[string]any{"pattern": t.KeyPattern}
	}
	return m, nil
}

func (e *exporter) arraySchema(t *schema.Type) (map[string]any, error) {
	m := map[string]any{"type": "array"}
	if t.Item != nil {
		is, err := e.typeSchema(t.Item)
		if err != nil {
			return nil, err
		}
		m["items"] = is
	}
	if t.MinLen != nil {
		m["minItems"] = *t.MinLen
	}
	if t.MaxLen != nil {
		m["maxItems"] = *t.MaxLen
	}
	return m, nil
}

func (e *exporter) tupleSchema(t *schema.Type) (map[string]any, error) {
	var prefix []any
	for _, ref := range t.Elements {
		es, err := e.typeSchema(&schema.Type{Kind: schema.KindRef, Ref: ref})
		if err != nil {
			return nil, err
		}
		prefix = append(prefix, es)
	}
	return map[string]any{
		"type":        "array",
		"prefixItems": prefix,
		"items":       false,
		"minItems":    len(prefix),
		"maxItems":    len(prefix),
	}, nil
}

func (e *exporter) enumSchema(t *schema.Type) (map[string]any, error) {
	var vals []any
	if t.Sourced {
		for _, a := range t.Baked {
			vals = append(vals, a)
		}
		return map[string]any{"type": "string", "enum": vals}, nil
	}
	allInt := true
	for _, ev := range t.EnumValues {
		vals = append(vals, svalToAny(ev))
		if ev.Kind.String() != "Integer" {
			allInt = false
		}
	}
	typ := "string"
	if allInt {
		typ = "integer"
	}
	return map[string]any{"type": typ, "enum": vals}, nil
}

func (e *exporter) unionSchema(t *schema.Type) (map[string]any, error) {
	var arms []any
	for _, arm := range t.Arms {
		as, err := e.typeSchema(arm.Type)
		if err != nil {
			return nil, err
		}
		arms = append(arms, as)
	}
	m := map[string]any{"oneOf": arms}
	if t.Discriminator != "" {
		m["x-strictspec-discriminator"] = t.Discriminator
	}
	return m, nil
}

func (e *exporter) nullableSchema(t *schema.Type) (map[string]any, error) {
	inner, err := e.typeSchema(t.Inner)
	if err != nil {
		return nil, err
	}
	return map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}, nil
}

func withNumBounds(m map[string]any, t *schema.Type) map[string]any {
	if t.Min != nil {
		m["minimum"] = svalToAny(*t.Min)
	}
	if t.Max != nil {
		m["maximum"] = svalToAny(*t.Max)
	}
	if t.ExclusiveMin != nil {
		m["exclusiveMinimum"] = svalToAny(*t.ExclusiveMin)
	}
	if t.ExclusiveMax != nil {
		m["exclusiveMaximum"] = svalToAny(*t.ExclusiveMax)
	}
	return m
}

func svalToAny(sv schema.SVal) any {
	switch sv.Kind.String() {
	case "String":
		return sv.Str
	case "Integer":
		return sv.Int
	case "Float":
		return sv.Float
	case "Bool":
		return sv.Bool
	default:
		return sv.Lexeme
	}
}

// exportKey normalizes a type name for use as a $defs key (identity today; a
// hook if reserved characters ever appear).
func exportKey(name string) string { return name }
