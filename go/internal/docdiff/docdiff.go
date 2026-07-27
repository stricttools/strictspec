// Package docdiff is the strictspec `doc-diff` engine (spec/DESIGN.md — Accepted-set
// semantics, diff, and doc-diff; output shape in appendix-certificates.md Part C).
// It takes ONE schema and TWO documents of it (same schema, same format_version)
// and emits a structured per-path delta: added / removed / changed / moved, in
// traversal order (document order). Change detection is LEXEME-CLASS-AWARE — `1`
// (integer) vs `1.0` (float) at the same path is a `changed` delta because the
// A.1 rendered form differs. Array element MOVES are detected only when the
// array's schema declares a `unique-by`. It is a toolchain-only analysis;
// conformance is golden-output determinism, not multi-target execution.
package docdiff

import (
	"strconv"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
)

// Delta is one per-path structural delta (appendix-certificates.md C.2).
type Delta struct {
	Op       string  `json:"op"` // added | removed | changed | moved
	Path     string  `json:"path"`
	OldValue *string `json:"old_value,omitempty"`
	NewValue *string `json:"new_value,omitempty"`
	OldIndex *int    `json:"old_index,omitempty"`
	NewIndex *int    `json:"new_index,omitempty"`
}

// Result is the doc-diff output (appendix-certificates.md C.1).
type Result struct {
	SchemaID      string  `json:"schema_id"`
	FormatVersion int64   `json:"format_version"`
	Deltas        []Delta `json:"deltas"`
}

type differ struct {
	s      *schema.Schema
	format doc.Format
	out    []Delta
	// line is the 1-based JSONL line the current documents came from (0 = not
	// JSONL). When set, every delta's path carries the @Lline:byte suffix (C.2:
	// JSONL deltas are line-scoped).
	line int
}

// Diff compares two parsed documents of s and returns the structured delta.
// The caller is responsible for validating operands and the format_version match
// (see Compute for the full CLI-facing entry).
func Diff(s *schema.Schema, format doc.Format, oldRoot, newRoot doc.Node) *Result {
	d := &differ{s: s, format: format}
	rt, _ := s.LookupType(s.Root)
	d.walk(rt, oldRoot, newRoot, diag.NewPath())
	return &Result{SchemaID: s.Name, FormatVersion: s.FormatVersion, Deltas: d.out}
}

func (d *differ) emit(delta Delta) { d.out = append(d.out, delta) }

// render renders a delta path, appending the JSONL @Lline:byte suffix when the
// differ is line-scoped. The byte offset is the addressed node's within-line
// start offset (nodes are parsed per line, so spans are already line-relative).
func (d *differ) render(p diag.Path, anchor doc.Node) string {
	if d.line <= 0 {
		return p.Render()
	}
	off := 0
	if anchor != nil {
		if sp := anchor.Span(); sp.Start.IsValid() {
			off = sp.Start.ByteOffset
		}
	}
	return p.WithAnchor(d.line, off).Render()
}

// walk recursively diffs old vs new at the given schema type and path.
func (d *differ) walk(t *schema.Type, oldN, newN doc.Node, path diag.Path) {
	// Kind mismatch (record vs array vs scalar) is a whole-node change.
	if categoryOf(oldN) != categoryOf(newN) {
		d.emitChanged(oldN, newN, path)
		return
	}
	switch categoryOf(oldN) {
	case "record":
		d.walkRecord(t, oldN, newN, path)
	case "array":
		d.walkArray(t, oldN, newN, path)
	default:
		if d.renderNode(oldN) != d.renderNode(newN) {
			d.emitChanged(oldN, newN, path)
		}
	}
}

