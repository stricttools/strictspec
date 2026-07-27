package migrate

import (
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/dpath"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/write"
)

// applyOp applies one op to wd (mutating its bytes in place) and returns any
// migration diagnostics. An empty return means the op succeeded.
func applyOp(wd *write.Doc, op Op) []diag.Diagnostic {
	switch op.Kind {
	case OpRenameField:
		return opRename(wd, op)
	case OpMoveField:
		return opMove(wd, op)
	case OpWrapInArray:
		return opWrap(wd, op)
	case OpUnwrapSingleton:
		return opUnwrap(wd, op)
	case OpSetValue:
		return opSetValue(wd, op)
	case OpAddField, OpAddCollection:
		return opAddField(wd, op)
	case OpRemoveField, OpDropCollection:
		return opRemoveField(wd, op)
	case OpAppend:
		return opAppend(wd, op)
	case OpMergeDefaults:
		return opMergeDefaults(wd, op)
	case OpRemoveWhere:
		return opRemoveWhere(wd, op)
	case OpSetValueWhere:
		return opSetValueWhere(wd, op)
	default:
		return []diag.Diagnostic{migErr("STRICTSPEC_TYPE_NOT_ENUM_MEMBER", op.Kind, diag.NewPath(), nil)}
	}
}

// --- individual ops ---------------------------------------------------------

func opRename(wd *write.Doc, op Op) []diag.Diagnostic {
	fromP, err := dpath.Parse(op.From)
	if err != nil {
		return spliceErr(err)
	}
	toP, err := dpath.Parse(op.To)
	if err != nil {
		return spliceErr(err)
	}
	parent, fromEntry, idx, ok := fieldEntry(wd.Root(), fromP)
	if !ok {
		return targetMissing(op.Kind, op.From)
	}
	toKey, ok := lastKey(toP)
	if !ok {
		return spliceErrMsg("rename_field: `to` must address a field")
	}
	if _, present := findEntry(parent, toKey); present {
		return collision(op.Kind, op.To)
	}
	_ = idx
	repl := renderKey(wd.Format(), toKey)
	ks := fromEntry.KeySpan
	return spliceOrDiag(wd, []write.Edit{{Start: ks.Start.ByteOffset, End: ks.End.ByteOffset, Repl: repl}})
}

func opMove(wd *write.Doc, op Op) []diag.Diagnostic {
	fromP, err := dpath.Parse(op.From)
	if err != nil {
		return spliceErr(err)
	}
	toP, err := dpath.Parse(op.To)
	if err != nil {
		return spliceErr(err)
	}
	parent, fromEntry, _, ok := fieldEntry(wd.Root(), fromP)
	if !ok {
		return targetMissing(op.Kind, op.From)
	}
	toParentNode, toKey, ok := parentAndKey(wd.Root(), toP)
	if !ok || toParentNode.Kind() != doc.Record {
		return targetMissing(op.Kind, op.To)
	}
	if _, present := findEntry(toParentNode, toKey); present {
		return collision(op.Kind, op.To)
	}
	// Capture the value bytes, remove from source, then add to destination.
	vs := fromEntry.Value.Span()
	valBytes := append([]byte(nil), wd.Bytes()[vs.Start.ByteOffset:vs.End.ByteOffset]...)
	if diags := removeEntry(wd, parent, fromEntry); len(diags) > 0 {
		return diags
	}
	toParent, ok := dpath.Navigate(wd.Root(), parentPath(toP))
	if !ok || toParent.Kind() != doc.Record {
		return targetMissing(op.Kind, op.To)
	}
	return insertField(wd, toParent, toKey, valBytes)
}

