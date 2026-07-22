# strictspec — Design Charter

strictspec is a strict, multi-language schema toolchain for declarative spec files.
A project defines, once, in a TOML schema, what its spec files (JSON/TOML/JSONL) look like;
strictspec generates the code that reads, validates, and version-gates those files — in Python, Go,
and TypeScript — with read-side verdicts, error codes, paths, AND template-rendered message
text kept identical across all four conformance targets by a shared conformance suite
(write-side TOML fidelity is within-backend only). Cross-document validation is portable by
construction: a declarative constraint vocabulary over closed evidence resolvers, executed by
a constraint engine ported to every target and conformance-tested exactly like structural
validation; the genuinely bespoke tail is consumer-native code over generated typed values,
outside strictspec by declaration. Format migrations are declarative op lists, executed by the
strictspec CLI and by tool-generated version-boundary checkpoints — never receiver-side, never
automatic. The CLI is eight commands: gen, check, validate, migrate, export, init, diff,
doc-diff.

Analogy: protobuf for human- and agent-edited config files. Schema in, typed code out in many
languages, with a first-class story for format evolution.

This document was amended 2026-07-11 after a full design review; the review's decisions are
integrated below and marked where they changed a previously locked ruling. It was amended
again 2026-07-12 after a follow-up decision round (defaults removed from the language; message
text spec-pinned via templates; the decision language removed in favor of a consumer-native
bespoke tail; the formal diff analyzer unbundled into a separate future project; binary
distribution, the docs home, and the fix-forward recovery policy pinned) — those changes are
marked AMENDED 2026-07-12. It was amended again 2026-07-22: the project was renamed dclrbl →
strictspec, the schema-language version key `dclrbl_spec_version` became `meta_version`, the
error-code prefix became `STRICTSPEC_*`, and the -bin wrapper packages were eliminated in favor
of shipping the CLI inside the runtime packages — marked AMENDED 2026-07-22.

## Motivation (evidence)

A sizing audit across ten declarative projects in this ecosystem found:

- ~7,000–8,000 hand-written lines of duplicated parse/validate/error-report code, plus ~2,200
  lines of hand-maintained format documentation that drifts from the code.
- Systematic policy inconsistency: unknown-key handling ranges from hard-error to warn to
  silent-ignore, sometimes within a single project. Silent ignoring means an agent's typo'd
  field vanishes without an error — the deadliest failure mode for agent-authored files.
- Confirmed cross-language drift: two implementations of the same format accept *different
  documents* (tunebox Go accepts integral floats Python rejects), report different first
  errors, and share zero identical error strings — with no conformance tests guarding any of it.
- Format versioning is nearly absent despite demonstrated rename pain handled by hand-rolled
  compat shims.
