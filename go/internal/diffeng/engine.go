package diffeng

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/migrate"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/write"
)

// Inputs bundles a diff run's ambient inputs. The corpus files are already
// resolved (ResolveGlob) so the engine performs no glob logic itself.
type Inputs struct {
	SchemaID    string
	OldProg     *ir.Program
	NewProg     *ir.Program
	OldFV       int64
	NewFV       int64
	Migration   *migrate.Migration // nil in same-version mode
	Glob        string
	Files       []string
	Release     string
	SameVersion bool
}

type corpusDoc struct {
	path     string
	format   doc.Format
	root     doc.Node
	bytes    []byte
	parseOK  bool
	validOld bool
	validNew bool
	killNew  []diag.Diagnostic // killing diagnostics under the new schema
}

// Run executes the empirical diff and returns the certificate plus the
// hard-error VIOLATION diagnostics (empty = green). An empty corpus is a hard
// error (STRICTSPEC_DIFF_CORPUS_EMPTY) with no certificate.
func Run(in Inputs) (*Certificate, []diag.Diagnostic) {
	if len(in.Files) == 0 {
		return nil, []diag.Diagnostic{{
			Code:  "STRICTSPEC_DIFF_CORPUS_EMPTY",
			Path:  diag.NewPath(),
			Slots: map[string]diag.Slot{"source": diag.SlotString{S: in.Glob}},
		}}
	}
	hash, _ := ContentHash(in.Files)
	docs := loadCorpus(in)

	cert := &Certificate{
		CertificateFormatVersion: CertFormatVersion,
		SchemaID:                 in.SchemaID,
		NewFormatVersion:         in.NewFV,
		Corpus: CorpusIdentity{
			DeclaredGlob:      in.Glob,
			ResolvedFileCount: len(in.Files),
			ContentHash:       hash,
		},
		StrictspecRelease: in.Release,
	}
	var violations []diag.Diagnostic

	if in.SameVersion {
		cert.OldFormatVersion = SameVersionMarker
		claim := sameVersionFlipScan(docs, in.NewFV, &violations)
		cert.Claims = append(cert.Claims, claim)
		return cert, violations
	}

	cert.OldFormatVersion = in.OldFV
	if in.Migration != nil {
		cert.MigrationSetID = in.Migration.Set
	}
	cert.Claims = append(cert.Claims,
		completenessClaim(in, docs, &violations),
		soundnessClaim(in, docs, &violations),
		downTaxonomyClaim(in, docs, &violations),
	)
	return cert, violations
}

// --- same-version flip-scan -------------------------------------------------

func sameVersionFlipScan(docs []corpusDoc, sharedFV int64, violations *[]diag.Diagnostic) Claim {
	claim := Claim{
		Kind:      KindFlipScan,
		Statement: "no corpus document valid under the old schema is rejected by the new schema at the same format_version",
	}
	witnesses := 0
	for _, d := range docs {
		if d.validOld {
			witnesses++
		}
		if d.validOld && !d.validNew {
			claim.Counterexamples = append(claim.Counterexamples, Counterexample{
				DocumentPath: d.path,
				Diagnostics:  toDiagJ(d.killNew),
			})
			*violations = append(*violations, diag.Diagnostic{
				Code: "STRICTSPEC_DIFF_NARROWING_UNBUMPED",
				Path: diag.NewPath(),
				Slots: map[string]diag.Slot{
					"source":   diag.SlotString{S: d.path},
					"expected": diag.SlotVersion{V: sharedFV},
				},
			})
		}
	}
	claim.Supported = witnesses > 0
	claim.Grade = gradeFor(claim.Counterexamples)
	return claim
}

// --- migrate round-trip -----------------------------------------------------

func completenessClaim(in Inputs, docs []corpusDoc, violations *[]diag.Diagnostic) Claim {
	claim := Claim{
		Kind:      KindRoundTripCompleteness,
		Statement: "the migration never errors on a corpus document valid at N",
	}
	if in.Migration == nil {
		claim.Grade = GradeCorpusSupported
		return claim
	}
	witnesses := 0
	for _, d := range docs {
		if !d.validOld {
			continue
		}
		witnesses++
		_, mdiags := migrate.ApplyUp(in.Migration, d.format, d.bytes)
		if len(mdiags) > 0 {
			claim.Counterexamples = append(claim.Counterexamples, Counterexample{
				DocumentPath: d.path,
				Diagnostics:  toDiagJ(mdiags),
			})
			*violations = append(*violations, diffViolated(claim.Statement, d.path))
		}
	}
	claim.Supported = witnesses > 0
	claim.Grade = gradeFor(claim.Counterexamples)
	return claim
}

func soundnessClaim(in Inputs, docs []corpusDoc, violations *[]diag.Diagnostic) Claim {
	claim := Claim{
		Kind:      KindRoundTripSoundness,
		Statement: "every corpus document valid at N re-validates at N+1 after M",
	}
	if in.Migration == nil {
		claim.Grade = GradeCorpusSupported
		return claim
	}
	witnesses := 0
	for _, d := range docs {
		if !d.validOld {
			continue
		}
		out, mdiags := migrate.ApplyUp(in.Migration, d.format, d.bytes)
		if len(mdiags) > 0 {
			continue // completeness already flags this
		}
		witnesses++
		vdiags := validateBytes(in.NewProg, d.format, out)
		if len(vdiags) > 0 {
			claim.Counterexamples = append(claim.Counterexamples, Counterexample{
				DocumentPath: d.path,
				Diagnostics:  toDiagJ(vdiags),
			})
			*violations = append(*violations, diffViolated(claim.Statement, d.path))
		}
	}
	claim.Supported = witnesses > 0
	claim.Grade = gradeFor(claim.Counterexamples)
	return claim
}

