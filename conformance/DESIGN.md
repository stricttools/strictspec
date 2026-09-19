# conformance/ — Cross-Target Conformance Suite (dev_node)

Turns "multiple implementations, zero drift" from a hope into a guarantee. The guarantee is
exactly what consumers are allowed to rely on: ordered VERDICT + ERROR-CODE + PATH +
MESSAGE-TEXT identity across all four targets (messages render from the spec-pinned
templates), for structural validation AND the cross-document constraint vocabulary (the
constraint engine is a first-class conformance category, not a carve-out). The toolchain's own
output (certificates, structured diffs) is asserted as single-engine GOLDEN OUTPUT. Write-side
TOML fidelity is within-backend only.
`dev_node = true`: never released, no changelog. Pattern proven by strictcli's conformance/.
CI needs Go, uv/Python, and Node — no external formatters exist anywhere in the system.

## Targets — four

1. generated Python (frozen dataclasses + generated checks),
2. generated Go,
3. generated TypeScript,
4. the internal interpreter.

All four targets run structural validation AND the ported constraint engine (cross-document
vocabulary + evidence resolvers). Pairing note: the suite always runs same-release artifacts, and
holds the generated-code format contract itself — the accepted range, the refusal text, the emitted
declaration names and the emitted shape — in lockstep across the three ports.

## Fixture format

Pure data — no code per case:

- a strictspec schema (TOML),
- an input document (JSON/TOML/JSONL), stored LF-only (`.gitattributes` enforced), in one or
  both input forms: raw text, and/or a tagged-value construction script expressed as data
  (constructor calls in fixture notation) — the second exercises entry point 2,
- for cross-document constraint fixtures: the pinned EVIDENCE (resolver name -> returned
  data), so constraint verdicts are a pure function of (document, evidence) and identical
  everywhere,
- backend applicability (derived from syntax by default: ALL formats run on ALL four targets —
  TS has full TOML/JSONL parity),
- expected outcome: valid, or the exact ordered diagnostics list (code, path, and template
  slot values — emission order, phase 1 then phase 2). There is no valid-with-warnings
  outcome; warnings do not exist.

Codes, paths, AND message text asserted across targets: the harness renders the expected
message from the spec-pinned template for each expected diagnostic (fixtures carry codes,
paths, and slot values — never embedded prose). The
normative appendices (path grammar, traversal order, datetime lexeme rules, edge-input
outcomes, canonical serialization, message templates, value rendering, did-you-mean, formal
semantics, undecidability catalog, model-search order) are versioned; any
change is a breaking-class, changelog-covered entry that triggers FULL conformance-fixture
regeneration — appendix-driven regenerations are declared, never silent.

## Fixture categories

- Construct coverage: every construct and constraint form, positive and negative, at multiple
  scopes; recursion at and beyond the pinned depth limit (asserting the canonical diagnostic,
  not surviving a crash).
- Unions: discriminated (missing/invalid/valid-arm-invalid-body; non-matching arms never
  validated), node-kind unions (kind selects arm; no-kind-match diagnostic), nullable nesting;
  nullable-union-against-TOML hard error (document-level, and meta-schema rejection where
  document syntax is known).
- Numbers: integer/float/NUMBER scalars; integral floats on all targets; `1e3` classification;
  int64 and float64 overflow lexemes; NUMBER unrepresentable-lexeme rejection (large integer
  lexemes, precision-exceeding decimals — hard error on every target); 2^53 schema-wide
  rejection when the schema declares `safe_integers = true`; negative zero; bool-not-int;
  non-finite.
- Datetimes: date/time/datetime scalars; offset vs local kind declarations and mismatches;
  TOML natives; RFC 3339 strings in JSON; non-conforming strings in datetime-typed fields;
  lexeme retention (no `+00:00` -> `Z` rewriting); same-kind range constraints; the meta-schema
  rejection of cross-kind comparisons.
- Duplicate JSON keys: hard error on every backend (incl. Python object_pairs_hook path).
- Tagged-entry equivalence: the same logical document via raw text and via tagged construction
  produces identical verdicts and diagnostics (minus source positions).
- Unknown keys: the always-error invariant — an unknown key anywhere yields exact ordered
  code+path diagnostics on every backend.