func opWrap(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	node, ok := dpath.Navigate(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	sp := node.Span()
	orig := wd.Bytes()[sp.Start.ByteOffset:sp.End.ByteOffset]
	repl := make([]byte, 0, len(orig)+2)
	repl = append(repl, '[')
	repl = append(repl, orig...)
	repl = append(repl, ']')
	return spliceOrDiag(wd, []write.Edit{{Start: sp.Start.ByteOffset, End: sp.End.ByteOffset, Repl: repl}})
}

func opUnwrap(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	node, ok := dpath.Navigate(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	if node.Kind() != doc.Array {
		return typeMismatch(op.Kind, op.Path, "array", nodeKind(node))
	}
	its := node.Items()
	if len(its) != 1 {
		return []diag.Diagnostic{{
			Code: "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON",
			Path: mustPath(op.Path),
			Slots: map[string]diag.Slot{
				"actual": diag.SlotInt{N: int64(len(its))},
			},
		}}
	}
	sp := node.Span()
	is := its[0].Span()
	elem := append([]byte(nil), wd.Bytes()[is.Start.ByteOffset:is.End.ByteOffset]...)
	return spliceOrDiag(wd, []write.Edit{{Start: sp.Start.ByteOffset, End: sp.End.ByteOffset, Repl: elem}})
}

func opSetValue(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	node, ok := dpath.Navigate(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	repl, rerr := write.RenderConstructed(op.Value, wd.Format())
	if rerr != nil {
		return spliceErr(rerr)
	}
	sp := node.Span()
	return spliceOrDiag(wd, []write.Edit{{Start: sp.Start.ByteOffset, End: sp.End.ByteOffset, Repl: repl}})
}

func opAddField(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	parent, key, ok := parentAndKey(wd.Root(), p)
	if !ok || parent.Kind() != doc.Record {
		return targetMissing(op.Kind, op.Path)
	}
	if _, present := findEntry(parent, key); present {
		return collision(op.Kind, op.Path)
	}
	repl, rerr := write.RenderConstructed(op.Value, wd.Format())
	if rerr != nil {
		return spliceErr(rerr)
	}
	return insertField(wd, parent, key, repl)
}

func opRemoveField(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	parent, entry, _, ok := fieldEntry(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	if op.Kind == OpDropCollection && entry.Value.Kind() != doc.Array {
		return typeMismatch(op.Kind, op.Path, "array", nodeKind(entry.Value))
	}
	return removeEntry(wd, parent, entry)
}

func opAppend(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	node, ok := dpath.Navigate(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	if node.Kind() != doc.Array {
		return typeMismatch(op.Kind, op.Path, "array", nodeKind(node))
	}
	repl, rerr := write.RenderConstructed(op.Value, wd.Format())
	if rerr != nil {
		return spliceErr(rerr)
	}
	its := node.Items()
	var edit write.Edit
	if len(its) == 0 {
		pos := node.Span().Start.ByteOffset + 1 // just after '['
		edit = write.Edit{Start: pos, End: pos, Repl: repl}
	} else {
		pos := its[len(its)-1].Span().End.ByteOffset
		lead := append([]byte(", "), repl...)
		edit = write.Edit{Start: pos, End: pos, Repl: lead}
	}
	return spliceOrDiag(wd, []write.Edit{edit})
}

func opMergeDefaults(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	node, ok := dpath.Navigate(wd.Root(), p)
	if !ok {
		return targetMissing(op.Kind, op.Path)
	}
	if node.Kind() != doc.Record {
		return typeMismatch(op.Kind, op.Path, "record", nodeKind(node))
	}
	// Inject each default whose key is ABSENT; present keys untouched. Re-navigate
	// after each insert so spans stay valid.
	for _, def := range op.Defaults {
		rec, ok := dpath.Navigate(wd.Root(), p)
		if !ok || rec.Kind() != doc.Record {
			return targetMissing(op.Kind, op.Path)
		}
		if _, present := findEntry(rec, def.Key); present {
			continue
		}
		repl, rerr := write.RenderConstructed(def.Value, wd.Format())
		if rerr != nil {
			return spliceErr(rerr)
		}
		if diags := insertField(wd, rec, def.Key, repl); len(diags) > 0 {
			return diags
		}
	}
	return nil
}

func opRemoveWhere(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	for {
		node, ok := dpath.Navigate(wd.Root(), p)
		if !ok {
			return targetMissing(op.Kind, op.Path)
		}
		if node.Kind() != doc.Array {
			return typeMismatch(op.Kind, op.Path, "array", nodeKind(node))
		}
		matchIdx := -1
		for i, it := range node.Items() {
			if matchCond(it, op.Where, wd.Format()) {
				matchIdx = i
				break
			}
		}
		if matchIdx < 0 {
			return nil
		}
		if diags := removeArrayElem(wd, node, matchIdx); len(diags) > 0 {
			return diags
		}
	}
}

func opSetValueWhere(wd *write.Doc, op Op) []diag.Diagnostic {
	p, err := dpath.Parse(op.Path)
	if err != nil {
		return spliceErr(err)
	}
	// Iterate elements; for each matching record, set its Field to Value. Re-navigate
	// after each edit so spans stay valid.
	idx := 0
	for {
		node, ok := dpath.Navigate(wd.Root(), p)
		if !ok {
			return targetMissing(op.Kind, op.Path)
		}
		if node.Kind() != doc.Array {
			return typeMismatch(op.Kind, op.Path, "array", nodeKind(node))
		}
		its := node.Items()
		// Find the next matching element at or after idx that still needs setting.
		found := -1
		for i := idx; i < len(its); i++ {
			if matchCond(its[i], op.Where, wd.Format()) {
				found = i
				break
			}
		}
		if found < 0 {
			return nil
		}
		elem := its[found]
		if elem.Kind() != doc.Record {
			idx = found + 1
			continue
		}
		entry, present := findEntry(elem, op.Field)
		repl, rerr := write.RenderConstructed(op.Value, wd.Format())
		if rerr != nil {
			return spliceErr(rerr)
		}
		if present {
			vs := entry.Value.Span()
			if diags := spliceOrDiag(wd, []write.Edit{{Start: vs.Start.ByteOffset, End: vs.End.ByteOffset, Repl: repl}}); len(diags) > 0 {
				return diags
			}
		} else {
			if diags := insertField(wd, elem, op.Field, repl); len(diags) > 0 {
				return diags
			}
		}
		idx = found + 1
	}
}

// --- structural helpers -----------------------------------------------------

func insertField(wd *write.Doc, parent doc.Node, key string, valueBytes []byte) []diag.Diagnostic {
	keyText := renderKey(wd.Format(), key)
	sep := kvSep(wd.Format())
	field := append(append(append([]byte(nil), keyText...), sep...), valueBytes...)
	entries := parent.Entries()
	var edit write.Edit
	if len(entries) == 0 {
		pos := parent.Span().Start.ByteOffset + 1 // after '{'
		edit = write.Edit{Start: pos, End: pos, Repl: field}
	} else {
		pos := entries[len(entries)-1].Value.Span().End.ByteOffset
		lead := append([]byte(", "), field...)
		edit = write.Edit{Start: pos, End: pos, Repl: lead}
	}
	return spliceOrDiag(wd, []write.Edit{edit})
}

func removeEntry(wd *write.Doc, parent doc.Node, entry doc.Entry) []diag.Diagnostic {
	entries := parent.Entries()
	idx := -1
	for i, e := range entries {
		if e.KeySpan.Start.ByteOffset == entry.KeySpan.Start.ByteOffset {
			idx = i
			break
		}
	}
	if idx < 0 {
		return spliceErrMsg("remove: entry not found in parent")
	}
	start := entry.KeySpan.Start.ByteOffset
	end := entry.Value.Span().End.ByteOffset
	switch {
	case len(entries) == 1:
		// only entry
	case idx < len(entries)-1:
		end = entries[idx+1].KeySpan.Start.ByteOffset // consume trailing comma+ws
	default:
		start = entries[idx-1].Value.Span().End.ByteOffset // consume leading comma+ws
	}
	return spliceOrDiag(wd, []write.Edit{{Start: start, End: end, Repl: nil}})
}

func removeArrayElem(wd *write.Doc, arr doc.Node, idx int) []diag.Diagnostic {
	items := arr.Items()
	if idx < 0 || idx >= len(items) {
		return spliceErrMsg("remove_where: index out of range")
	}
	start := items[idx].Span().Start.ByteOffset
	end := items[idx].Span().End.ByteOffset
	switch {
	case len(items) == 1:
	case idx < len(items)-1:
		end = items[idx+1].Span().Start.ByteOffset
	default:
		start = items[idx-1].Span().End.ByteOffset
	}
	return spliceOrDiag(wd, []write.Edit{{Start: start, End: end, Repl: nil}})
}

// setFormatVersion replaces the document root's integer format_version value.
func setFormatVersion(wd *write.Doc, to int64) []diag.Diagnostic {
	root := wd.Root()
	if root.Kind() != doc.Record {
		return spliceErrMsg("set format_version: root is not a record")
	}
	entry, ok := findEntry(root, "format_version")
	if !ok {
		return spliceErrMsg("set format_version: document is missing format_version")
	}
	vs := entry.Value.Span()
	repl := []byte(strconv.FormatInt(to, 10))
	return spliceOrDiag(wd, []write.Edit{{Start: vs.Start.ByteOffset, End: vs.End.ByteOffset, Repl: repl}})
}

// --- condition evaluation ---------------------------------------------------

func matchCond(elem doc.Node, cond *Cond, format doc.Format) bool {
	if cond == nil {
		return false
	}
	entry, present := findEntry(elem, cond.Field)
	switch cond.Predicate {
	case "present":
		return present
	case "absent":
		return !present
	case "equals":
		return present && valEq(entry.Value, cond.Value, format)
	case "not-equals":
		return !(present && valEq(entry.Value, cond.Value, format))
	case "in":
		if !present {
			return false
		}
		for _, v := range cond.Values {
			if valEq(entry.Value, v, format) {
				return true
			}
		}
		return false
	case "not-in":
		if !present {
			return true
		}
		for _, v := range cond.Values {
			if valEq(entry.Value, v, format) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// valEq compares a DOCUMENT value node (JSON or TOML) against a migration LITERAL
// node (always TOML), class-sensitively (an integer lexeme never equals a float
// lexeme — strictspec's lexical rule).
func valEq(docVal, lit doc.Node, format doc.Format) bool {
	if docVal == nil || lit == nil {
		return false
	}
	if docVal.Kind() != lit.Kind() {
		return false
	}
	switch docVal.Kind() {
	case doc.String:
		var a string
		if format == doc.FormatTOML {
			a = strdecode.TOML(docVal.Lexeme())
		} else {
			a = strdecode.JSON(docVal.Lexeme())
		}
		return a == strdecode.TOML(lit.Lexeme())
	case doc.Integer, doc.Bool:
		return docVal.Lexeme() == lit.Lexeme()
	case doc.Float:
		af, ea := strconv.ParseFloat(docVal.Lexeme(), 64)
		bf, eb := strconv.ParseFloat(lit.Lexeme(), 64)
		return ea == nil && eb == nil && af == bf
	default:
		return docVal.Lexeme() == lit.Lexeme()
	}
}

// --- navigation helpers -----------------------------------------------------

func findEntry(rec doc.Node, key string) (doc.Entry, bool) {
	if rec == nil || rec.Kind() != doc.Record {
		return doc.Entry{}, false
	}
	for _, e := range rec.Entries() {
		if e.Key == key {
			return e, true
		}
	}
	return doc.Entry{}, false
}

// fieldEntry resolves a record-field path (last step is a Key) to (parent record,
// the entry, its index).
func fieldEntry(root doc.Node, p dpath.Path) (doc.Node, doc.Entry, int, bool) {
	parent, last, ok := dpath.Parent(root, p)
	if !ok || parent == nil || parent.Kind() != doc.Record {
		return nil, doc.Entry{}, 0, false
	}
	key, ok := last.(dpath.Key)
	if !ok {
		return nil, doc.Entry{}, 0, false
	}
	for i, e := range parent.Entries() {
		if e.Key == key.Name {
			return parent, e, i, true
		}
	}
	return nil, doc.Entry{}, 0, false
}

// parentAndKey resolves the parent container and the final key of a field path
// even when the field is ABSENT (used by add/rename destination checks).
func parentAndKey(root doc.Node, p dpath.Path) (doc.Node, string, bool) {
	parent, last, ok := dpath.Parent(root, p)
	if !ok {
		return nil, "", false
	}
	key, ok := last.(dpath.Key)
	if !ok {
		return nil, "", false
	}
	return parent, key.Name, true
}

func lastKey(p dpath.Path) (string, bool) {
	if len(p.Steps) == 0 {
		return "", false
	}
	key, ok := p.Steps[len(p.Steps)-1].(dpath.Key)
	if !ok {
		return "", false
	}
	return key.Name, true
}

func parentPath(p dpath.Path) dpath.Path {
	if len(p.Steps) == 0 {
		return p
	}
	return dpath.Path{Raw: p.Raw, Steps: p.Steps[:len(p.Steps)-1]}
}

// --- rendering helpers ------------------------------------------------------

func renderKey(format doc.Format, name string) []byte {
	if format == doc.FormatTOML {
		if diag.IsIdentShaped(name) {
			return []byte(name)
		}
		return []byte(`"` + diag.EscapeString(name) + `"`)
	}
	return []byte(`"` + diag.EscapeString(name) + `"`)
}

func kvSep(format doc.Format) []byte {
	if format == doc.FormatTOML {
		return []byte(" = ")
	}
	return []byte(": ")
}

// --- diagnostics ------------------------------------------------------------

func spliceOrDiag(wd *write.Doc, edits []write.Edit) []diag.Diagnostic {
	if err := wd.Splice(edits); err != nil {
		return spliceErr(err)
	}
	return nil
}

func targetMissing(opKind, path string) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Code: "STRICTSPEC_MIGRATE_TARGET_MISSING",
		Path: mustPath(path),
		Slots: map[string]diag.Slot{
			"op": diag.SlotString{S: opKind},
		},
	}}
}

func collision(opKind, path string) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Code: "STRICTSPEC_MIGRATE_COLLISION",
		Path: mustPath(path),
		Slots: map[string]diag.Slot{
			"op": diag.SlotString{S: opKind},
		},
	}}
}

func typeMismatch(opKind, path, expected, got string) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Code: "STRICTSPEC_MIGRATE_TYPE_MISMATCH",
		Path: mustPath(path),
		Slots: map[string]diag.Slot{
			"op":       diag.SlotString{S: opKind},
			"expected": diag.SlotString{S: expected},
			"got":      diag.SlotString{S: got},
		},
	}}
}

