package ir

import (
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// runConstraints evaluates a record's phase-2 constraint vocabulary in
// declaration order, emitting diagnostics at the pinned paths.
func (v *exec) runConstraints(task p2task) {
	rec, path := task.rec, task.path
	for _, c := range task.typ.Constraints {
		switch c.Form {
		case "conditional-required":
			if v.evalCondition(rec, c.When) && !hasKey(rec, c.Field) {
				v.emit("STRICTSPEC_INTRA_CONDITIONAL_REQUIRED", path, rec, map[string]diag.Slot{
					"key": diag.SlotString{S: c.Field}, "condition": diag.SlotString{S: renderCondition(c.When)},
				})
			}
		case "forbidden-when":
			if v.evalCondition(rec, c.When) && hasKey(rec, c.Field) {
				fn, _ := entryOf(rec, c.Field)
				v.emit("STRICTSPEC_INTRA_FORBIDDEN_WHEN", appendKey(path, c.Field), fn, map[string]diag.Slot{
					"key": diag.SlotString{S: c.Field}, "condition": diag.SlotString{S: renderCondition(c.When)},
				})
			}
		case "conditional-value":
			if fn, ok := entryOf(rec, c.Field); ok && v.evalCondition(rec, c.When) && !v.sameScalar(c.EqualsLiteral, fn) {
				v.emit("STRICTSPEC_INTRA_CONDITIONAL_VALUE", appendKey(path, c.Field), fn, map[string]diag.Slot{
					"key":       diag.SlotString{S: c.Field},
					"expected":  diag.SlotValue{V: svalToValue(c.EqualsLiteral)},
					"got":       diag.SlotValue{V: v.valueOf(fn)},
					"condition": diag.SlotString{S: renderCondition(c.When)},
				})
			}
		case "exactly-one-of":
			present := presentOf(rec, c.Fields)
			if len(present) != 1 {
				v.emit("STRICTSPEC_INTRA_EXACTLY_ONE_OF", path, rec, map[string]diag.Slot{
					"fields": strList(c.Fields), "actual": strList(present),
				})
			}
		case "at-least-one-of":
			if len(presentOf(rec, c.Fields)) == 0 {
				v.emit("STRICTSPEC_INTRA_AT_LEAST_ONE_OF", path, rec, map[string]diag.Slot{
					"fields": strList(c.Fields),
				})
			}
		case "co-presence":
			present := presentOf(rec, c.Fields)
			if len(present) != 0 && len(present) != len(c.Fields) {
				v.emit("STRICTSPEC_INTRA_CO_PRESENCE", path, rec, map[string]diag.Slot{
					"fields": strList(c.Fields), "actual": strList(present),
				})
			}
		case "mutual-exclusion":
			present := presentOf(rec, c.Fields)
			if len(present) >= 2 {
				v.emit("STRICTSPEC_INTRA_MUTUAL_EXCLUSION", path, rec, map[string]diag.Slot{
					"fields": strList(c.Fields), "actual": strList(present),
				})
			}
		case "collections-disjoint":
			v.collectionsDisjoint(rec, path, c)
		case "unique-by":
			v.uniqueBy(rec, path, c)
		case "pairwise-distinct":
			v.pairwiseDistinct(rec, path, c)
		case "ranges-disjoint":
			v.rangesDisjoint(rec, path, c)
		case "ordered-pair":
			v.orderedPair(rec, path, c)
		case "intra-document-references":
			v.intraReferences(rec, path, c)
		case "count-limit":
			v.countLimit(path, c)
		case "sum-limit":
			v.sumLimit(path, c)
		}
	}
}

func (v *exec) collectionsDisjoint(rec doc.Node, path diag.Path, c *schema.Constraint) {
	left := v.stringElems(rec, c.Left)
	right := v.stringElems(rec, c.Right)
	seen := map[string]bool{}
	for _, s := range left {
		seen[normalize(s, c.Normalization)] = true
	}
	for i, s := range right {
		if seen[normalize(s, c.Normalization)] {
			_ = i
			v.emit("STRICTSPEC_INTRA_COLLECTIONS_DISJOINT", path, rec, map[string]diag.Slot{
				"fields":        strList([]string{c.Left, c.Right}),
				"value":         diag.SlotValue{V: diag.StringVal(s)},
				"normalization": diag.SlotString{S: normOr(c.Normalization)},
			})
			return
		}
	}
}

func (v *exec) uniqueBy(rec doc.Node, path diag.Path, c *schema.Constraint) {
	coll, ok := entryOf(rec, c.Collection)
	if !ok || coll.Kind() != doc.Array {
		return
	}
	seen := map[string]bool{}
	for _, elem := range coll.Items() {
		fn, ok := entryOf(elem, c.UniqField)
		if !ok || fn.Kind() != doc.String {
			continue
		}
		val := v.decodeString(fn)
		key := normalize(val, c.Normalization)
		if seen[key] {
			v.emit("STRICTSPEC_INTRA_UNIQUE_BY", appendKey(path, c.Collection), coll, map[string]diag.Slot{
				"value":         diag.SlotValue{V: diag.StringVal(val)},
				"field":         diag.SlotString{S: c.UniqField},
				"normalization": diag.SlotString{S: normOr(c.Normalization)},
			})
			return
		}
		seen[key] = true
	}
}

func (v *exec) pairwiseDistinct(rec doc.Node, path diag.Path, c *schema.Constraint) {
	coll, ok := entryOf(rec, c.Collection)
	if !ok || coll.Kind() != doc.Array {
		return
	}
	seen := map[string]bool{}
	for _, elem := range coll.Items() {
		if elem.Kind() != doc.String {
			continue
		}
		val := v.decodeString(elem)
		key := normalize(val, c.Normalization)
		if seen[key] {
			v.emit("STRICTSPEC_INTRA_PAIRWISE_DISTINCT", appendKey(path, c.Collection), coll, map[string]diag.Slot{
				"value":         diag.SlotValue{V: diag.StringVal(val)},
				"normalization": diag.SlotString{S: normOr(c.Normalization)},
			})
			return
		}
		seen[key] = true
	}
}

func (v *exec) rangesDisjoint(rec doc.Node, path diag.Path, c *schema.Constraint) {
	coll, ok := entryOf(rec, c.Collection)
	if !ok || coll.Kind() != doc.Array {
		return
	}
	type rng struct{ start, end int64 }
	var ranges []rng
	for _, elem := range coll.Items() {
		s, sok := intField(elem, c.Start)
		l, lok := intField(elem, c.Length)
		if !sok || !lok {
			continue
		}
		ranges = append(ranges, rng{start: s, end: s + l})
	}
	fmtRange := func(r rng) string {
		return "[" + itoa(r.start) + ", " + itoa(r.end) + ")"
	}
	for i := 0; i < len(ranges); i++ {
		for j := 0; j < i; j++ {
			a, b := ranges[j], ranges[i]
			if a.start < b.end && b.start < a.end {
				v.emit("STRICTSPEC_INTRA_RANGES_DISJOINT", appendKey(path, c.Collection), coll, map[string]diag.Slot{
					"value":  diag.SlotString{S: fmtRange(b)},
					"actual": diag.SlotString{S: fmtRange(a)},
				})
				return
			}
		}
	}
}

func (v *exec) orderedPair(rec doc.Node, path diag.Path, c *schema.Constraint) {
	ln, lok := numField(rec, c.Less)
	tn, tok := numField(rec, c.Than)
	if !lok || !tok {
		return
	}
	if !(ln < tn) {
		v.emit("STRICTSPEC_INTRA_ORDERED_PAIR", path, rec, map[string]diag.Slot{
			"actual": diag.SlotString{S: c.Less},
			"value":  diag.SlotString{S: c.Than},
		})
	}
}

// intraReferences resolves references within the document against a root-level
// collection (the pinned resolves_by shapes in the corpus).
func (v *exec) intraReferences(rec doc.Node, path diag.Path, c *schema.Constraint) {
	keyset := v.rootKeyset(c.ResolvesInto)
	if keyset == nil {
		return
	}
	// Projection form: "collection[].field".
	if strings.Contains(c.Reference, "[].") {
		parts := strings.SplitN(c.Reference, "[].", 2)
		coll, ok := entryOf(v.root, parts[0])
		if !ok || coll.Kind() != doc.Array {
			return
		}
		for i, elem := range coll.Items() {
			fn, ok := entryOf(elem, parts[1])
			if !ok || fn.Kind() != doc.String {
				continue
			}
			val := v.decodeString(fn)
			if !keyset[val] {
				p := appendKey(appendIndex(appendKey(diag.NewPath(), parts[0]), i), parts[1])
				v.emit("STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", p, fn,
					map[string]diag.Slot{"value": diag.SlotValue{V: diag.StringVal(val)}})
			}
		}
		return
	}
	refNode, ok := entryOf(rec, c.Reference)
	if !ok || refNode == nil {
		return
	}
	switch refNode.Kind() {
	case doc.Array:
		for i, elem := range refNode.Items() {
			if elem.Kind() != doc.String {
				continue
			}
			val := v.decodeString(elem)
			if !keyset[val] {
				v.emit("STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", appendIndex(appendKey(path, c.Reference), i), elem,
					map[string]diag.Slot{"value": diag.SlotValue{V: diag.StringVal(val)}})
			}
		}
	case doc.String:
		val := v.decodeString(refNode)
		if !keyset[val] {
			v.emit("STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", appendKey(path, c.Reference), refNode,
				map[string]diag.Slot{"value": diag.SlotValue{V: diag.StringVal(val)}})
		}
	case doc.Record: // map: each key must resolve
		for _, e := range refNode.Entries() {
			if !keyset[e.Key] {
				v.emit("STRICTSPEC_INTRA_REFERENCE_UNRESOLVED", appendKey(path, c.Reference), refNode,
					map[string]diag.Slot{"value": diag.SlotValue{V: diag.StringVal(e.Key)}})
			}
		}
	case doc.Null:
		// nullable reference: null short-circuits.
	}
}

// rootKeyset builds the membership set of a root-level collection (map keys, or
// array element names/values).
func (v *exec) rootKeyset(name string) map[string]bool {
	coll, ok := entryOf(v.root, name)
	if !ok {
		return nil
	}
	set := map[string]bool{}
	switch coll.Kind() {
	case doc.Record: // map: keys
		for _, e := range coll.Entries() {
			set[e.Key] = true
		}
	case doc.Array:
		for _, elem := range coll.Items() {
			switch elem.Kind() {
			case doc.String:
				set[v.decodeString(elem)] = true
			case doc.Record:
				if nn, ok := entryOf(elem, "name"); ok && nn.Kind() == doc.String {
					set[v.decodeString(nn)] = true
				}
			}
		}
	}
	return set
}

// --- cross-document aggregates ----------------------------------------------

func (v *exec) countLimit(path diag.Path, c *schema.Constraint) {
	docs, ok := v.evidence[c.Selection]
	if !ok {
		return // domain checks not requested for this resolver (structural-only)
	}
	count := int64(len(docs))
	limit := c.Limit.Int
	violated := (c.Compare == "<=" && count > limit) || (c.Compare == ">=" && count < limit)
	if violated {
		v.emit("STRICTSPEC_CROSS_COUNT_LIMIT", path, v.root, map[string]diag.Slot{
			"actual": diag.SlotInt{N: count},
			"source": diag.SlotString{S: c.Selection},
			"limit":  diag.SlotInt{N: limit},
		})
	}
}

func (v *exec) sumLimit(path diag.Path, c *schema.Constraint) {
	docs, ok := v.evidence[c.Selection]
	if !ok {
		return
	}
	var sum float64
	allInt := true
	for _, d := range docs {
		val, present := d[c.SumField]
		f, numeric := asFloat(val)
		if !present || !numeric {
			v.emit("STRICTSPEC_CROSS_SUM_FIELD_MISSING", path, v.root, map[string]diag.Slot{
				"source": diag.SlotString{S: c.Selection},
				"field":  diag.SlotString{S: c.SumField},
				"actual": diag.SlotString{S: docName(d)},
			})
			return
		}
		if f != float64(int64(f)) {
			allInt = false
		}
		sum += f
	}
	limit := svalNum(c.Limit)
	violated := (c.Compare == "<=" && sum > limit) || (c.Compare == ">=" && sum < limit)
	if violated {
		v.emit("STRICTSPEC_CROSS_SUM_LIMIT", path, v.root, map[string]diag.Slot{
			"field":  diag.SlotString{S: c.SumField},
			"source": diag.SlotString{S: c.Selection},
			"actual": diag.SlotValue{V: sumValue(sum, allInt)},
			"limit":  diag.SlotValue{V: svalToValue(c.Limit)},
		})
	}
}

// --- small helpers ----------------------------------------------------------

func presentOf(rec doc.Node, fields []string) []string {
	var out []string
	for _, f := range fields {
		if hasKey(rec, f) {
			out = append(out, f)
		}
	}
	return out
}

func strList(names []string) diag.SlotList {
	elems := make([]diag.Value, len(names))
	for i, n := range names {
		elems[i] = diag.StringVal(n)
	}
	return diag.SlotList{Elems: elems}
}

func (v *exec) stringElems(rec doc.Node, field string) []string {
	n, ok := entryOf(rec, field)
	if !ok || n.Kind() != doc.Array {
		return nil
	}
	var out []string
	for _, e := range n.Items() {
		if e.Kind() == doc.String {
			out = append(out, v.decodeString(e))
		}
	}
	return out
}

func intField(rec doc.Node, field string) (int64, bool) {
	n, ok := entryOf(rec, field)
	if !ok || n.Kind() != doc.Integer {
		return 0, false
	}
	return svalIntLexeme(n.Lexeme()), true
}

func numField(rec doc.Node, field string) (float64, bool) {
	n, ok := entryOf(rec, field)
	if !ok {
		return 0, false
	}
	return numOf(n)
}

func normalize(s, mode string) string {
	switch mode {
	case "case-fold":
		return strings.ToLower(s)
	case "trim":
		return strings.TrimSpace(s)
	default:
		return s
	}
}

func normOr(mode string) string {
	if mode == "" {
		return "none"
	}
	return mode
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}

func sumValue(sum float64, allInt bool) diag.Value {
	if allInt {
		return diag.NumberVal{Lexeme: itoa(int64(sum)), IntClass: true}
	}
	return diag.FloatVal{F: sum}
}

func docName(d map[string]any) string {
	if n, ok := d["name"].(string); ok {
		return n
	}
	return "<document>"
}