- Opaque leaves: meta-schema rejection when an opaque JSON object leaf declares no stance;
  meta-schema rejection when `unchecked = true` lacks `unchecked_reason`; positive fixtures
  for `consumer_check = "<name>"` and justified `unchecked`; the `strictspec check` inventory
  of unchecked subtrees and consumer-check declarations (toolchain golden output).
- Shared type definitions (decision 21, reopened): a schema importing named types from a
  dedicated type-definition file (types only, no cross-file constraints, no transitive
  imports); positive fixtures; meta-schema rejection of a cross-file constraint reference and
  of a transitive import.
- Enum arms sourced from a document: a schema declaring enum arms sourced from a named
  document; freshness enforcement (a stale baked arm set is a hard error in gen/check — the
  sanctioned data→schema dependency edge); positive (fresh) and negative (stale) fixtures.
- Aggregate constraints (decision 23): count-limit and sum-limit forms with literal bounds
  (positive and negative, at multiple scopes).
- CROSS-DOCUMENT CONSTRAINTS (first-class category, not a dispatch carve-out): vocabulary
  forms over pinned evidence — verdict+code+path+message identity on all four targets; phase
  ordering; partial-subtree binding; reference-must-resolve, set-coverage,
  cross-collection-unique over pinned sibling-document evidence;
  RESOLVER PARITY: each evidence resolver, given identical inputs against a fixture
  environment (a fixture git repo, a fixture document tree), returns identical data on every
  target that implements it; unavailable-resolver hard errors (the browser profile asserts
  the named-resolver diagnostic, never a skip). The decision language is removed (decision
  23); consumer-native checks are outside the suite by declaration.
- Versioning: gate fixtures (integer `format_version`, exact match, three messages + the
  STRUCTURED remediation payload) on ALL targets; migration chains up;
  wrap_in_array/unwrap_singleton incl. partial-down failures; collision semantics;
  where-predicates (equality + presence); in-memory archive migration (toolchain-only,
  explicit); per-line JSONL
  gating with mixed-version streams (streaming semantics: line-by-line, memory bounded by the
  largest line, all-errors-in-one-pass per line, byte-offset positions; verdicts and
  diagnostics unchanged by streaming, per-line independence); manifest (strictspec.toml)
  meta-fixtures incl. store/channel declarations; meta-migration fixtures (schema-language
  version bumps ship declarative meta-migrations; `strictspec migrate` upgrades consumer schema
  files like any document; `meta_version` gating). Down-chains after up-chains are green.
- BOUNDARY INVARIANT: producer-current-only — the write path on every target refuses
  non-current `format_version` serialization; generated store ingest write-doors
  migrate-then-persist atomically (stale-in, current-at-rest; unmigratable-in, rejected,
  store untouched); negotiation envelope fixtures (agree-or-refuse, no fallback); egress
  migration (older negotiated version -> egress migrates, receiver gates clean); the
  browser-profile refusal ("update the client" structured diagnostic); symmetric rejection
  (old reader vs new document).
- Toolchain (CLI): `strictspec migrate` atomicity — all-or-nothing per run (transform+revalidate
  to temp, rename sweep only after all succeed; a chain that fails on file N leaves all files
  untouched, zero disk changes); `strictspec migrate --dry-run` produces zero disk changes and
  a per-file structured diff; `strictspec init` manifest skeleton + .gitattributes, hard error
  if a manifest exists.
