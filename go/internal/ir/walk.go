package ir

import (
	"sort"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// walk realizes the depth-guard and type-dispatch nodes: it validates node n
// against type t at path, returning whether n's SUBTREE was clean (zero
// diagnostics) — the partial-subtree-binding signal that gates phase 2.
func (v *exec) walk(t *schema.Type, n doc.Node, path diag.Path) bool {
	v.depth++
	defer func() { v.depth-- }()
	before := v.diags.Len()
	if v.depth > MaxValidationDepth {
		v.emit("STRICTSPEC_DEPTH_EXCEEDED", path, n,
			map[string]diag.Slot{"limit": diag.SlotInt{N: MaxValidationDepth}})
		return false
	}
	v.walkInner(t, n, path)
	clean := v.diags.Len() == before
	if n != nil {
		// Record the subtree-clean flag so phase 2 runs only for records whose
		// phase 1 passed (partial-subtree binding).
		v.clean[n] = clean
	}
	return clean
}

func (v *exec) walkInner(t *schema.Type, n doc.Node, path diag.Path) {
	switch t.Kind {
	case schema.KindRef:
		if named, ok := v.s.Types[t.Ref]; ok {
			v.walkInner(named, n, path)
			return
		}
		v.walkScalar(t, n, path)
	case schema.KindRecord:
		v.walkRecord(t, n, path)
	case schema.KindMap:
		v.walkMap(t, n, path)
	case schema.KindArray:
		v.walkArray(t, n, path)
	case schema.KindTuple:
		v.walkTuple(t, n, path)
	case schema.KindEnum:
		v.walkEnum(t, n, path)
	case schema.KindLiteral:
		v.walkLiteral(t, n, path)
	case schema.KindDiscriminatedUnion:
		v.walkDiscriminated(t, n, path)
	case schema.KindNodeKindUnion:
		v.walkNodeKind(t, n, path)
	case schema.KindNullable:
		v.walkNullable(t, n, path)
	case schema.KindOpaque:
		// Verbatim leaf: strictspec never introspects the blob (either stance).
	}
}

func (v *exec) walkRecord(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Record {
		v.emit("STRICTSPEC_TYPE_NOT_RECORD", path, n,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKindName(kindOf(n))}})
		return
	}
	isRoot := len(path.Steps) == 1 // just the Root step

	// Enqueue phase-2 constraints in PRE-ORDER (containing-record traversal order).
	if len(t.Constraints) > 0 {
		v.phase2 = append(v.phase2, p2task{typ: t, rec: n, path: path})
	}

	fieldNames := make([]string, 0, len(t.Fields))
	for _, f := range t.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	matched := map[string]bool{}

	// Document key positions: present-key diagnostics emit in DOCUMENT order
	// (spec/DESIGN.md primitives appendix item 6), so a record whose keys are
	// reordered relative to the schema declaration emits its diagnostics in the
	// order the keys appear in the document, not in declaration order.
	docIndex := map[string]int{}
	for i, e := range n.Entries() {
		if _, seen := docIndex[e.Key]; !seen {
			docIndex[e.Key] = i
		}
	}

	// Classification pass (declaration order): decide each declared field's
	// emission WITHOUT emitting yet, and record every matched document key. This
	// separates the two orderings item 6 pins: present keys (document order) and
	// missing-required (declaration order).
	const (
		actPresent = iota // present, validate the value
		actAlias          // alias + canonical both present (exactly-one rule)
	)
	type action struct {
		kind    int
		name    string // canonical field name
		typ     *schema.Type
		key     string   // matched document key (present)
		alias   string   // reported non-canonical spelling (alias case)
		declIdx int      // schema-declaration index
		docPos  int      // document position of the (earliest) matched key
		before  []string // missing-required field names anchored before this one
	}
	var present []action // present + alias, sorted to document order below
	var missing []string // missing-required field names, declaration order
	var missingIdx []int // parallel declaration indices for anchoring

	for i, f := range t.Fields {
		var found []string
		if hasKey(n, f.Name) {
			found = append(found, f.Name)
		}
		for _, a := range f.Aliases {
			if hasKey(n, a) {
				found = append(found, a)
			}
		}
		if len(found) >= 2 {
			// Alias both-present: exactly-one rule. Report the non-canonical spelling.
			aliasName := found[0]
			for _, fn := range found {
				if fn != f.Name {
					aliasName = fn
					break
				}
			}
			pos := docIndex[found[0]]
			for _, fn := range found {
				matched[fn] = true
				if docIndex[fn] < pos {
					pos = docIndex[fn]
				}
			}
			present = append(present, action{
				kind: actAlias, name: f.Name, alias: aliasName, declIdx: i, docPos: pos,
			})
			continue
		}
		if len(found) == 1 {
			key := found[0]
			matched[key] = true
			present = append(present, action{
				kind: actPresent, name: f.Name, typ: f.Type, key: key,
				declIdx: i, docPos: docIndex[key],
			})
			continue
		}
		if f.Required {
			missing = append(missing, f.Name)
			missingIdx = append(missingIdx, i)
		}
	}

	// Anchor each missing-required field (declaration order) immediately before
	// the first present field declared after it, matching the pre-existing corpus
	// (pgdesign-account-invalid, where a missing field sits between two present
	// fields) and item 6's dated interleaving pin. Missing fields declared after
	// every present field are trailing. `present` is still in declaration order
	// here (declIdx and missingIdx both ascending), so a single merge suffices.
	mi := 0
	for pi := range present {
		for mi < len(missing) && missingIdx[mi] < present[pi].declIdx {
			present[pi].before = append(present[pi].before, missing[mi])
			mi++
		}
	}
	trailing := missing[mi:]

	// Reorder present-key emissions to document order (stable: equal positions —
	// impossible for distinct keys — keep declaration order).
	sort.SliceStable(present, func(a, b int) bool {
		return present[a].docPos < present[b].docPos
	})

	emitMissing := func(names []string) {
		for _, name := range names {
			v.emit("STRICTSPEC_TYPE_MISSING_REQUIRED", path, n,
				map[string]diag.Slot{"key": diag.SlotString{S: name}})
		}
	}
	for _, p := range present {
		emitMissing(p.before)
		switch p.kind {
		case actAlias:
			v.emit("STRICTSPEC_ALIAS_BOTH_PRESENT", path, n, map[string]diag.Slot{
				"alias":     diag.SlotIdentifier{Name: p.alias},
				"canonical": diag.SlotIdentifier{Name: p.name},
			})
		case actPresent:
			val, _ := entryOf(n, p.key)
			v.walk(p.typ, val, appendKey(path, p.name))
		}
	}
	emitMissing(trailing)

	// Unknown-key pass (document order). The root format_version gate field is exempt.
	for _, e := range n.Entries() {
		if matched[e.Key] {
			continue
		}
		if isRoot && e.Key == "format_version" {
			continue
		}
		v.emit("STRICTSPEC_KEY_UNKNOWN", path, n, map[string]diag.Slot{
			"key":        diag.SlotString{S: e.Key},
			"suggestion": diag.SlotSuggestion{Unknown: e.Key, Candidates: fieldNames},
		})
	}
}

