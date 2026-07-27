package migrate

import (
	"bytes"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/write"
)

// ApplyUp applies m's forward ops to a single-document source (JSON or TOML) and
// bumps format_version to m.To. It returns the new bytes and any migration
// diagnostics (empty on success). It does NOT revalidate — that is the chain's
// job (MigrateDocument).
func ApplyUp(m *Migration, format doc.Format, src []byte) ([]byte, []diag.Diagnostic) {
	wd, err := write.New(format, src)
	if err != nil {
		return nil, spliceErr(err)
	}
	for _, op := range m.Ops {
		if diags := applyOp(wd, op); len(diags) > 0 {
			return nil, diags
		}
	}
	if diags := setFormatVersion(wd, m.To); len(diags) > 0 {
		return nil, diags
	}
	return wd.Bytes(), nil
}

// ApplyDown applies m's author-supplied inverse (down) ops to a single-document
// source and sets format_version back to m.From. An op declared irreversible with
// no down_ops is a STRICTSPEC_MIGRATE_IRREVERSIBLE_DOWN hard error.
func ApplyDown(m *Migration, format doc.Format, src []byte) ([]byte, []diag.Diagnostic) {
	if m.DeclaredTaxonomy() == DownIrreversible {
		return nil, []diag.Diagnostic{{
			Code: "STRICTSPEC_MIGRATE_IRREVERSIBLE_DOWN",
			Path: diag.NewPath(),
			Slots: map[string]diag.Slot{
				"op": diag.SlotString{S: m.Set},
			},
		}}
	}
	wd, err := write.New(format, src)
	if err != nil {
		return nil, spliceErr(err)
	}
	for _, op := range m.DownOps {
		if diags := applyOp(wd, op); len(diags) > 0 {
			return nil, diags
		}
	}
	if diags := setFormatVersion(wd, m.From); len(diags) > 0 {
		return nil, diags
	}
	return wd.Bytes(), nil
}

// Result is one document's migration outcome.
type Result struct {
	Output  []byte
	Diags   []diag.Diagnostic
	Changed bool
}

// MigrateDocument applies the ordered chain of forward migrations to one document
// (JSON or TOML) and REVALIDATES the result against prog (the target schema
// compiled to an IR program). A migration producing an invalid document is a hard
// error: STRICTSPEC_MIGRATE_REVALIDATION_FAILED followed by the underlying
// validation diagnostics. prog may be nil to skip revalidation (used by the diff
// engine's round-trip, which validates separately).
func MigrateDocument(chain []*Migration, prog *ir.Program, format doc.Format, src []byte) Result {
	cur := src
	for _, m := range chain {
		out, diags := ApplyUp(m, format, cur)
		if len(diags) > 0 {
			return Result{Diags: diags}
		}
		cur = out
	}
	res := Result{Output: cur, Changed: !bytes.Equal(cur, src)}
	if prog != nil {
		if vdiags := revalidate(prog, format, cur); len(vdiags) > 0 {
			res.Diags = append([]diag.Diagnostic{{
				Code: "STRICTSPEC_MIGRATE_REVALIDATION_FAILED",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"expected": diag.SlotVersion{V: prog.FormatVersion()},
				},
			}}, vdiags...)
		}
	}
	return res
}

// revalidate runs the IR validator over migrated bytes.
func revalidate(prog *ir.Program, format doc.Format, src []byte) []diag.Diagnostic {
	root, err := parseSingle(format, src)
	if err != nil {
		return []diag.Diagnostic{{Code: "STRICTSPEC_PARSE_JSON_SYNTAX", Path: diag.NewPath(),
			Slots: map[string]diag.Slot{"detail": diag.SlotString{S: err.Error()}}}}
	}
	fmtToUse := format
	if fmtToUse == doc.FormatJSONL {
		fmtToUse = doc.FormatJSON
	}
	return ir.Execute(prog, root, ir.ExecOptions{Format: fmtToUse})
}

// MigrateJSONL migrates a JSONL stream PER LINE (each line is an independent
// JSON document; spec/DESIGN.md — JSONL: per-line migration). Every line is
// migrated through the chain and revalidated independently; the result rejoins
// lines with LF, preserving a trailing newline when the input had one. Any line
// failing anywhere makes the whole run fail (atomicity is the caller's rename
// sweep; here we simply report the first line's diagnostics).
func MigrateJSONL(chain []*Migration, prog *ir.Program, src []byte) Result {
	trailingLF := len(src) > 0 && src[len(src)-1] == '\n'
	body := src
	if trailingLF {
		body = src[:len(src)-1]
	}
	lines := bytes.Split(body, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	changed := false
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			// Preserve blank lines verbatim (they are per-line edge cases; migrating
			// a blank line is a no-op here — validation would reject them separately).
			out = append(out, line)
			continue
		}
		res := MigrateDocument(chain, prog, doc.FormatJSONL, line)
		if len(res.Diags) > 0 {
			// Anchor the diagnostics to the failing line for legibility.
			for j := range res.Diags {
				res.Diags[j].Path = res.Diags[j].Path.WithAnchor(i+1, 0)
			}
			return Result{Diags: res.Diags}
		}
		if res.Changed {
			changed = true
		}
		out = append(out, res.Output)
	}
	joined := bytes.Join(out, []byte("\n"))
	if trailingLF {
		joined = append(joined, '\n')
	}
	return Result{Output: joined, Changed: changed}
}

func parseSingle(format doc.Format, src []byte) (doc.Node, error) {
	wd, err := write.New(format, src)
	if err != nil {
		return nil, err
	}
	return wd.Root(), nil
}