- DIFF ENGINE (toolchain golden output; EMPIRICAL — the formal analyzer is unbundled,
  decision 25): `--corpus` flip-scan outcomes (every flipped document reported with its
  killing diagnostics); same-version flip-scan (the corpus replayed against old and new schema
  at the same format_version; a document flipping valid→invalid is an un-bumped narrowing —
  a hard error); migrate-round-trip outcomes (soundness/completeness
  counterexamples are real corpus documents); down-taxonomy verification against declarations
  (a mis-declared taxonomy is a hard error); certificate format stability (spec-pinned JSON
  shape; evidence grades `violated` / `corpus-supported`; corpus identity and size recorded);
  adjudication-file validation (it is a strictspec-schema'd document) and the gate-blocking
  behavior for `violated` and for a missing corpus without adjudication. Proof-object,
  witness-synthesis, and undecidability-catalog fixtures belong to the future analyzer
  project — the certificate format is forward-compatible with the `proven` grade.
- DOC-DIFF (toolchain golden output): per-path typed deltas; array move detection keyed by
  declared unique-by; pinned output shape stability.
- Error reporting: one-pass ordering, phase-1-then-phase-2, index-then-key paths,
  emission-order determinism, full message-text identity via the spec-pinned templates,
  did-you-mean determinism (pinned metric, threshold, tie-break) and canonical value
  rendering asserted on all four targets.
- Write side: JSON write fixpoint (read-then-write byte-identity via retained lexemes);
  constructed-value rendering table incl. datetimes; POST-MIGRATION REVALIDATION (the flagship
  migration's output must validate); TOML within-backend fixpoint (Go, Python, AND TS);
  alias canonicalization preserving comments; JSONL append/rewrite.
- Aliases (defaults are not in the language, decision 30): co-valid spellings, both-present
  error, unknown-key exemption, meta-schema rejections (a `default` key present — dedicated
  diagnostic; alias-on-discriminator; tuple/length-bounds conflict).
- Edge inputs: empty text, whitespace-only, empty TOML, JSONL blank lines and
  final-line-without-LF.
- Unicode: code-point identity (NFC vs NFD distinct), case-fold and trim normalization options,
  code-point lengths on non-ASCII.

## Harness

- Runner: generate per fixture SCHEMA (one compiled artifact per schema, many input cases —
  never a compile per fixture), execute per applicable target, diff verdicts + codes + paths.
- Parity checkers (strictcli pattern): verdict/code/path/message identity across targets;
  constraint-engine verdict identity over pinned evidence; resolver parity against fixture
  environments;
  artifact determinism (the emitters' canonical formatting is self-pinning — regenerate twice,
  byte-compare); JSON Schema export stability against the lossiness table; structured-metadata
  export stability; generated-code format pairing (accepted range, refusal text, emitted
  declarations, and the emitted-shape pin that forces a format bump).
- The `strictspec check` gate exercised here (stale code fails; pairing mismatch fails; the
  unchecked inventory renders); `strictspec gen` hard-errors when a TS target is declared for a
  schema lacking `safe_integers = true` (harness meta case); generated-file lint-suppression
  headers and prettier-ignore maintenance asserted on generated output.

FIXTURE-AUTHORING DISCIPLINE: expected fixture outcomes are HAND-AUTHORED from the specification, never
regenerated from any target — guarding against common-mode emitter-IR bugs passing all four
targets simultaneously. A bug shared by the shared emitter IR would make all four targets agree
on a wrong answer; only a spec-derived, hand-authored expectation catches it.

## The acceptance test

Source: the hand-written strictspec TOML translation of PixelWeaver's character-preview schema
(examples/ draft #2; JSON Schema is never a source; translation fidelity is part of what the
corpus checks). The `number` scalar and node-kind unions make the translation faithful.

Corpus: PixelWeaver's existing character-preview test inputs + every conformance fixture
derivable from the translated schema.

Criterion:
- Path NORMALIZATION per legacy target (pydantic loc tuples -> canonical paths; legacy TS
  dotted strings -> canonical), index-then-key switching applied to both sides.
- Verdict parity STRICT except a committed WAIVER LIST where divergence is the pass condition.
  Every waiver entry must name the specific legacy defect it covers (e.g. unenforced minItems,
  pydantic lax int coercion) AND the expected strictspec diagnostic codes — the entry asserts
  strictspec's correct behavior, not merely "ignore this divergence." The lexical-number class
  wholesale on legacy targets is waived the same way (legacy TS necessarily parsed with
  JSON.parse; legacy pydantic coerces) — strictspec's stricter verdicts on those fixtures are
  correct by definition. Once the acceptance test goes green the list is LOCKED: adding any
  entry afterward is a hard error.
- On reject: normalized legacy path SET must be a subset of strictspec's (pydantic sprays
  per-arm errors and stops at the first model_validator; strictspec reports one arm, all errors).

Timing: at MVP time, from the schema alone, BEFORE any consumer migration. Roadmap wave 1 is
then only the swap — which the tagged-value entry makes true for PixelWeaver's in-memory call
sites as well.

## Seed fixtures (imported during MVP)

- wavescript: 47 score fixtures + expectations derived from its 158-pair golden manifest
  (render-hash pairs) — the strongest conditional-required / registry-gate stress in the corpus.
- PixelWeaver: the acceptance corpus above — only documents that map to its three drafted
  schemas (constraint-manifest, part-manifest, character-preview); its project.json/history.json
  save format is OUT of scope.
- predraw: scene corpus (node-kind unions, NUMBER scalar, aliases, recursion).