func (v *exec) walkMap(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Record {
		v.emit("STRICTSPEC_TYPE_NOT_MAP", path, n,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKindName(kindOf(n))}})
		return
	}
	var keyRe *compiledRegex
	if t.KeyPattern != "" {
		keyRe = v.compile(t.KeyPattern)
	}
	for _, e := range n.Entries() {
		kp := appendMapKey(path, e.Key)
		if keyRe != nil && !keyRe.MatchString(e.Key) {
			v.emit("STRICTSPEC_VALUE_MAP_KEY_REGEX", kp, e.Value, map[string]diag.Slot{
				"key":     diag.SlotString{S: e.Key},
				"pattern": diag.SlotValue{V: diag.StringVal(t.KeyPattern)},
			})
		}
		if t.Value != nil {
			v.walk(t.Value, e.Value, kp)
		}
	}
}

func (v *exec) walkArray(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Array {
		v.emit("STRICTSPEC_TYPE_NOT_ARRAY", path, n,
			map[string]diag.Slot{"got": diag.SlotString{S: nodeKindName(kindOf(n))}})
		return
	}
	items := n.Items()
	if t.MinLen != nil && len(items) < *t.MinLen {
		v.emit("STRICTSPEC_VALUE_ARRAY_TOO_SHORT", path, n, map[string]diag.Slot{
			"actual": diag.SlotInt{N: int64(len(items))},
			"limit":  diag.SlotInt{N: int64(*t.MinLen)},
		})
	}
	if t.MaxLen != nil && len(items) > *t.MaxLen {
		v.emit("STRICTSPEC_VALUE_ARRAY_TOO_LONG", path, n, map[string]diag.Slot{
			"actual": diag.SlotInt{N: int64(len(items))},
			"limit":  diag.SlotInt{N: int64(*t.MaxLen)},
		})
	}
	if t.Item != nil {
		for i, it := range items {
			v.walk(t.Item, it, appendIndex(path, i))
		}
	}
}