// --- down-taxonomy verification ---------------------------------------------

func downTaxonomyClaim(in Inputs, docs []corpusDoc, violations *[]diag.Diagnostic) Claim {
	claim := Claim{
		Kind:      KindDownTaxonomy,
		Statement: "the declared down taxonomy holds over the corpus",
	}
	if in.Migration == nil {
		claim.Grade = GradeCorpusSupported
		return claim
	}
	declared := in.Migration.DeclaredTaxonomy()
	successes, failures := 0, 0
	var firstFail *corpusDoc
	var firstFailDiags []diag.Diagnostic
	if declared != migrate.DownIrreversible {
		for i := range docs {
			d := &docs[i]
			if !d.validNew {
				continue
			}
			_, ddiags := migrate.ApplyDown(in.Migration, d.format, d.bytes)
			if len(ddiags) > 0 {
				failures++
				if firstFail == nil {
					firstFail = d
					firstFailDiags = ddiags
				}
			} else {
				successes++
			}
		}
	}
	claim.Supported = (successes + failures) > 0
	actual := actualTaxonomy(successes, failures, declared)
	if taxonomyMisdeclared(declared, actual) {
		src := ""
		var cdiags []diag.Diagnostic
		if firstFail != nil {
			src = firstFail.path
			cdiags = firstFailDiags
		}
		claim.Counterexamples = append(claim.Counterexamples, Counterexample{
			DocumentPath: src,
			Diagnostics:  toDiagJ(cdiags),
		})
		*violations = append(*violations, diag.Diagnostic{
			Code: "STRICTSPEC_DIFF_TAXONOMY_MISDECLARED",
			Path: diag.NewPath(),
			Slots: map[string]diag.Slot{
				"op":       diag.SlotString{S: in.Migration.Set},
				"expected": diag.SlotString{S: declared},
				"actual":   diag.SlotString{S: actual},
			},
		})
	}
	claim.Grade = gradeFor(claim.Counterexamples)
	return claim
}

func actualTaxonomy(successes, failures int, declared string) string {
	switch {
	case failures == 0 && successes > 0:
		return migrate.DownTotal
	case failures > 0 && successes > 0:
		return migrate.DownPartial
	case failures > 0 && successes == 0:
		return migrate.DownIrreversible
	default:
		// No witnesses: cannot disprove the declaration.
		return declared
	}
}

// taxonomyMisdeclared: the corpus DISPROVES the declaration. Declaring `total`
// while a down fails is the pinned budget case; `partial` permits both outcomes,
// so it is never disproven; `irreversible` is not empirically exercised here.
func taxonomyMisdeclared(declared, actual string) bool {
	if declared == migrate.DownTotal && actual != migrate.DownTotal {
		return true
	}
	return false
}

// --- corpus loading ---------------------------------------------------------

func loadCorpus(in Inputs) []corpusDoc {
	docs := make([]corpusDoc, 0, len(in.Files))
	for _, f := range in.Files {
		cd := corpusDoc{path: f, format: formatOf(f)}
		b, err := os.ReadFile(f)
		if err != nil {
			docs = append(docs, cd)
			continue
		}
		cd.bytes = b
		wd, werr := write.New(cd.format, b)
		if werr != nil {
			docs = append(docs, cd)
			continue
		}
		cd.parseOK = true
		cd.root = wd.Root()
		cd.validOld = len(validateNode(in.OldProg, cd.format, cd.root)) == 0
		nk := validateNode(in.NewProg, cd.format, cd.root)
		cd.validNew = len(nk) == 0
		cd.killNew = nk
		docs = append(docs, cd)
	}
	return docs
}

func validateNode(prog *ir.Program, format doc.Format, root doc.Node) []diag.Diagnostic {
	if prog == nil || root == nil {
		return nil
	}
	f := format
	if f == doc.FormatJSONL {
		f = doc.FormatJSON
	}
	return ir.Execute(prog, root, ir.ExecOptions{Format: f})
}

func validateBytes(prog *ir.Program, format doc.Format, src []byte) []diag.Diagnostic {
	wd, err := write.New(format, src)
	if err != nil {
		return []diag.Diagnostic{{Code: "STRICTSPEC_PARSE_JSON_SYNTAX", Path: diag.NewPath(),
			Slots: map[string]diag.Slot{"detail": diag.SlotString{S: err.Error()}}}}
	}
	return validateNode(prog, format, wd.Root())
}

// --- helpers ----------------------------------------------------------------

func gradeFor(cexs []Counterexample) string {
	if len(cexs) > 0 {
		return GradeViolated
	}
	return GradeCorpusSupported
}

func diffViolated(statement, source string) diag.Diagnostic {
	return diag.Diagnostic{
		Code: "STRICTSPEC_DIFF_VIOLATED",
		Path: diag.NewPath(),
		Slots: map[string]diag.Slot{
			"condition": diag.SlotString{S: statement},
			"source":    diag.SlotString{S: source},
		},
	}
}

func toDiagJ(diags []diag.Diagnostic) []DiagnosticJ {
	out := make([]DiagnosticJ, 0, len(diags))
	for _, d := range diags {
		out = append(out, DiagnosticJ{
			Code:    d.Code,
			Path:    d.Path.Render(),
			Message: safeRender(d),
		})
	}
	return out
}

// safeRender renders a diagnostic, falling back to the code if the template/slots
// are not renderable (defensive — engine-internal diagnostics may carry
// non-catalogue slot shapes).
func safeRender(d diag.Diagnostic) (msg string) {
	defer func() {
		if recover() != nil {
			msg = d.Code
		}
	}()
	return render.Render(d)
}

func formatOf(path string) doc.Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return doc.FormatTOML
	case ".jsonl":
		return doc.FormatJSONL
	default:
		return doc.FormatJSON
	}
}