- Two projects independently began building this tool in-house (a private project's schema-to-code
  generator; incantino's schema notation + unwired codegen) and both stalled on incompleteness.

Roughly 70% of the initial build already exists in the ecosystem as disconnected fragments
(see Donor inventory below). Honesty note on the line-count claim: the eliminated lines are
*structural* parse/validate/error code plus — with the declarative cross-document constraint
vocabulary (decision 23) — most cross-file validation logic (references, coverage,
uniqueness). The genuinely bespoke tail (and non-validation analysis like pgdesign's
normal-form work) remains consumer code, out of scope by declaration.

## Locked decisions

| # | Decision | Ruling |
|---|----------|--------|
| 1 | Name | strictspec (RENAMED 2026-07-22 from dclrbl; verified available on PyPI, npm, and GitHub — no repos of that name anywhere — 2026-07-22; name approved). There are NO separate binary wrapper packages: the former -bin wrappers are eliminated (decision 31) — the CLI ships inside the runtime packages, so strictspec is the only published name on every registry |
| 2 | License | MIT, plus an explicit statement: code the generator emits into consumer repos is unencumbered — no license obligation attaches to generated output |
| 3 | Scope | These documents design the COMPLETE software; there is no "v0" scope boundary. The construct set is bounded to the analyzed corpus, and the corpus MUST include paper schemas for demobl, F, and step (absent from the original sizing audit) before the construct set freezes — the sufficiency claim has to be true, not assumed. Custom scalar registration is part of the language design. Build sequencing (acceptance test first, then the rest) is an implementation concern, never a design boundary |
| 4 | Enforcement | Codegen-primary. The internal interpreter is a FOURTH CONFORMANCE TARGET. `strictspec validate` requires an explicit mode: `--structural-only` or `--with-domain-checks`. No default. AMENDED: because domain checks are portable (decision 23), the CLI hosts them natively — `--with-domain-checks` runs the constraint engine (the cross-document vocabulary) with the toolchain's evidence resolvers; a resolver the current environment cannot satisfy is a hard error, never a skip |
| 5 | Backends | Python + Go + TypeScript generated code. AMENDED: TS has FULL FORMAT PARITY — lossless, lexeme-retaining TOML and JSONL alongside JSON — so four-target identity holds for every format |
| 6 | Toolchain language | Go. One static binary hosts generator, interpreter, migration engine, constraint engine, diff engine, CLI. Built on go-toml-edit; absorbs migrable's engine code (minus CEL) and pgdesign's diagnostics/coercers. a private project's emitters ported as templates. AMENDED: migrable is RETIRED upon absorption — strictspec is the ecosystem's only migration engine. Value-computing migrations are deliberately not offered; a consumer needing one writes a one-off conversion script plus a normal declarative migration for the structural part. VERIFIED 2026-07-12: no consumer uses the dropped ops — transform/raw/merge_defaults_by_key were exercised only by migrable's own tests and docs; migrable's sole API consumer (howmuchleft, roadmap step 3) uses only surviving ops |
| 7 | Emitter architecture | One architecture across all three languages: typed immutable shells + explicit generated checks + generated `with_*` copy helpers for mutate-then-revalidate flows. Python: frozen dataclasses, zero third-party runtime deps (pydantic and msgspec out — partial-subtree binding). Generated output lands chmod 444 in consumer repos (selfdoc convention); `gen` and the batch helper chmod before overwrite. AMENDED: generated code is formatted by strictspec's OWN canonical, deterministic emitters — no external formatters (see decision 18) — and every generated file carries the target ecosystem's machine-readable generated-file markers and lint-suppression headers (Go: `// Code generated by strictspec. DO NOT EDIT.`; Python: `# ruff: noqa` header; TS: `/* eslint-disable */` header). Where a tool supports only repo-level ignores (prettier), `strictspec gen` maintains the ignore entries for the manifest-declared generated paths. Consumers never hand-silence linters on generated files |
| 8 | Entry points | TWO per language, defined by the normative Generated API Contract: (1) raw text/bytes through the backend's lossless parser; (2) strictspec's own TAGGED document-model values (produced by the lossless parser or by generated typed constructors). Raw untagged objects/dicts are banned as input — ambiguity never enters the model. This serves in-memory mutate-then-validate consumers (a private project state layer, orxtra) without re-serialization |
| 9 | Migrations | Native engine inside the toolchain. CLOSED op vocabulary (13 ops incl. wrap_in_array / unwrap_singleton) with a one-sentence admission criterion: ops may move, rename, reshape, delete, and inject literal values — never compute a value from a value; predicates test equality and presence only. Down-ops declare `down`, `partial` (per-document reversibility, failures are canonical hard errors), or `irreversible`. AMENDED execution locus: the CLI plus TOOL-GENERATED VERSION-BOUNDARY CHECKPOINTS (decision 24) — never receiver-side, never automatic; generated read paths ship only the inline version gate. `strictspec migrate` is ALL-OR-NOTHING per run: every file transforms and revalidates to a temp copy first, and only after ALL succeed does the rename sweep run — any failure anywhere leaves zero changes on disk. `strictspec migrate --dry-run` renders a per-file structured diff of what would change, with zero disk writes. Each schema-language version bump ships declarative meta-migrations INSIDE the toolchain, so `strictspec migrate` upgrades consumer schema files exactly like any other document |
| 10 | Repo shape | strictcli-style monorepo, rlsbl-monorepo-managed. Go: ONE module (`github.com/smm-h/strictspec`), `cmd/strictspec` + runtime subpackage. AMENDED: the CLI is BUILT ON STRICTCLI (Go) — flag conventions enforced at registration, schema dump integrated; strictspec does not hand-roll its own CLI layer |
| 11 | Schema syntax | TOML canonical; JSON Schema export-only. Generation is FILE-DRIVEN via a committed `strictspec.toml` manifest — itself a document of a toolchain-shipped built-in schema, versioned and migrated like any document. `strictspec init` writes a commented `strictspec.toml` skeleton plus the mandated `.gitattributes` LF rules, hard-erroring if a manifest already exists |
| 12 | Unknown keys | ALWAYS a canonical hard error — a language invariant, nothing to declare. Extension zones use the opaque JSON leaf. AMENDED: warnings are REMOVED from the language entirely — the diagnostic model has no severity field, every diagnostic is an error, and there is no valid-with-warnings outcome anywhere, including domain checks. Agents ignore warnings; strictspec does not emit any |
| 13 | Versioning | AMENDED naming: every document carries integer `format_version`; the gate accepts exactly the schema's declared `format_version` — the SAME token in document and schema, so the pairing is visible in the text. Schema files carry `meta_version` (RENAMED 2026-07-22 from `dclrbl_spec_version`) (the schema-language version — schemas are documents of the meta-schema) and `format_version` (the value their documents must carry). Absent/wrong-type/unsupported = hard error with a STRUCTURED payload (got, expected, schema id, migration-set id, exact remediation invocation). JSONL: per-line; readers in all backends and the CLI stream line-by-line, memory bounded by the largest line, and all-errors-in-one-pass applies per line with byte-offset positions. Pre-versioning bootstrap: per-consumer one-time conversion scripts (stamp, never reshape, refuse ambiguity) |
| 14 | Numbers | Scalars: integer, float, and NUMBER (accepts both lexeme classes, binds float64, rendering preserves lexeme). AMENDED: a `number` field HARD-ERRORS on any lexeme whose exact value float64 cannot represent — silent precision loss never happens, in any backend, ever. Lexical strictness unchanged where schemas say integer/float. JSON duplicate keys = hard error in every backend. Large integers: a schema opts into the 2^53 safe-integer constraint by declaring `safe_integers = true` (schema-wide, enforced in ALL backends when declared); declaring a TS target for a schema that lacks the declaration is a hard error at `strictspec gen` time; TS binds plain `number`; no BigInt anywhere |
| 15 | Unions | Discriminated unions (literal-valued field) plus BOUNDED NODE-KIND UNIONS: undiscriminated only when arms differ by node kind (scalar vs record vs array) — covers predraw's fill-or-gradient; same-kind arms require a discriminator |
| 16 | Error output | AMENDED 2026-07-12: conformance guarantee upgraded to VERDICT + CODE + PATH + MESSAGE-TEXT identity (ordered) across all four targets. Every `STRICTSPEC_*` code has a SPEC-PINNED MESSAGE TEMPLATE (fixed prose plus typed slots — path, expected, got, suggestion — rendered per the canonical value-rendering table); generators emit each runtime's renderer FROM the templates, so an agent reads the identical message no matter which target validated. Did-you-mean is deterministic (pinned metric, threshold, tie-break) and asserted. The former per-target best-effort stance is REVERSED — agents are the primary consumers and they read messages, not codes; the zero-identical-error-strings defect cited in Motivation is fixed, not tolerated. Message wording changes are versioned appendix changes (breaking-class, changelog-covered, full conformance-fixture regeneration) — codes remain permanent (never renamed or reused) and consumers may still assert them. All-errors-in-one-pass (per phase) unchanged. Path grammar and traversal order remain normative cross-target appendices. The normative appendices are VERSIONED; appendix-driven behavior changes are always declared, never silent. Consumer-prefixed codes emitted by consumer-native checks are outside this surface (their templates are consumer-owned) |
| 17 | Write path | PRESERVE SOURCE LEXEMES: the document model retains each value's lexeme; values untouched by an op serialize byte-identically WITHIN A BACKEND (write-side TOML fidelity is within-backend only — Go and Python emit different bytes for the same migrated TOML); constructed values render per a pinned table. Migration output revalidates by construction; a normative canonical-serialization appendix mirrors the read-side one. AMENDED (decision 24): the write path REFUSES to serialize a document at any format_version other than the schema's current one — no conforming producer can create new staleness |
| 18 | Drift gate | AMENDED: `strictspec check` = regenerate from the manifest, byte-compare against strictspec's OWN canonical emitter output. There are NO external formatters anywhere — no pinned ruff/prettier, no version treadmill, no repo-config discovery problem; the emitters are the formatting authority and their output is pinned by the canonical-emission rules. `check` also prints the complete blind-spot inventory — unchecked leaves and consumer-check declarations (decision 29) — and hard-errors on runtime/codegen pairing mismatch. Consumers regenerate-and-commit on releases; the batch-regeneration helper across consumer repos is a REQUIRED DELIVERABLE — per repo it requires only the manifest-declared generated paths to be clean/unmodified (other working-tree dirt from concurrent sessions is irrelevant), scopes its follow-up commit to exactly those generated paths, commits via `rlsbl commit` (never reimplementing the Autogenerated-trailer flow), and NEVER pushes |
| 19 | Version pairing | Generated code and runtime from the SAME strictspec release — exact match, hard error on mismatch. Dev builds carry a dev version string that pairs only with itself. CLARIFIED: under the ecosystem's always-latest dependency rule, a runtime floated past its generated code hard-errors immediately — that error IS the intended surfacing mechanism; remediation is regeneration (the batch helper), never pinning. AMENDED 2026-07-12: recovery from a defective strictspec release (e.g. an emitter bug regenerated into consumers) is FIX-FORWARD ONLY — patch release plus batch regeneration; no rollback machinery exists, and pinning around a bad release is banned (the ecosystem fix-forward rule) AMENDED 2026-07-22: for non-Go consumers the pairing holds by construction — the CLI stub ships inside the runtime package (decision 31), so runtime, generated code, and downloaded binary all key off the single package version |
| 20 | Concurrency | JSONL append is SINGLE-WRITER by declaration: one O_APPEND write per complete line; rewrites are temp+rename (reader-safe). Cross-process coordination is the consumer's job |
| 21 | Schema sharing | Single-file schemas, no imports/composition. claudestream and toolstream keep two separate schemas |
| 22 | Ecosystem bugs found during sizing | AMENDED: FILED AS TODOS in each affected project (standalone descriptions, no cross-project coupling), so they survive any slip in this project's schedule. Migrations still fix them structurally where applicable |
| 23 | Domain checks (NEW) | AMENDED 2026-07-12: TWO strictspec-owned layers, not three — the DECISION LANGUAGE IS REMOVED. (1) EVIDENCE RESOLVERS — the only IO surface: a CLOSED, named vocabulary implemented once by the toolchain and runtimes; resolvers return DATA, never verdicts; resolver parity is conformance-tested (identical evidence inputs yield identical outputs across targets); a resolver the environment cannot satisfy (git in a browser) is a hard error at check-execution time, never a skip; extending the vocabulary is a versioned, changelog-covered language change. (2) THE DECLARATIVE CONSTRAINT VOCABULARY — the finite cross-field and cross-document forms, implemented once in the shared emitter IR, executed by a CONSTRAINT ENGINE ported to all four targets and conformance-tested for verdict+code+path+message identity exactly like phase 1. The genuinely bespoke tail is CONSUMER-NATIVE CODE over generated typed values: invoked by the consumer after validation, emitting diagnostics with consumer-prefixed codes via the runtime's constructor — outside strictspec and outside the conformance guarantee, BY DECLARATION (strictspec still has no registration surface, no plugin API, no embedded expression language; consumer checks are not plugged in, they simply run downstream). Rationale: the vocabulary covers the portable majority (references, coverage, uniqueness); a bespoke expression DSL hand-ported to four targets was a perpetual drift surface serving only the tail that is bespoke anyway. CEL-class open computation remains expressly rejected — there is nothing left to embed it in; a recurring check shape across consumers is a vocabulary-evolution conversation, not an escape hatch |
| 24 | Version-boundary invariant (NEW) | A document only ever exists at the current format_version within any boundary it inhabits; every boundary crossing is a tool-generated migrate-to-current checkpoint. Producers: the write path refuses non-current serialization (decision 17). Stores: manifest-declared stores get GENERATED INGEST WRITE-DOORS that migrate-then-persist atomically — nothing stale exists at rest, by invariant. Live channels: a normative version-negotiation envelope; the channel agrees on one version or refuses to open; the EGRESS side migrates before sending. Receivers only gate — receiver-side migration does not exist. Browser runtimes never migrate: they receive current bytes or refuse with a structured "update the client" diagnostic. This AMENDS the former "migration is CLI-only" ruling: same intent (canonical documents at rest, no runtime migration sprawl), stronger mechanism (checkpoints are tool-generated, explicit, single-target, hard-failing — never automatic, never receiver-side). rlsbl integration: format_version bumps become deploy-gated release events (see decision 25) |
| 25 | diff (NEW) | AMENDED 2026-07-12: the PROOF-CARRYING ANALYZER IS UNBUNDLED into a separate future project; strictspec ships the EMPIRICAL engine. `strictspec diff` (inputs: a schema at two format versions, the migration between them, and a REQUIRED `--corpus <glob>` of real documents) runs FLIP-SCAN (every corpus document validated at N and N+1; every flip reported with the document and its killing diagnostics — a flipped document is a real witness, no synthesis needed), MIGRATE-ROUND-TRIP (soundness: M(d) revalidates at N+1 for every corpus d valid at N; completeness: M never errors on a valid-at-N corpus document — failures are counterexample documents, enabling red-green migration authoring), and DOWN-TAXONOMY VERIFICATION (declared down/partial/irreversible exercised against the corpus; a mis-declared taxonomy is a hard error). It emits a CERTIFICATE in a spec-pinned JSON format whose claims carry an EVIDENCE GRADE: `violated` (a corpus document is the counterexample — a proof) or `corpus-supported` (no counterexample in the declared corpus — explicitly NOT a proof; corpus identity and size are recorded). The `proven` grade — machine-checkable proof objects, witness synthesis, the undecidability catalog — is RESERVED for the future analyzer, which emits the SAME certificate format. The formal-semantics appendix, undecidability catalog, and model-search order are still WRITTEN NOW as normative, versioned spec/ sections (every construct added to the language MUST extend the semantics appendix — a construct without semantics cannot ship); only the analyzer implementing them is deferred. GATE INTEGRATION: the certificate is the required input to rlsbl's format_version deploy gate; `violated` BLOCKS release; the green light is `corpus-supported` over the consumer's declared at-rest corpus; a consumer with no corpus discharges via a committed ADJUDICATION FILE (a strictspec-schema'd TOML document with a signed manual justification). There is no bypass |
| 26 | doc-diff (NEW) | Schema-aware document-to-document semantic diff: given one schema and two documents, a structured per-path delta (added/removed/changed with typed values; array element moves keyed by declared unique-by). Pinned output shape; golden-output conformance |
| 27 | Docs (NEW) | First-class selfdoc integration: `strictspec export` emits JSON Schema (per the lossiness table) plus STRUCTURED SCHEMA METADATA; selfdoc owns rendering docs pages from that metadata. strictspec generates no docs pages itself — one docs system in the ecosystem. AMENDED 2026-07-12: the strictspec LANGUAGE REFERENCE has the same home — the spec/ constitution (authoring guide, constraint vocabulary, op set, appendices) is rendered by selfdoc as strictspec's own docs site, versioned with the spec; the published site is the canonical citable manual |
| 28 | YAML (NEW) | Excluded, FINAL. YAML's anchors, implicit typing, and ambiguous scalar forms are hostile to lexeme retention and byte-stable writes. Consumers convert once, at migration time; conversion is consumer-owned |
| 29 | unchecked (NEW) | Every `unchecked = true` opaque leaf requires a mandatory `unchecked_reason` string — omission fails the meta-schema. `strictspec check` prints the complete inventory of unchecked subtrees with their reasons, so the language's only sanctioned blind spot is always visible in review. AMENDED 2026-07-12: the stance key for consumer-validated blobs is `consumer_check = "<name>"` (the named check is consumer-native code, never executed by strictspec — see decision 23), and the inventory prints BOTH unchecked subtrees and consumer-check declarations: every strictspec-blind subtree is visible in review |
| 30 | Defaults (NEW 2026-07-12) | REMOVED from the language. A field is required or optional; an absent optional field binds as ABSENT — there is no bind-time injection and no `default` construct (a `default` key in a schema fails the meta-schema with a dedicated diagnostic). Consumers handle absence in code, visibly. Format evolution still injects literals — explicitly, once, at migration time (add_field / merge_defaults). This deletes the one construct that contradicted the guiding principle: a typed value never carries data the author didn't write |
| 31 | Binary distribution (NEW 2026-07-12) | AMENDED 2026-07-22: wrapper packages are ELIMINATED — one published name per registry. The CLI ships inside the runtime packages: the PyPI strictspec wheel (stays `py3-none-any`) exposes a `strictspec` console script that lazy-downloads the EXACT-version binary from the GitHub Release into the package's `_bin/` on first CLI invocation; the npm strictspec package carries a stub bin that downloads on first run — never at postinstall, so library-only installs perform no network access. Go consumers install the module directly (the module is the binary). goreleaser cross-compiled per-OS/arch archives on the GitHub Release remain the single binary source of truth. Runtime package version = strictspec release version, so lazy download and the exact version-pairing rule (decision 19) agree by construction; consumer CI simply runs the packaged CLI (`uv run strictspec` / `npx strictspec`) and gets the repo's exact paired version. A failed download is a hard error with manual-install remediation, never a silent fallback. Consequence, accepted: a non-Go consumer's first CLI run per machine needs GitHub network access (cached thereafter) |

Guiding principle throughout: strictspec is pristine and clean; the burden is on consumers to adapt.
No escape hatches, no lenient modes, no warnings, no implicit defaults.

## Architecture (directory map)

- `spec/` — the constitution: schema language, constraint vocabulary, error model, primitives
  appendix (read side), canonical-serialization appendix (write side), message-template
  appendix, generated API contract, union diagnostics, versioning and migration rules, the
  version-boundary invariant, the domain-check architecture (evidence resolvers + constraint
  vocabulary), the accepted-set formal semantics and undecidability catalog (written now,
  implemented only by the unbundled future analyzer), meta-schema. Language-neutral. Rendered
  by selfdoc as strictspec's published language reference (decision 27).
- `go/` — THE TOOLCHAIN and the Go releasable: `cmd/strictspec` (strictcli-based) + the runtime
  subpackage (diagnostics, coercers, ordered lexeme-retaining document I/O, constraint engine).
- `python/` — Python runtime library and PyPI releasable (document I/O, diagnostics, tagged
  values, constraint engine) for generated frozen-dataclass code.
- `ts/` — TypeScript runtime library and npm releasable: diagnostic types plus lossless
  JSON, TOML, and JSONL parsers producing tagged document-model values, and the constraint
  engine.
- `conformance/` — dev_node. Shared fixtures + runner + parity checkers over FOUR targets,
  including the constraint engine and evidence-resolver parity.
- `examples/` — paper schemas drafted before implementation; claudestream and a private project
  first; demobl, F, and step are mandatory before the construct set freezes.

## Distribution

- Registries: PyPI `strictspec` (Python runtime + CLI stub), npm `strictspec` (TS runtime + CLI stub), Go module `github.com/smm-h/strictspec` (toolchain + Go runtime). One name per registry; there are no separate binary packages.
- The Go binary reaches non-Go consumers through the runtime packages themselves (decision 31): goreleaser archives on the GitHub Release are the single binary source; the packaged CLI stub lazy-downloads the exact-version binary on first run. Runtime package version equals the strictspec release version, satisfying exact version pairing (decision 19) by construction.

## Donor inventory

| Donor | What it contributes | Where it lands |
|---|---|---|
| go-toml-edit | In-house comment-preserving TOML AST — document-model substrate | go/ toolchain + runtime |
| migrable | Migration engine code absorbed (Go), minus CEL; migrable is RETIRED on absorption. Dropped: `transform`, `raw`, `merge_defaults_by_key`. Added: `wrap_in_array`, `unwrap_singleton`. Verified 2026-07-12: the dropped ops are exercised only by migrable's own tests and docs — no consumer uses them; migrable's sole API consumer (howmuchleft) is roadmap step 3 | go/ migration engine |
| pgdesign `pkg/diagnostic` | Diagnostic model; Table/Column generalized to Path | go/ runtime; spec/ |
| pgdesign `parse.go:2036-2130` | Six node coercers, extended per scalar rules | go/ runtime |
| a private project generator | Emitter design + templates (TS ~301 lines, typed-model ~120, cross-field reworked to any-scope); Python target retargeted to frozen dataclasses | go/ generator |
| incantino schema notation | Header + per-property notation, battle-tested across 64 schemas (compatibleSince = documentation only) | spec/ |
| strictcli | The CLI framework itself (decision 10) AND the dual-implementation parity harness pattern | go/ CLI; conformance/ |
| wakethemup version gate | Three-message pattern, extended with the structured remediation payload | spec/, generated gate |
| claudestream budget change | Flagship shape migration: rename_field + wrap_in_array (down: partial) | spec/, examples/ |
| rlsbl dev_node rename chain | Pure-rename migration example | examples/ |
| tunebox parallel tests | ~33 mirrored Py/Go failure cases -> shared fixtures | conformance/ |
| toolstream `test_schema.py` | ~26 type-mapping cases | conformance/ |
| predraw `scene.schema.json` | 750-line corpus bounding the construct set (source of the `number` scalar and node-kind unions) | spec/, examples/ |
| tomlkit | Comment-preserving TOML for the PYTHON RUNTIME only; round-trip conformance within-backend | python/ |

## Migration roadmap (dependency-ordered)

Adoption ticket for every pre-versioning consumer: a one-time conversion script (per the
bootstrap contract) stamping `format_version` into existing files before strictspec reads them.

1. wakethemup — smallest surface, strictest discipline, only enforced version gate. Proving ground.
2. tunebox — dual Python+Go; kills confirmed drift; tests seed the conformance suite. Needs git init.
3. howmuchleft — migrable's sole API consumer (engine.Merge/engine.Migrate at startup); swaps
   to strictspec generated code + runtime, unblocking migrable's retirement (decision 6). Its
   startup config migration becomes an explicit tool-generated store checkpoint
   (manifest-declared store, decision 24): migrate-then-persist at the door — never silent
   receiver-side migration.
4. claudestream + toolstream — two separate agent schemas; toolstream gains validation;
   claudestream's budget hack becomes the flagship declared migration.
5. predraw — JSON Schema hand-translated to strictspec TOML (number scalar + node-kind unions make
   it faithful); validation moves into the load path; 8 alias pairs become declarations.
6. a private project — deletes its 811-line generator. The acceptance test (normalized paths +
   waiver list, see conformance/) runs at MVP time; every waiver entry names the specific legacy
   defect it covers AND the expected strictspec diagnostic codes, and once the acceptance test goes
   green the list is FROZEN — adding entries afterward is a hard error. The tagged-value entries
   serve its in-memory validation call sites, so step 6 is genuinely only the swap.
7. rlsbl — largest consumer; conversion script stamps per-line `format_version` into 459
   finalized files (unlock-stamp-relock); four known validation bugs eliminated structurally.
   rlsbl also gains the format_version deploy gate consuming diff certificates (decision 25).
8. pgdesign — sequenced after custom scalar registration lands (a build dependency, not a
   design boundary); its 2,100-line walker collapses to ~350-line schema, domain analysis
   untouched. Remains a donor (diagnostics + coercers) from day one.
9. orxtra — ~580 lines deleted; whitelist-drift bug class becomes impossible.
10. incantino — YAML to JSON conversion (one-time, consumer-owned; decision 28), then four
    drifting validators replaced (~2,600 lines). Swift/Kotlin backends remain outside this
    design.

Design-phase prerequisites (not migrations): paper schemas for demobl, F, and step
(examples/ drafts 11–13) before the construct set freezes.

## Defects found during sizing

Filed as todos in each affected project (2026-07-11), described standalone per todo
confidentiality. Where a migration to strictspec fixes one structurally, the migration is the fix;
the todos exist so the bugs survive any change of plan here.

## Pre-scaffolding verification

Before `rlsbl monorepo init`: confirm rlsbl's monorepo release flow can produce the tag shape
the Go module requires. Adapt the layout before locking it if not. The CLI's strictcli
dependency (decision 10) follows the ecosystem's always-latest rule.

## Status

Design phase. Next steps, in order: redraft spec/ in full (primitives appendix,
canonical-serialization appendix, the message-template appendix, generated API contract,
union diagnostics, the closed op set — including the pinned `merge_defaults` semantics — the
domain-check architecture, the version-boundary invariant, the accepted-set formal semantics
with the undecidability catalog and model-search order (written now, implemented only by the
unbundled future analyzer; decision 25), and the negotiation envelope are mandatory sections);
draft examples/ — claudestream and a private project first, then demobl/F/step before construct-set
freeze; verify the Go tagging question; then scaffold and build toward the acceptance test.