func (v *exec) walkTuple(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Array {
		v.emit("STRICTSPEC_TYPE_MISMATCH", path, n, map[string]diag.Slot{
			"expected": diag.SlotString{S: "tuple"},
			"got":      diag.SlotString{S: nodeKindName(kindOf(n))},
		})
		return
	}
	items := n.Items()
	if len(items) != len(t.Elements) {
		v.emit("STRICTSPEC_TYPE_TUPLE_ARITY", path, n, map[string]diag.Slot{
			"expected": diag.SlotInt{N: int64(len(t.Elements))},
			"got":      diag.SlotInt{N: int64(len(items))},
		})
		return
	}
	for i, elemRef := range t.Elements {
		et := &schema.Type{Kind: schema.KindRef, Ref: elemRef}
		v.walk(et, items[i], appendIndex(path, i))
	}
}

func (v *exec) walkNullable(t *schema.Type, n doc.Node, path diag.Path) {
	if n != nil && n.Kind() == doc.Null {
		return // null short-circuits
	}
	if t.Inner != nil {
		v.walkInner(t.Inner, n, path)
	}
}

func (v *exec) walkDiscriminated(t *schema.Type, n doc.Node, path diag.Path) {
	if n == nil || n.Kind() != doc.Record {
		v.emit("STRICTSPEC_TYPE_MISMATCH", path, n, map[string]diag.Slot{
			"expected": diag.SlotString{S: "record"},
			"got":      diag.SlotString{S: nodeKindName(kindOf(n))},
		})
		return
	}
	// Build arm discriminator values.
	discStrs := make([]diag.Value, 0, len(t.Arms))
	armDisc := make([]string, 0, len(t.Arms))
	for _, arm := range t.Arms {
		dv := v.armDiscriminator(arm, t.Discriminator)
		armDisc = append(armDisc, dv)
		discStrs = append(discStrs, diag.StringVal(dv))
	}
	discNode, ok := entryOf(n, t.Discriminator)
	if !ok {
		v.emit("STRICTSPEC_UNION_DISCRIMINATOR_MISSING", path, n, map[string]diag.Slot{
			"key":      diag.SlotString{S: t.Discriminator},
			"expected": diag.SlotList{Elems: discStrs},
		})
		return
	}
	got := v.scalarKeyString(discNode)
	for i, arm := range t.Arms {
		if armDisc[i] == got {
			v.walkInner(arm.Type, n, appendArm(path, arm.Name))
			return
		}
	}
	v.emit("STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN", path, discNode, map[string]diag.Slot{
		"got":        diag.SlotValue{V: v.valueOf(discNode)},
		"expected":   diag.SlotList{Elems: discStrs},
		"suggestion": diag.SlotSuggestion{Unknown: got, Candidates: armDisc},
	})
}

func (v *exec) walkNodeKind(t *schema.Type, n doc.Node, path diag.Path) {
	cat := nodeCategory(kindOf(n))
	var kinds []diag.Value
	for _, arm := range t.Arms {
		ac := v.armCategory(arm.Type)
		kinds = append(kinds, diag.StringVal(ac))
		if ac == cat {
			v.walkInner(arm.Type, n, path)
			return
		}
	}
	v.emit("STRICTSPEC_UNION_NODE_KIND", path, n, map[string]diag.Slot{
		"got":      diag.SlotString{S: cat},
		"expected": diag.SlotList{Elems: kinds},
	})
}

func (v *exec) walkEnum(t *schema.Type, n doc.Node, path diag.Path) {
	members := v.enumMembers(t)
	var memberVals []diag.Value
	for _, m := range members {
		memberVals = append(memberVals, diag.StringVal(m))
	}
	// String enums compare decoded string; integer enums compare integer value.
	if t.Sourced || allStringEnum(t) {
		if n == nil || n.Kind() != doc.String {
			v.emitEnumMiss(t, n, path, members, memberVals)
			return
		}
		val := v.decodeString(n)
		for _, m := range members {
			if m == val {
				return
			}
		}
		v.emitEnumMiss(t, n, path, members, memberVals)
		return
	}
	// Non-string (e.g. integer) inline enum.
	if n == nil {
		v.emitEnumMiss(t, n, path, members, memberVals)
		return
	}
	for _, ev := range t.EnumValues {
		if v.sameScalar(ev, n) {
			return
		}
	}
	v.emitEnumMiss(t, n, path, members, memberVals)
}

func (v *exec) emitEnumMiss(t *schema.Type, n doc.Node, path diag.Path, members []string, memberVals []diag.Value) {
	got := ""
	if n != nil && n.Kind() == doc.String {
		got = v.decodeString(n)
	}
	v.emit("STRICTSPEC_TYPE_NOT_ENUM_MEMBER", path, n, map[string]diag.Slot{
		"got":        diag.SlotValue{V: v.valueOf(n)},
		"expected":   diag.SlotList{Elems: memberVals},
		"suggestion": diag.SlotSuggestion{Unknown: got, Candidates: members},
	})
}