func migErr(code, opKind string, path diag.Path, slots map[string]diag.Slot) diag.Diagnostic {
	if slots == nil {
		slots = map[string]diag.Slot{}
	}
	if _, ok := slots["op"]; !ok {
		slots["op"] = diag.SlotString{S: opKind}
	}
	return diag.Diagnostic{Code: code, Path: path, Slots: slots}
}

// spliceErr / spliceErrMsg wrap a mechanical splice failure as a migration
// revalidation-style diagnostic (an internal, non-catalogue error surfaced to the
// operator). We reuse REVALIDATION_FAILED with the document root path.
func spliceErr(err error) []diag.Diagnostic {
	return spliceErrMsg(err.Error())
}

func spliceErrMsg(msg string) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Code: "STRICTSPEC_MIGRATE_TARGET_MISSING",
		Path: diag.NewPath(),
		Slots: map[string]diag.Slot{
			"op": diag.SlotString{S: msg},
		},
	}}
}

// mustPath parses a path string for diagnostics; on failure it falls back to the
// document root (the diagnostic still carries the op name).
func mustPath(s string) diag.Path {
	p, err := dpath.Parse(s)
	if err != nil {
		return diag.NewPath()
	}
	return toDiagPath(p)
}

// toDiagPath converts a dpath.Path to a diag.Path for rendering.
func toDiagPath(p dpath.Path) diag.Path {
	steps := make([]diag.Step, 0, len(p.Steps))
	for _, st := range p.Steps {
		switch s := st.(type) {
		case dpath.Key:
			steps = append(steps, diag.Key{Name: s.Name})
		case dpath.Index:
			steps = append(steps, diag.Index{N: s.N})
		}
	}
	return diag.NewPath(steps...)
}