func (d *differ) walkRecord(t *schema.Type, oldN, newN doc.Node, path diag.Path) {
	rt := d.resolve(t)
	oldKeys := entryOrder(oldN)
	newSet := entrySet(newN)
	seen := map[string]bool{}
	for _, k := range oldKeys {
		seen[k] = true
		kp := appendKey(path, k)
		ov, _ := entryOf(oldN, k)
		nv, present := entryOf(newN, k)
		if !present {
			d.emit(Delta{Op: "removed", Path: d.render(kp, ov), OldValue: strp(d.renderNode(ov))})
			continue
		}
		// Record-scope unique-by (the pinned surface places `unique-by` on the record
		// owning the collection: collection = <field>, field = <key>).
		if ov.Kind() == doc.Array && nv.Kind() == doc.Array {
			if key, ok := recordUniqueBy(rt, k); ok {
				d.walkArrayKeyed(d.resolve(fieldType(rt, k)), ov, nv, kp, key)
				continue
			}
		}
		d.walk(fieldType(rt, k), ov, nv, kp)
	}
	for _, k := range entryOrder(newN) {
		if seen[k] {
			continue
		}
		if _, ok := newSet[k]; ok {
			kp := appendKey(path, k)
			nv, _ := entryOf(newN, k)
			d.emit(Delta{Op: "added", Path: d.render(kp, nv), NewValue: strp(d.renderNode(nv))})
		}
	}
}

func (d *differ) walkArray(t *schema.Type, oldN, newN doc.Node, path diag.Path) {
	at := d.resolve(t)
	// unique-by move detection: the array's schema declares a unique-by keyed by a
	// field. When present, match elements by key across the two documents.
	if key, ok := d.uniqueByKey(at); ok {
		d.walkArrayKeyed(at, oldN, newN, path, key)
		return
	}
	itemT := arrayItem(at)
	oldItems := oldN.Items()
	newItems := newN.Items()
	n := max(len(oldItems), len(newItems))
	for i := 0; i < n; i++ {
		ip := appendIndex(path, i)
		switch {
		case i < len(oldItems) && i < len(newItems):
			d.walk(itemT, oldItems[i], newItems[i], ip)
		case i < len(oldItems):
			d.emit(Delta{Op: "removed", Path: d.render(ip, oldItems[i]), OldValue: strp(d.renderNode(oldItems[i]))})
		default:
			d.emit(Delta{Op: "added", Path: d.render(ip, newItems[i]), NewValue: strp(d.renderNode(newItems[i]))})
		}
	}
}

// walkArrayKeyed matches elements by their unique-by key: a matched element at a
// different index yields `moved` (not removed+added).
func (d *differ) walkArrayKeyed(at *schema.Type, oldN, newN doc.Node, path diag.Path, key string) {
	itemT := arrayItem(at)
	oldItems := oldN.Items()
	newItems := newN.Items()
	oldByKey := map[string]int{}
	for i, it := range oldItems {
		if kv, ok := elemKey(it, key); ok {
			oldByKey[kv] = i
		}
	}
	newByKey := map[string]int{}
	for i, it := range newItems {
		if kv, ok := elemKey(it, key); ok {
			newByKey[kv] = i
		}
	}
	// New-document order drives traversal; removed elements reported after.
	for ni, it := range newItems {
		kv, ok := elemKey(it, key)
		ip := appendIndex(path, ni)
		if !ok {
			d.emit(Delta{Op: "added", Path: d.render(ip, it), NewValue: strp(d.renderNode(it))})
			continue
		}
		oi, existed := oldByKey[kv]
		if !existed {
			d.emit(Delta{Op: "added", Path: d.render(ip, it), NewValue: strp(d.renderNode(it))})
			continue
		}
		if oi != ni {
			oidx, nidx := oi, ni
			d.emit(Delta{Op: "moved", Path: d.render(ip, it), OldIndex: &oidx, NewIndex: &nidx})
		}
		// Diff element contents regardless of move.
		d.walk(itemT, oldItems[oi], it, ip)
	}
	for oi, it := range oldItems {
		kv, ok := elemKey(it, key)
		if !ok {
			continue
		}
		if _, still := newByKey[kv]; !still {
			d.emit(Delta{Op: "removed", Path: d.render(appendIndex(path, oi), it), OldValue: strp(d.renderNode(it))})
		}
	}
}

func (d *differ) emitChanged(oldN, newN doc.Node, path diag.Path) {
	// Anchor the delta at the NEW node's within-line offset (the new document is
	// the reference for changed/added/moved deltas, per C.2).
	d.emit(Delta{
		Op:       "changed",
		Path:     d.render(path, newN),
		OldValue: strp(d.renderNode(oldN)),
		NewValue: strp(d.renderNode(newN)),
	})
}

