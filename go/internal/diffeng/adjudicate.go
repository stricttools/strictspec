package diffeng

import (
	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/strdecode"
	"github.com/smm-h/strictspec/go/internal/write"
)

// Adjudication is a parsed adjudication file (appendix-certificates.md Part B):
// a no-corpus consumer's committed discharge of otherwise-unsupported claims.
type Adjudication struct {
	SchemaID   string
	OldFV      int64
	NewFV      int64
	Entries    []AdjEntry
	SourcePath string
}

// AdjEntry is one adjudication entry (B.2): it discharges exactly one claim,
// matched by (ClaimKind, Scope == the claim's statement).
type AdjEntry struct {
	ClaimKind     string
	Scope         string
	Justification string
	Author        string
	Date          string
}

// ParseAdjudication reads and VALIDATES an adjudication file (a strictspec-schema'd
// TOML document, appendix-certificates.md Part B) against its built-in schema, and
// returns the typed value for the gate cross-check. A malformed file yields
// STRICTSPEC_DIFF_ADJUDICATION_INVALID (and a nil value).
func ParseAdjudication(src []byte, path string) (*Adjudication, []diag.Diagnostic) {
	wd, werr := write.New(doc.FormatTOML, src)
	if werr != nil {
		return nil, []diag.Diagnostic{adjInvalid(path, werr.Error())}
	}
	if diags := ir.Execute(adjProgram(), wd.Root(), ir.ExecOptions{Format: doc.FormatTOML}); len(diags) > 0 {
		return nil, []diag.Diagnostic{adjInvalid(path, renderAll(diags))}
	}
	adj := &Adjudication{SourcePath: path}
	root := wd.Root()
	adj.SchemaID = adjStr(root, "schema_id")
	adj.OldFV = adjInt(root, "old_format_version")
	adj.NewFV = adjInt(root, "new_format_version")
	if node, ok := adjEntry(root, "adjudications"); ok && node.Kind() == doc.Array {
		for _, it := range node.Items() {
			adj.Entries = append(adj.Entries, AdjEntry{
				ClaimKind:     adjStr(it, "claim_kind"),
				Scope:         adjStr(it, "scope"),
				Justification: adjStr(it, "justification"),
				Author:        adjStr(it, "author"),
				Date:          adjStr(it, "date"),
			})
		}
	}
	return adj, nil
}

// Adjudicate is the DIFF-CLI side of the deploy gate (appendix-certificates.md A.5
// / Part B). Each claim graded corpus-supported WITHOUT genuine corpus support
// (Supported == false) is a no-corpus/unsupported situation that must be
// discharged by a matching adjudication entry; an uncovered one is
// STRICTSPEC_DIFF_ADJUDICATION_MISSING. Every adjudication entry must map to a
// real unsupported claim; a stray (dangling/over-broad) entry is
// STRICTSPEC_DIFF_ADJUDICATION_INVALID. adj may be nil (no adjudication file):
// then every unsupported claim is MISSING. The release-side gate (rlsbl) is out
// of scope here — this only produces the diff CLI's gate diagnostics.
func Adjudicate(cert *Certificate, adj *Adjudication) []diag.Diagnostic {
	var out []diag.Diagnostic

	// Which claims genuinely need discharging: corpus-supported but vacuous.
	// (violated claims already block; proven is reserved and never emitted here.)
	unsupported := map[int]*Claim{}
	for i := range cert.Claims {
		c := &cert.Claims[i]
		if c.Grade == GradeCorpusSupported && !c.Supported {
			unsupported[i] = c
		}
	}

	var entries []AdjEntry
	if adj != nil {
		entries = adj.Entries
	}
	entryUsed := make([]bool, len(entries))

	// Each unsupported claim must be covered by exactly a matching entry.
	for _, c := range unsupported {
		covered := false
		for j := range entries {
			if entries[j].ClaimKind == c.Kind && entries[j].Scope == c.Statement {
				entryUsed[j] = true
				covered = true
			}
		}
		if !covered {
			out = append(out, diag.Diagnostic{
				Code: "STRICTSPEC_DIFF_ADJUDICATION_MISSING",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"condition": diag.SlotString{S: c.Statement},
				},
			})
		}
	}

	// Any entry matching no unsupported claim is dangling/over-broad — invalid.
	for j := range entries {
		if entryUsed[j] {
			continue
		}
		src := ""
		if adj != nil {
			src = adj.SourcePath
		}
		out = append(out, adjInvalid(src,
			"adjudication entry (claim_kind="+entries[j].ClaimKind+", scope="+entries[j].Scope+
				") matches no unsupported claim in the certificate"))
	}
	return out
}

func adjInvalid(path, detail string) diag.Diagnostic {
	return diag.Diagnostic{
		Code: "STRICTSPEC_DIFF_ADJUDICATION_INVALID",
		Path: diag.NewPath(),
		Slots: map[string]diag.Slot{
			"source": diag.SlotString{S: path},
			"detail": diag.SlotString{S: detail},
		},
	}
}

// --- node helpers -----------------------------------------------------------

func adjEntry(rec doc.Node, key string) (doc.Node, bool) {
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

func adjStr(rec doc.Node, key string) string {
	n, ok := adjEntry(rec, key)
	if !ok {
		return ""
	}
	if n.Kind() == doc.String {
		return strdecode.TOML(n.Lexeme())
	}
	// Date/other scalars: surface the lexeme verbatim.
	return n.Lexeme()
}

func adjInt(rec doc.Node, key string) int64 {
	n, ok := adjEntry(rec, key)
	if !ok || n.Kind() != doc.Integer {
		return 0
	}
	var v int64
	neg := false
	for i, c := range n.Lexeme() {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int64(c-'0')
	}
	if neg {
		v = -v
	}
	return v
}
