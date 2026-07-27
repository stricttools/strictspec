// Package diffeng is the strictspec `diff` EMPIRICAL ENGINE (spec/DESIGN.md —
// Accepted-set semantics, diff, and doc-diff; certificate shape in
// appendix-certificates.md Part A). Given a schema at two format versions, the
// migration M between them, and a REQUIRED corpus of real documents, it runs:
//
//   - FLIP-SCAN (same-version mode): the corpus replayed against the OLD and NEW
//     schema at the SAME format_version; any valid->invalid flip is an UN-BUMPED
//     NARROWING — a hard error (STRICTSPEC_DIFF_NARROWING_UNBUMPED).
//   - MIGRATE-ROUND-TRIP soundness (M(d) revalidates at N+1) and completeness
//     (M never errors on a valid-at-N corpus doc).
//   - DOWN-TAXONOMY verification (the declared total/partial/irreversible taxonomy
//     exercised against the corpus; a mis-declaration is a hard error).
//
// It emits ONE CERTIFICATE (JSON) whose claims each carry an evidence grade
// (violated | corpus-supported; `proven` is reserved for the future analyzer).
// The certificate self-validates against a built-in strictspec schema (the shape
// IS a strictspec schema — dogfooding).
package diffeng

// Certificate is the `strictspec diff` output (appendix-certificates.md A.1).
type Certificate struct {
	CertificateFormatVersion int            `json:"certificate_format_version"`
	SchemaID                 string         `json:"schema_id"`
	OldFormatVersion         any            `json:"old_format_version"` // integer OR the marker "same-version"
	NewFormatVersion         int64          `json:"new_format_version"`
	MigrationSetID           string         `json:"migration_set_id,omitempty"` // ABSENT in same-version mode
	Corpus                   CorpusIdentity `json:"corpus"`
	Claims                   []Claim        `json:"claims"`
	StrictspecRelease        string         `json:"strictspec_release"`
}

// CorpusIdentity pins the corpus a certificate speaks for (A.2).
type CorpusIdentity struct {
	DeclaredGlob      string `json:"declared_glob"`
	ResolvedFileCount int    `json:"resolved_file_count"`
	ContentHash       string `json:"content_hash"`
}

// Claim is one verified claim (A.3).
type Claim struct {
	Kind            string           `json:"kind"`
	Grade           string           `json:"grade"`
	Counterexamples []Counterexample `json:"counterexamples,omitempty"`
	Statement       string           `json:"statement"`

	// Supported records whether the DECLARED corpus actually exercised this claim
	// (≥1 witnessing document). A claim graded corpus-supported with Supported ==
	// false is VACUOUSLY supported — a no-corpus/unsupported situation the deploy
	// gate (A.5) requires be discharged via an adjudication entry. Not serialized:
	// the certificate shape (A.3) is pinned, so support is engine-internal gate
	// state, not a certificate field.
	Supported bool `json:"-"`
}

// Counterexample is a real corpus witness of a violation (A.4).
type Counterexample struct {
	DocumentPath string        `json:"document_path"`
	Diagnostics  []DiagnosticJ `json:"diagnostics"`
}

// DiagnosticJ is a rendered diagnostic embedded in a counterexample.
type DiagnosticJ struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Claim kinds (A.3) and grades (A.5).
const (
	KindFlipScan              = "flip-scan"
	KindRoundTripSoundness    = "migrate-round-trip-soundness"
	KindRoundTripCompleteness = "migrate-round-trip-completeness"
	KindDownTaxonomy          = "down-taxonomy"

	GradeViolated        = "violated"
	GradeCorpusSupported = "corpus-supported"
	GradeProven          = "proven"

	// CertificateFormatVersion is the version of THIS certificate shape.
	CertFormatVersion = 1
	// SameVersionMarker is the old_format_version marker in same-version mode.
	SameVersionMarker = "same-version"
)

// certSchemaTOML is the BUILT-IN strictspec schema for the diff certificate (the
// shape IS a strictspec schema, authored in the pinned surface — dogfooding). The
// engine self-validates every NORMAL-mode certificate it emits against this
// schema; SAME-VERSION certificates self-validate against certSchemaSameVersionTOML
// (below), so BOTH modes are dogfooded (no silent partiality).
//
// old_format_version is modeled per mode, NOT as a single-schema union: a
// node-kind-union selects arms by node CATEGORY (scalar/record/array), and both an
// integer and the "same-version" string literal are the SAME category (scalar), so
// no single union can distinguish them. The two mode-specific schemas are explicit
// mode selection, not a fallback. See appendix-certificates.md "Self-validation".
const certSchemaTOML = `
name = "DiffCertificate"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Certificate"

[types.Certificate]
type = "record"
[types.Certificate.fields.certificate_format_version]
type = "integer"
required = true
[types.Certificate.fields.schema_id]
type = "string"
required = true
[types.Certificate.fields.old_format_version]
type = "integer"
required = true
[types.Certificate.fields.new_format_version]
type = "integer"
required = true
[types.Certificate.fields.migration_set_id]
type = "string"
required = false
[types.Certificate.fields.corpus]
type = "Corpus"
required = true
[types.Certificate.fields.claims]
type = "array"
required = true
  [types.Certificate.fields.claims.item]
  type = "Claim"
[types.Certificate.fields.strictspec_release]
type = "string"
required = true

[types.Corpus]
type = "record"
[types.Corpus.fields.declared_glob]
type = "string"
required = true
[types.Corpus.fields.resolved_file_count]
type = "integer"
required = true
[types.Corpus.fields.content_hash]
type = "string"
required = true

[types.Claim]
type = "record"
[types.Claim.fields.kind]
type = "enum"
required = true
values = ["flip-scan", "migrate-round-trip-soundness", "migrate-round-trip-completeness", "down-taxonomy"]
[types.Claim.fields.grade]
type = "enum"
required = true
values = ["violated", "corpus-supported", "proven"]
[types.Claim.fields.statement]
type = "string"
required = true
[types.Claim.fields.counterexamples]
type = "array"
required = false
  [types.Claim.fields.counterexamples.item]
  type = "Counterexample"

[types.Counterexample]
type = "record"
[types.Counterexample.fields.document_path]
type = "string"
required = true
[types.Counterexample.fields.diagnostics]
type = "array"
required = true
  [types.Counterexample.fields.diagnostics.item]
  type = "CertDiag"

[types.CertDiag]
type = "record"
[types.CertDiag.fields.code]
type = "string"
required = true
[types.CertDiag.fields.path]
type = "string"
required = true
[types.CertDiag.fields.message]
type = "string"
required = true
`