func (v *exec) walkLiteral(t *schema.Type, n doc.Node, path diag.Path) {
	if n != nil && v.sameScalar(t.Literal, n) {
		return
	}
	v.emit("STRICTSPEC_TYPE_NOT_LITERAL", path, n, map[string]diag.Slot{
		"expected": diag.SlotValue{V: svalToValue(t.Literal)},
		"got":      diag.SlotValue{V: v.valueOf(n)},
	})
}

// --- helpers ----------------------------------------------------------------

func kindOf(n doc.Node) doc.Kind {
	if n == nil {
		return doc.Null
	}
	return n.Kind()
}

// armDiscriminator resolves an arm's discriminator literal value (as a string).
func (v *exec) armDiscriminator(arm *schema.Arm, discField string) string {
	rec := v.resolveRecord(arm.Type)
	if rec != nil {
		for _, f := range rec.Fields {
			if f.Name == discField && f.Type != nil && f.Type.Kind == schema.KindLiteral {
				return svalKeyString(f.Type.Literal)
			}
		}
	}
	return arm.Name
}

// resolveRecord follows named references to a record type, if any.
func (v *exec) resolveRecord(t *schema.Type) *schema.Type {
	seen := 0
	for t != nil && t.Kind == schema.KindRef && seen < 32 {
		named, ok := v.s.Types[t.Ref]
		if !ok {
			return nil
		}
		t = named
		seen++
	}
	if t != nil && t.Kind == schema.KindRecord {
		return t
	}
	return t
}

// armCategory returns the node-kind category (scalar/record/array) an arm accepts.
func (v *exec) armCategory(t *schema.Type) string {
	seen := 0
	for t != nil && t.Kind == schema.KindRef {
		if named, ok := v.s.Types[t.Ref]; ok && seen < 32 {
			t = named
			seen++
			continue
		}
		return "scalar" // builtin/custom scalar
	}
	if t == nil {
		return "scalar"
	}
	switch t.Kind {
	case schema.KindRecord, schema.KindMap:
		return "record"
	case schema.KindArray, schema.KindTuple:
		return "array"
	default:
		return "scalar"
	}
}

func (v *exec) enumMembers(t *schema.Type) []string {
	if t.Sourced {
		return t.Baked
	}
	out := make([]string, 0, len(t.EnumValues))
	for _, ev := range t.EnumValues {
		out = append(out, svalKeyString(ev))
	}
	return out
}

func allStringEnum(t *schema.Type) bool {
	for _, ev := range t.EnumValues {
		if ev.Kind != doc.String {
			return false
		}
	}
	return len(t.EnumValues) > 0
}

// scalarKeyString returns a document scalar's comparable key (decoded string, or
// integer/bool lexeme) for discriminator matching.
func (v *exec) scalarKeyString(n doc.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind() == doc.String {
		return v.decodeString(n)
	}
	return n.Lexeme()
}

// svalKeyString returns a schema literal's comparable key.
func svalKeyString(sv schema.SVal) string {
	switch sv.Kind {
	case doc.String:
		return sv.Str
	default:
		return sv.Lexeme
	}
}

// sameScalar reports whether a document scalar node equals a schema literal by
// class and value.
func (v *exec) sameScalar(sv schema.SVal, n doc.Node) bool {
	if n == nil {
		return false
	}
	switch sv.Kind {
	case doc.String:
		return n.Kind() == doc.String && v.decodeString(n) == sv.Str
	case doc.Integer:
		return n.Kind() == doc.Integer && svalIntLexeme(n.Lexeme()) == sv.Int
	case doc.Bool:
		return n.Kind() == doc.Bool && (n.Lexeme() == "true") == sv.Bool
	case doc.Float:
		return n.Kind() == doc.Float && n.Lexeme() == sv.Lexeme
	default:
		return false
	}
}

func appendKey(p diag.Path, key string) diag.Path {
	return appendStep(p, diag.Key{Name: key})
}
func appendMapKey(p diag.Path, key string) diag.Path {
	return appendStep(p, diag.MapKey{Name: key})
}
func appendIndex(p diag.Path, i int) diag.Path {
	return appendStep(p, diag.Index{N: i})
}
func appendArm(p diag.Path, name string) diag.Path {
	return appendStep(p, diag.Arm{Name: name})
}
func appendStep(p diag.Path, s diag.Step) diag.Path {
	steps := make([]diag.Step, len(p.Steps)+1)
	copy(steps, p.Steps)
	steps[len(p.Steps)] = s
	return diag.Path{Steps: steps, Anchor: p.Anchor}
}