// renderNode renders a document node to its A.1 form (lexeme-class-aware).
func (d *differ) renderNode(n doc.Node) string {
	return render.Value(NodeValue(n, d.format))
}

// --- schema type navigation -------------------------------------------------

func (d *differ) resolve(t *schema.Type) *schema.Type {
	for t != nil && t.Kind == schema.KindRef {
		next, ok := d.s.LookupType(t.Ref)
		if !ok {
			return t
		}
		t = next
	}
	return t
}

func (d *differ) uniqueByKey(arrType *schema.Type) (string, bool) {
	// unique-by is declared on the array type's constraints (collection is the
	// array itself when the constraint sits at the array scope) OR is discovered
	// via an item-field. We support the array-scope declaration with a `field`.
	if arrType == nil {
		return "", false
	}
	for _, c := range arrType.Constraints {
		if c.Form == "unique-by" && c.UniqField != "" {
			return c.UniqField, true
		}
	}
	return "", false
}

// recordUniqueBy reports the unique-by key for an array FIELD when the owning
// record declares `unique-by` with collection == fieldName.
func recordUniqueBy(rt *schema.Type, fieldName string) (string, bool) {
	if rt == nil {
		return "", false
	}
	for _, c := range rt.Constraints {
		if c.Form == "unique-by" && c.Collection == fieldName && c.UniqField != "" {
			return c.UniqField, true
		}
	}
	return "", false
}

func fieldType(rt *schema.Type, key string) *schema.Type {
	if rt == nil {
		return nil
	}
	for _, f := range rt.Fields {
		if f.Name == key {
			return f.Type
		}
	}
	if rt.Value != nil { // map value
		return rt.Value
	}
	return nil
}

func arrayItem(at *schema.Type) *schema.Type {
	if at == nil {
		return nil
	}
	return at.Item
}

// --- node helpers -----------------------------------------------------------

func categoryOf(n doc.Node) string {
	switch n.Kind() {
	case doc.Record:
		return "record"
	case doc.Array:
		return "array"
	default:
		return "scalar"
	}
}

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

func entryOrder(rec doc.Node) []string {
	if rec == nil || rec.Kind() != doc.Record {
		return nil
	}
	out := make([]string, 0, len(rec.Entries()))
	for _, e := range rec.Entries() {
		out = append(out, e.Key)
	}
	return out
}

func entrySet(rec doc.Node) map[string]bool {
	out := map[string]bool{}
	for _, k := range entryOrder(rec) {
		out[k] = true
	}
	return out
}

func elemKey(elem doc.Node, key string) (string, bool) {
	v, ok := entryOf(elem, key)
	if !ok {
		return "", false
	}
	// Key identity is the value's lexeme (adequate for string/int unique keys).
	return v.Kind().String() + ":" + v.Lexeme(), true
}

func appendKey(p diag.Path, key string) diag.Path {
	steps := make([]diag.Step, len(p.Steps)+1)
	copy(steps, p.Steps)
	steps[len(p.Steps)] = diag.Key{Name: key}
	return diag.Path{Steps: steps, Anchor: p.Anchor}
}

func appendIndex(p diag.Path, i int) diag.Path {
	steps := make([]diag.Step, len(p.Steps)+1)
	copy(steps, p.Steps)
	steps[len(p.Steps)] = diag.Index{N: i}
	return diag.Path{Steps: steps, Anchor: p.Anchor}
}

func strp(s string) *string { return &s }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- validation / mismatch entry --------------------------------------------