// certSchemaSameVersionTOML is the SAME-VERSION built-in certificate schema. It is
// identical to certSchemaTOML except old_format_version is the string LITERAL
// "same-version" (decision 25: a same-version flip-scan carries the marker, and no
// migration_set_id exists in that mode). Same-version certificates self-validate
// against THIS schema, so the same-version shape is dogfooded too.
const certSchemaSameVersionTOML = `
name = "DiffCertificate"
meta_version = 1
format_version = 1
document_syntax = "json"
role = "schema"
root = "Certificate"

[types.Certificate]
type = "record"
[types.Certificate.fields.certificate_format_version]
type = "integer"
required = true
[types.Certificate.fields.schema_id]
type = "string"
required = true
[types.Certificate.fields.old_format_version]
type = "literal"
value = "same-version"
required = true
[types.Certificate.fields.new_format_version]
type = "integer"
required = true
[types.Certificate.fields.migration_set_id]
type = "string"
required = false
[types.Certificate.fields.corpus]
type = "Corpus"
required = true
[types.Certificate.fields.claims]
type = "array"
required = true
  [types.Certificate.fields.claims.item]
  type = "Claim"
[types.Certificate.fields.strictspec_release]
type = "string"
required = true

[types.Corpus]
type = "record"
[types.Corpus.fields.declared_glob]
type = "string"
required = true
[types.Corpus.fields.resolved_file_count]
type = "integer"
required = true
[types.Corpus.fields.content_hash]
type = "string"
required = true

[types.Claim]
type = "record"
[types.Claim.fields.kind]
type = "enum"
required = true
values = ["flip-scan", "migrate-round-trip-soundness", "migrate-round-trip-completeness", "down-taxonomy"]
[types.Claim.fields.grade]
type = "enum"
required = true
values = ["violated", "corpus-supported", "proven"]
[types.Claim.fields.statement]
type = "string"
required = true
[types.Claim.fields.counterexamples]
type = "array"
required = false
  [types.Claim.fields.counterexamples.item]
  type = "Counterexample"

[types.Counterexample]
type = "record"
[types.Counterexample.fields.document_path]
type = "string"
required = true
[types.Counterexample.fields.diagnostics]
type = "array"
required = true
  [types.Counterexample.fields.diagnostics.item]
  type = "CertDiag"

[types.CertDiag]
type = "record"
[types.CertDiag.fields.code]
type = "string"
required = true
[types.CertDiag.fields.path]
type = "string"
required = true
[types.CertDiag.fields.message]
type = "string"
required = true
`

// adjudicationSchemaTOML is the BUILT-IN strictspec schema for the adjudication
// file (appendix-certificates.md Part B). It IS a gated document (it carries
// format_version), so it self-gates like any document.
const adjudicationSchemaTOML = `
name = "Adjudication"
meta_version = 1
format_version = 1
document_syntax = "toml"
role = "schema"
root = "Adjudication"

[types.Adjudication]
type = "record"
[types.Adjudication.fields.schema_id]
type = "string"
required = true
[types.Adjudication.fields.old_format_version]
type = "integer"
required = true
[types.Adjudication.fields.new_format_version]
type = "integer"
required = true
[types.Adjudication.fields.adjudications]
type = "array"
required = true
  [types.Adjudication.fields.adjudications.item]
  type = "AdjEntry"

[types.AdjEntry]
type = "record"
[types.AdjEntry.fields.claim_kind]
type = "enum"
required = true
values = ["flip-scan", "migrate-round-trip-soundness", "migrate-round-trip-completeness", "down-taxonomy"]
[types.AdjEntry.fields.scope]
type = "string"
required = true
[types.AdjEntry.fields.justification]
type = "string"
required = true
non_empty = true
[types.AdjEntry.fields.author]
type = "string"
required = true
non_empty = true
[types.AdjEntry.fields.date]
type = "date"
required = true
`