// Compute is the CLI-facing entry: it validates both operands against s, checks
// the format_version match, and returns either the delta or the appropriate
// STRICTSPEC_DOCDIFF_* diagnostic.
func Compute(prog *ir.Program, s *schema.Schema, format doc.Format,
	oldPath string, oldRoot doc.Node, newPath string, newRoot doc.Node) (*Result, []diag.Diagnostic) {

	fmtToUse := format
	if fmtToUse == doc.FormatJSONL {
		fmtToUse = doc.FormatJSON
	}
	if diags := ir.Execute(prog, oldRoot, ir.ExecOptions{Format: fmtToUse}); len(diags) > 0 {
		return nil, []diag.Diagnostic{invalidOperand(oldPath)}
	}
	if diags := ir.Execute(prog, newRoot, ir.ExecOptions{Format: fmtToUse}); len(diags) > 0 {
		return nil, []diag.Diagnostic{invalidOperand(newPath)}
	}
	ofv, _ := formatVersionOf(oldRoot)
	nfv, _ := formatVersionOf(newRoot)
	if ofv != nfv {
		return nil, []diag.Diagnostic{{
			Code: "STRICTSPEC_DOCDIFF_SCHEMA_MISMATCH",
			Path: diag.NewPath(),
			Slots: map[string]diag.Slot{
				"source": diag.SlotString{S: oldPath},
				"actual": diag.SlotString{S: newPath},
			},
		}}
	}
	return Diff(s, format, oldRoot, newRoot), nil
}

// ComputeJSONL is the CLI-facing entry for JSONL doc-diff: it diffs two JSONL
// streams PER LINE (each line is an independent JSON document; C.2: JSONL deltas
// are line-scoped). Every line is validated against the schema (an invalid line
// is STRICTSPEC_DOCDIFF_INVALID_OPERAND); corresponding lines are diffed and every
// resulting delta carries the @Lline:byte suffix in its path. Lines present in
// only one stream yield a whole-document added/removed delta on that line. The
// format_version match is subsumed by validation (the version gate makes every
// valid line carry the schema's format_version).
func ComputeJSONL(prog *ir.Program, s *schema.Schema,
	oldPath string, oldSrc []byte, newPath string, newSrc []byte) (*Result, []diag.Diagnostic) {

	oldDocs, operr := jsondoc.ParseLines(oldSrc)
	if operr != nil {
		return nil, []diag.Diagnostic{invalidOperand(oldPath)}
	}
	newDocs, nperr := jsondoc.ParseLines(newSrc)
	if nperr != nil {
		return nil, []diag.Diagnostic{invalidOperand(newPath)}
	}
	for _, dd := range oldDocs {
		if diags := ir.Execute(prog, dd.Root, ir.ExecOptions{Format: doc.FormatJSON}); len(diags) > 0 {
			return nil, []diag.Diagnostic{invalidOperand(oldPath)}
		}
	}
	for _, dd := range newDocs {
		if diags := ir.Execute(prog, dd.Root, ir.ExecOptions{Format: doc.FormatJSON}); len(diags) > 0 {
			return nil, []diag.Diagnostic{invalidOperand(newPath)}
		}
	}
	res := &Result{SchemaID: s.Name, FormatVersion: s.FormatVersion}
	rt, _ := s.LookupType(s.Root)
	n := len(oldDocs)
	if len(newDocs) > n {
		n = len(newDocs)
	}
	for i := 0; i < n; i++ {
		d := &differ{s: s, format: doc.FormatJSONL, line: i + 1}
		switch {
		case i < len(oldDocs) && i < len(newDocs):
			d.walk(rt, oldDocs[i].Root, newDocs[i].Root, diag.NewPath())
		case i < len(oldDocs):
			d.emit(Delta{Op: "removed", Path: d.render(diag.NewPath(), oldDocs[i].Root),
				OldValue: strp(d.renderNode(oldDocs[i].Root))})
		default:
			d.emit(Delta{Op: "added", Path: d.render(diag.NewPath(), newDocs[i].Root),
				NewValue: strp(d.renderNode(newDocs[i].Root))})
		}
		res.Deltas = append(res.Deltas, d.out...)
	}
	return res, nil
}

func invalidOperand(path string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:  "STRICTSPEC_DOCDIFF_INVALID_OPERAND",
		Path:  diag.NewPath(),
		Slots: map[string]diag.Slot{"source": diag.SlotString{S: path}},
	}
}

func formatVersionOf(root doc.Node) (int64, bool) {
	if root == nil || root.Kind() != doc.Record {
		return 0, false
	}
	for _, e := range root.Entries() {
		if e.Key == "format_version" && e.Value.Kind() == doc.Integer {
			v, err := strconv.ParseInt(e.Value.Lexeme(), 10, 64)
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
}
