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
marked AMENDED 2026-07-12. It was amended again 2026-07-22: the project was renamed to
strictspec (from its former working name), the schema-language version key became `meta_version`,
the error-code prefix became `STRICTSPEC_*`, and the -bin wrapper packages were eliminated in
favor of shipping the CLI inside the runtime packages — marked AMENDED 2026-07-22. It was
amended again 2026-07-27 after a long decision round: the freeze regime, the TS TOML parser
choice, migrable-retirement decoupling, the Go module path, a normative format_version bump
rule, reopened schema sharing, aggregate constraints, same-version flip-scan, the launcher
distribution mechanism, the corpus/roadmap/donor rewrite, and several new rulings were written
in — marked AMENDED 2026-07-27. It was amended again 2026-07-28: the FREEZE FRAMING WAS RETIRED —
the construct set, constraint vocabulary, migration op set, and error-code catalogue are STABLE
BUT GROWING in the growth phase, not frozen; additions and amendments are normal and expected,
still going through the existing discipline, and released-surface compatibility is governed by
semver at release boundaries — marked AMENDED 2026-07-28. Trust-adopted rulings (the `%%`
provenance convention) carry a [%%] tag; deliberate owner picks carry none.

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
- Two projects independently began building this tool in-house (PixelWeaver's schema-to-code
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
| 1 | Name | strictspec (renamed 2026-07-22 from the project's former working name; verified available on PyPI, npm, and GitHub — no repos of that name anywhere — and re-confirmed by the owner 2026-07-27; name approved). There are NO separate binary wrapper packages: the former -bin wrappers are eliminated (decision 31) — the CLI ships inside the runtime packages, so strictspec is the only published name on every registry |
| 2 | License | MIT, plus an explicit statement: code the generator emits into consumer repos is unencumbered — no license obligation attaches to generated output |
| 3 | Scope & freeze | These documents design the COMPLETE software; there is no "v0" scope boundary. The construct set is bounded to the analyzed corpus. AMENDED 2026-07-27 [%%]: the freeze regime is SOFT until the first release — no NEW construct enters without the examples/ gap-note process, but implementation-driven amendments (semantics corrections, error-code catalogue growth) before the first release are normal and recorded. The binding freeze IS the first release. Custom scalar registration is part of the language design. Build sequencing (acceptance test first, then the rest) is an implementation concern, never a design boundary. This SUPERSEDES the former demobl/F/step paper-schema precondition — those donors left the corpus; the construct-freeze gate is now all examples/ drafts coming back clean plus resolved gap notes. AMENDED 2026-07-28 (owner decision, deliberate): the FREEZE FRAMING IS RETIRED. strictspec is in its GROWTH PHASE — the construct set, the constraint vocabulary, the migration op set, and the error-code catalogue are STABLE BUT GROWING; additions and amendments are NORMAL and EXPECTED. The PROCESS is unchanged (gap-note process for new constructs, a semantics-appendix entry required before a construct ships, dated amendment logs, recorded rationale, red-green fixtures) — only the freeze ceremony and freeze vocabulary are removed. Single-consumer demand is still weighed against generality and the existing rejection rationales stand as recorded history, but "it would break the freeze" is no longer a valid argument. Released-surface compatibility is governed by semver at release boundaries, not by a freeze. This SUPERSEDES the "soft-freeze until first release / binding freeze IS the first release" framing earlier in this cell |
| 4 | Enforcement | Codegen-primary. The internal interpreter is a FOURTH CONFORMANCE TARGET. `strictspec validate` requires an explicit mode: `--structural-only` or `--with-domain-checks`. No default. AMENDED: because domain checks are portable (decision 23), the CLI hosts them natively — `--with-domain-checks` runs the constraint engine (the cross-document vocabulary) with the toolchain's evidence resolvers; a resolver the current environment cannot satisfy is a hard error, never a skip |
| 5 | Backends | Python + Go + TypeScript generated code. TS has FULL FORMAT PARITY — lossless, lexeme-retaining TOML and JSONL alongside JSON — so four-target identity holds for every format. AMENDED 2026-07-27 [%%]: lossless TOML in TS is built on the `toml-eslint-parser` library (AST ranges + text-splicing, proven by strictcli's TS implementation), NOT a from-scratch parser — the earlier "no suitable lossless library exists" claim was falsified in-ecosystem. CONFIRMED by the validation spike in conformance/spikes/toml-eslint-parser/ (24 tests passing; VERDICT: CONFIRMS; sole caveat: range-based splicing only, never `node.number`) |
| 6 | Toolchain language | Go. One static binary hosts generator, interpreter, migration engine, constraint engine, diff engine, CLI. Built on go-toml-edit; absorbs migrable's engine code (minus CEL) and pgdesign's diagnostics/coercers. PixelWeaver's emitters ported as templates. Value-computing migrations are deliberately not offered; a consumer needing one writes a one-off conversion script plus a normal declarative migration for the structural part. VERIFIED 2026-07-12: no consumer uses the dropped ops — transform/raw/merge_defaults_by_key were exercised only by migrable's own tests and docs. AMENDED 2026-07-27: strictspec absorbs migrable's engine as before, but migrable-THE-PROJECT's retirement is DECOUPLED from this plan — its one remaining consumer is outside this corpus, so retiring migrable is tracked separately and gates nothing here |
| 7 | Emitter architecture | One architecture across all three languages: typed immutable shells + explicit generated checks + generated `with_*` copy helpers for mutate-then-revalidate flows. Python: frozen dataclasses, zero third-party runtime deps (pydantic and msgspec out — partial-subtree binding). Generated output lands chmod 444 in consumer repos (selfdoc convention); `gen` and the batch helper chmod before overwrite. AMENDED: generated code is formatted by strictspec's OWN canonical, deterministic emitters — no external formatters (see decision 18) — and every generated file carries the target ecosystem's machine-readable generated-file markers and lint-suppression headers (Go: `// Code generated by strictspec. DO NOT EDIT.`; Python: `# ruff: noqa` header; TS: `/* eslint-disable */` header). Where a tool supports only repo-level ignores (prettier), `strictspec gen` maintains the ignore entries for the manifest-declared generated paths. Consumers never hand-silence linters on generated files |
| 8 | Entry points | TWO per language, defined by the normative Generated API Contract: (1) raw text/bytes through the backend's lossless parser; (2) strictspec's own TAGGED document-model values (produced by the lossless parser or by generated typed constructors). Raw untagged objects/dicts are banned as input — ambiguity never enters the model. This serves in-memory mutate-then-validate consumers (PixelWeaver state layer, orxtra) without re-serialization |
| 9 | Migrations | Native engine inside the toolchain. CLOSED op vocabulary (13 ops incl. wrap_in_array / unwrap_singleton) with a one-sentence admission criterion: ops may move, rename, reshape, delete, and inject literal values — never compute a value from a value; predicates test equality and presence only. Down-ops declare `down`, `partial` (per-document reversibility, failures are canonical hard errors), or `irreversible`. AMENDED execution locus: the CLI plus TOOL-GENERATED VERSION-BOUNDARY CHECKPOINTS (decision 24) — never receiver-side, never automatic; generated read paths ship only the inline version gate. `strictspec migrate` is ALL-OR-NOTHING per run: every file transforms and revalidates to a temp copy first, and only after ALL succeed does the rename sweep run — any failure anywhere leaves zero changes on disk. `strictspec migrate --dry-run` renders a per-file structured diff of what would change, with zero disk writes. Each schema-language version bump ships declarative meta-migrations INSIDE the toolchain, so `strictspec migrate` upgrades consumer schema files exactly like any other document |
| 10 | Repo shape | strictcli-style monorepo, rlsbl-monorepo-managed. Go: ONE module with `go.mod` in go/, module path `github.com/smm-h/strictspec/go` (AMENDED 2026-07-27, user-confirmed); `cmd/strictspec` + runtime subpackage. rlsbl emits `go/vX.Y.Z` companion tags alongside the package tag — the strictcli-proven pattern. AMENDED: the CLI is BUILT ON STRICTCLI (Go) — flag conventions enforced at registration, schema dump integrated; strictspec does not hand-roll its own CLI layer |
| 11 | Schema syntax | TOML canonical; JSON Schema export-only. Generation is FILE-DRIVEN via a committed `strictspec.toml` manifest — itself a document of a toolchain-shipped built-in schema, versioned and migrated like any document. `strictspec init` writes a commented `strictspec.toml` skeleton plus the mandated `.gitattributes` LF rules, hard-erroring if a manifest already exists |
| 12 | Unknown keys | ALWAYS a canonical hard error — a language invariant, nothing to declare. Extension zones use the opaque JSON leaf. AMENDED: warnings are REMOVED from the language entirely — the diagnostic model has no severity field, every diagnostic is an error, and there is no valid-with-warnings outcome anywhere, including domain checks. Agents ignore warnings; strictspec does not emit any |
| 13 | Versioning | AMENDED naming: every document carries integer `format_version`; the gate accepts exactly the schema's declared `format_version` — the SAME token in document and schema, so the pairing is visible in the text. Schema files carry `meta_version` (renamed 2026-07-22 from the former schema-language version key) (the schema-language version — schemas are documents of the meta-schema) and `format_version` (the value their documents must carry). Absent/wrong-type/unsupported = hard error with a STRUCTURED payload (got, expected, schema id, migration-set id, exact remediation invocation). JSONL: per-line; readers in all backends and the CLI stream line-by-line, memory bounded by the largest line, and all-errors-in-one-pass applies per line with byte-offset positions. Pre-versioning bootstrap: per-consumer one-time conversion scripts (stamp, never reshape, refuse ambiguity). AMENDED 2026-07-27 (normative bump rule): any schema edit that SHRINKS the accepted-document set obligates a `format_version` bump; widening edits (accepting strictly more) do not |
| 14 | Numbers | Scalars: integer, float, and NUMBER (accepts both lexeme classes, binds float64, rendering preserves lexeme). AMENDED: a `number` field HARD-ERRORS on any lexeme whose exact value float64 cannot represent — silent precision loss never happens, in any backend, ever. Lexical strictness unchanged where schemas say integer/float. JSON duplicate keys = hard error in every backend. Large integers: a schema opts into the 2^53 safe-integer constraint by declaring `safe_integers = true` (schema-wide, enforced in ALL backends when declared); declaring a TS target for a schema that lacks the declaration is a hard error at `strictspec gen` time; TS binds plain `number`; no BigInt anywhere |
| 15 | Unions | Discriminated unions (literal-valued field) plus BOUNDED NODE-KIND UNIONS: undiscriminated only when arms differ by node kind (scalar vs record vs array) — covers predraw's fill-or-gradient; same-kind arms require a discriminator |
| 16 | Error output | AMENDED 2026-07-12: conformance guarantee upgraded to VERDICT + CODE + PATH + MESSAGE-TEXT identity (ordered) across all four targets. Every `STRICTSPEC_*` code has a SPEC-PINNED MESSAGE TEMPLATE (fixed prose plus typed slots — path, expected, got, suggestion — rendered per the canonical value-rendering table); generators emit each runtime's renderer FROM the templates, so an agent reads the identical message no matter which target validated. Did-you-mean is deterministic (pinned metric, threshold, tie-break) and asserted. The former per-target best-effort stance is REVERSED — agents are the primary consumers and they read messages, not codes; the zero-identical-error-strings defect cited in Motivation is fixed, not tolerated. Message wording changes are versioned appendix changes (breaking-class, changelog-covered, full conformance-fixture regeneration) — codes remain permanent (never renamed or reused) and consumers may still assert them. All-errors-in-one-pass (per phase) unchanged. Path grammar and traversal order remain normative cross-target appendices. The normative appendices are VERSIONED; appendix-driven behavior changes are always declared, never silent. Consumer-prefixed codes emitted by consumer-native checks are outside this surface (their templates are consumer-owned) |
| 17 | Write path | PRESERVE SOURCE LEXEMES: the document model retains each value's lexeme; values untouched by an op serialize byte-identically WITHIN A BACKEND (write-side TOML fidelity is within-backend only — Go and Python emit different bytes for the same migrated TOML); constructed values render per a pinned table. Migration output revalidates by construction; a normative canonical-serialization appendix mirrors the read-side one. AMENDED (decision 24): the write path REFUSES to serialize a document at any format_version other than the schema's current one — no conforming producer can create new staleness |
| 18 | Drift gate | AMENDED: `strictspec check` = regenerate from the manifest, byte-compare against strictspec's OWN canonical emitter output. There are NO external formatters anywhere — no pinned ruff/prettier, no version treadmill, no repo-config discovery problem; the emitters are the formatting authority and their output is pinned by the canonical-emission rules. `check` also prints the complete blind-spot inventory — unchecked leaves and consumer-check declarations (decision 29) — and hard-errors on runtime/codegen pairing mismatch. Consumers regenerate-and-commit on releases; the batch-regeneration helper across consumer repos is a REQUIRED DELIVERABLE — per repo it requires only the manifest-declared generated paths to be clean/unmodified (other working-tree dirt from concurrent sessions is irrelevant), scopes its follow-up commit to exactly those generated paths, commits via `rlsbl commit` (never reimplementing the Autogenerated-trailer flow), and NEVER pushes |
| 19 | Generated-code pairing | Generated code and runtime from the SAME strictspec release — exact match, hard error on mismatch. Dev builds carry a dev version string that pairs only with itself. CLARIFIED: under the ecosystem's always-latest dependency rule, a runtime floated past its generated code hard-errors immediately — that error IS the intended surfacing mechanism; remediation is regeneration (the batch helper), never pinning. AMENDED 2026-07-12: recovery from a defective strictspec release (e.g. an emitter bug regenerated into consumers) is FIX-FORWARD ONLY — patch release plus batch regeneration; no rollback machinery exists, and pinning around a bad release is banned (the ecosystem fix-forward rule) AMENDED 2026-07-22: for non-Go consumers the pairing holds by construction — the CLI stub ships inside the runtime package (decision 31), so runtime, generated code, and downloaded binary all key off the single package version AMENDED 2026-09-15 (SUPERSEDES the exact-release match above): pairing is on a GENERATED-CODE FORMAT. Generated code declares an integer `GENERATED_CODE_FORMAT` — the shape of emitted code, bumped only when that shape changes — and every runtime declares the inclusive range of formats it reads; pairing succeeds when the declared format is in that range, whatever release produced the file, and refuses otherwise with a message naming the declared format, the accepted range, the generating release, the runtime release and the regeneration remedy. `GENERATED_BY` stays in the emitted file as INFORMATION ONLY: no tool may derive a dependency floor from it. Generated code written before the format declaration existed declares no format and is REFUSED with the same remedy — never read as format 1 (no dual reading). The reason: under floor-only dependencies the exact match made every strictspec release an import-time outage for every already-published consumer, in dependency order, until the whole chain regenerated and re-released — observed once, for weeks. The bump discipline is mechanical, not a convention: the conformance suite pins the emitted shape of every target and fails any change to it that does not move the format |
| 20 | Concurrency | JSONL append is SINGLE-WRITER by declaration: one O_APPEND write per complete line; rewrites are temp+rename (reader-safe). Cross-process coordination is the consumer's job |
| 21 | Schema sharing | REOPENED and AMENDED 2026-07-27 [%%]: shared type-definition files are IN. A schema may import named TYPES from dedicated type-definition files — types only, no cross-file constraints, no transitive imports. This SUPERSEDES the former single-file-only, no-imports ruling. Cross-file constraint references and transitive imports are meta-schema rejections |
| 22 | Ecosystem bugs found during sizing | AMENDED: FILED AS TODOS in each affected project (standalone descriptions, no cross-project coupling), so they survive any slip in this project's schedule. Migrations still fix them structurally where applicable |
| 23 | Domain checks (NEW) | AMENDED 2026-07-12: TWO strictspec-owned layers, not three — the DECISION LANGUAGE IS REMOVED. (1) EVIDENCE RESOLVERS — the only IO surface: a CLOSED, named vocabulary implemented once by the toolchain and runtimes; resolvers return DATA, never verdicts; resolver parity is conformance-tested (identical evidence inputs yield identical outputs across targets); a resolver the environment cannot satisfy (git in a browser) is a hard error at check-execution time, never a skip; extending the vocabulary is a versioned, changelog-covered language change. (2) THE DECLARATIVE CONSTRAINT VOCABULARY — the finite cross-field and cross-document forms, implemented once in the shared emitter IR, executed by a CONSTRAINT ENGINE ported to all four targets and conformance-tested for verdict+code+path+message identity exactly like phase 1. The genuinely bespoke tail is CONSUMER-NATIVE CODE over generated typed values: invoked by the consumer after validation, emitting diagnostics with consumer-prefixed codes via the runtime's constructor — outside strictspec and outside the conformance guarantee, BY DECLARATION (strictspec still has no registration surface, no plugin API, no embedded expression language; consumer checks are not plugged in, they simply run downstream). Rationale: the vocabulary covers the portable majority (references, coverage, uniqueness); a bespoke expression DSL hand-ported to four targets was a perpetual drift surface serving only the tail that is bespoke anyway. CEL-class open computation remains expressly rejected — there is nothing left to embed it in; a recurring check shape across consumers is a vocabulary-evolution conversation, not an escape hatch. AMENDED 2026-07-27 [%%]: the constraint vocabulary gains COUNT-LIMIT and SUM-LIMIT — aggregate forms over a collection with LITERAL bounds only (no computed bounds). AMENDED 2026-07-27 (second amendment) [%%]: the freeze drafts drove two further intra-document forms — CONDITIONAL-VALUE (a gate condition ⇒ a target field equals a LITERAL; evidence wavescript Pin + rlsbl preid/bump) and COLLECTIONS-DISJOINT (two sibling arrays share no element, element-level; evidence rlsbl include/exclude, correcting the former mis-citation of that rule as `mutual exclusion`'s origin) — and closed the gated-form CONDITION SET to six kinds {present, absent, equals-literal, not-equals-literal, in-literal-set, not-in-literal-set}. Numeric comparison predicates, array-contains-literal gates, reference-target predicates, cross-scope existential forbidden-when, open-namespace reference resolution, and enum-typed map keys are REJECTED (single-consumer or expressible/consumer-native; rationale recorded in .stricttools/docs/DESIGN.md — revisit on recurrence) |
| 24 | Version-boundary invariant (NEW) | A document only ever exists at the current format_version within any boundary it inhabits; every boundary crossing is a tool-generated migrate-to-current checkpoint. Producers: the write path refuses non-current serialization (decision 17). Stores: manifest-declared stores get GENERATED INGEST WRITE-DOORS that migrate-then-persist atomically — nothing stale exists at rest, by invariant. Live channels: a normative version-negotiation envelope; the channel agrees on one version or refuses to open; the EGRESS side migrates before sending. Receivers only gate — receiver-side migration does not exist. Browser runtimes never migrate: they receive current bytes or refuse with a structured "update the client" diagnostic. This AMENDS the former "migration is CLI-only" ruling: same intent (canonical documents at rest, no runtime migration sprawl), stronger mechanism (checkpoints are tool-generated, explicit, single-target, hard-failing — never automatic, never receiver-side). rlsbl integration: format_version bumps become deploy-gated release events (see decision 25) |
| 25 | diff (NEW) | AMENDED 2026-07-12: the PROOF-CARRYING ANALYZER IS UNBUNDLED into a separate future project; strictspec ships the EMPIRICAL engine. `strictspec diff` (inputs: a schema at two format versions, the migration between them, and a REQUIRED `--corpus <glob>` of real documents) runs FLIP-SCAN (every corpus document validated at N and N+1; every flip reported with the document and its killing diagnostics — a flipped document is a real witness, no synthesis needed), MIGRATE-ROUND-TRIP (soundness: M(d) revalidates at N+1 for every corpus d valid at N; completeness: M never errors on a valid-at-N corpus document — failures are counterexample documents, enabling red-green migration authoring), and DOWN-TAXONOMY VERIFICATION (declared down/partial/irreversible exercised against the corpus; a mis-declared taxonomy is a hard error). It emits a CERTIFICATE in a spec-pinned JSON format whose claims carry an EVIDENCE GRADE: `violated` (a corpus document is the counterexample — a proof) or `corpus-supported` (no counterexample in the declared corpus — explicitly NOT a proof; corpus identity and size are recorded). The `proven` grade — machine-checkable proof objects, witness synthesis, the undecidability catalog — is RESERVED for the future analyzer, which emits the SAME certificate format. The formal-semantics appendix, undecidability catalog, and model-search order are still WRITTEN NOW as normative, versioned specification sections (every construct added to the language MUST extend the semantics appendix — a construct without semantics cannot ship); only the analyzer implementing them is deferred. GATE INTEGRATION: the certificate is the required input to rlsbl's format_version deploy gate; `violated` BLOCKS release; the green light is `corpus-supported` over the consumer's declared at-rest corpus; a consumer with no corpus discharges via a committed ADJUDICATION FILE (a strictspec-schema'd TOML document with a signed manual justification). There is no bypass. AMENDED 2026-07-27 [%%]: `diff` additionally runs a SAME-VERSION FLIP-SCAN — the corpus replayed against the OLD and NEW schema at the SAME format_version; a document flipping valid→invalid is an un-bumped narrowing and a HARD ERROR (pairs with decision 13's bump rule). ALSO: the PROOF-OBJECT FORMAT and the MODEL-SEARCH ORDER are DEFERRED to the future analyzer project (amending the WRITTEN NOW clause above); the per-construct formal-semantics entries, the undecidability catalogue, and the certificate format ARE still written now |
| 26 | doc-diff (NEW) | Schema-aware document-to-document semantic diff: given one schema and two documents, a structured per-path delta (added/removed/changed with typed values; array element moves keyed by declared unique-by). Pinned output shape; golden-output conformance |
| 27 | Docs (NEW) | First-class selfdoc integration: `strictspec export` emits JSON Schema (per the lossiness table) plus STRUCTURED SCHEMA METADATA; selfdoc owns rendering docs pages from that metadata. strictspec generates no docs pages itself — one docs system in the ecosystem. AMENDED 2026-07-12: the strictspec LANGUAGE REFERENCE has the same home — the specification constitution (authoring guide, constraint vocabulary, op set, appendices) is rendered by selfdoc as strictspec's own docs site, versioned with the spec; the published site is the canonical citable manual. AMENDED 2026-09-20: the specification pages moved out of `spec/` into `.stricttools/docs/` when the repository adopted selfdoc's `.stricttools/` layout, so the directory that holds them has a new name; the constitution, its home inside this repository, and the published site are unchanged. |
| 28 | YAML (NEW) | Excluded, FINAL. YAML's anchors, implicit typing, and ambiguous scalar forms are hostile to lexeme retention and byte-stable writes. Consumers convert once, at migration time; conversion is consumer-owned |
| 29 | unchecked (NEW) | Every `unchecked = true` opaque leaf requires a mandatory `unchecked_reason` string — omission fails the meta-schema. `strictspec check` prints the complete inventory of unchecked subtrees with their reasons, so the language's only sanctioned blind spot is always visible in review. AMENDED 2026-07-12: the stance key for consumer-validated blobs is `consumer_check = "<name>"` (the named check is consumer-native code, never executed by strictspec — see decision 23), and the inventory prints BOTH unchecked subtrees and consumer-check declarations: every strictspec-blind subtree is visible in review |
| 30 | Defaults (NEW 2026-07-12) | REMOVED from the language. A field is required or optional; an absent optional field binds as ABSENT — there is no bind-time injection and no `default` construct (a `default` key in a schema fails the meta-schema with a dedicated diagnostic). Consumers handle absence in code, visibly. Format evolution still injects literals — explicitly, once, at migration time (add_field / merge_defaults). This deletes the one construct that contradicted the guiding principle: a typed value never carries data the author didn't write |
| 31 | Binary distribution (NEW 2026-07-12) | AMENDED 2026-07-27: wrapper packages are ELIMINATED — one published name per registry. The CLI ships inside the runtime packages via rlsbl's first-party LAUNCHER artifact mechanism (checksum-verifying templates): the PyPI strictspec wheel (stays `py3-none-any`) exposes a `strictspec` console script that lazy-downloads the EXACT-version Go binary from the GitHub Release — with SHA-256 checksum verification — into a platform cache on FIRST CLI INVOCATION; the npm strictspec package carries a stub bin that downloads on FIRST RUN — NEVER at postinstall, so library-only installs perform zero network access (an npm first-run launcher variant is added to rlsbl in a later phase). Go consumers install the module directly (the module IS the binary). goreleaser cross-compiled per-OS/arch archives on the GitHub Release remain the single binary source of truth. Runtime package version = strictspec release version, so the runtime package and the binary it lazy-downloads are always the same release; consumer CI simply runs the packaged CLI (`uv run strictspec` / `npx strictspec`) and gets the repo's exact paired version. A failed download is a hard error with manual-install remediation, never a silent fallback. Consequence, accepted: a non-Go consumer's first CLI run per machine needs GitHub network access (cached thereafter) |
| 32 | Enum arms sourced from a document (NEW 2026-07-27) | FIRST-CLASS construct: a schema may declare enum arms SOURCED FROM a named document, with toolchain-enforced FRESHNESS — stale baked arms are a hard error in `gen` and `check`. This creates a sanctioned data→schema dependency edge, in the open, gated by the toolchain |
| 33 | rlsbl↔strictspec mutual dependency (NEW 2026-07-27) | No deadlock-escape machinery. The two tools depend on each other; incident recovery is running the PREVIOUS rlsbl version — there are no bootstrap-bypass flags and no escape hatch |
| 34 | Bootstrap stamping (NEW 2026-07-27) | Stays per-consumer one-time conversion scripts per the existing bootstrap contract (decision 13) — no centralized stamping tool, no shared runtime stamper |
| 35 | No duration scalar (NEW 2026-07-27) [%%] | The datetime scalar set (date/time/datetime) suffices for the corpus; there is no `duration` scalar. Future demand routes through custom scalar registration, never a new built-in |
| 36 | No performance posture (NEW 2026-07-27) | No performance target, benchmark, throughput, or latency claim is stated anywhere in the design — correctness and cross-target conformance are the only guarantees |

Guiding principle throughout: strictspec is pristine and clean; the burden is on consumers to adapt.
No escape hatches, no lenient modes, no warnings, no implicit defaults.

## Architecture (directory map)

- `.stricttools/docs/` — the specification pages, the constitution: schema language, the pinned concrete TOML surface syntax
  (appendix-surface-syntax.md), constraint vocabulary, error model, primitives
  appendix (read side), canonical-serialization appendix (write side), message-template
  appendix, generated API contract, union diagnostics, versioning and migration rules, the
  version-boundary invariant, the domain-check architecture (evidence resolvers + constraint
  vocabulary), the accepted-set formal semantics and undecidability catalog (written now,
  implemented only by the unbundled future analyzer). Language-neutral. Rendered
  by selfdoc as strictspec's published language reference (decision 27).
- `go/` — THE TOOLCHAIN and the Go releasable: module `github.com/smm-h/strictspec/go`,
  `cmd/strictspec` (strictcli-based) + the runtime subpackage (diagnostics, coercers, ordered
  lexeme-retaining document I/O, constraint engine).
- `python/` — Python runtime library and PyPI releasable (document I/O, diagnostics, tagged
  values, constraint engine) for generated frozen-dataclass code.
- `ts/` — TypeScript runtime library and npm releasable: diagnostic types plus lossless
  JSON, TOML (via `toml-eslint-parser`), and JSONL parsers producing tagged document-model
  values, and the constraint engine.
- `conformance/` — dev_node. Shared fixtures + runner + parity checkers over FOUR targets,
  including the constraint engine and evidence-resolver parity.
- `examples/` — paper schemas drafted before implementation; claudestream and PixelWeaver
  first; construct-only exercises (shared types, enum baking, aggregates) plus corpus-DRAFT
  sources (BetterClaude, imagine) round out the construct-set stability gate (decision 3).

## Distribution

- Registries: PyPI `strictspec` (Python runtime + CLI stub), npm `strictspec` (TS runtime + CLI stub), Go module `github.com/smm-h/strictspec/go` (toolchain + Go runtime). One name per registry; there are no separate binary packages.
- Names confirmed by the owner 2026-07-27: PyPI `strictspec` is free; npm `strictspec` is owner-reserved (a 0.0.0 placeholder); GitHub `smm-h/strictspec` is to be created at scaffolding.
- The Go binary reaches non-Go consumers through the runtime packages themselves (decision 31) via rlsbl's launcher mechanism: goreleaser archives on the GitHub Release are the single binary source; the packaged CLI stub lazy-downloads the exact-version binary (SHA-256 verified) on first invocation. Runtime package version equals the strictspec release version, so the CLI a consumer runs is the same release as the runtime it installed.

## Donor inventory

| Donor | What it contributes | Where it lands |
|---|---|---|
| go-toml-edit | In-house comment-preserving TOML AST — document-model substrate | go/ toolchain + runtime |
| migrable | Migration engine code absorbed (Go), minus CEL. Dropped: `transform`, `raw`, `merge_defaults_by_key`. Added: `wrap_in_array`, `unwrap_singleton`. Verified 2026-07-12: the dropped ops are exercised only by migrable's own tests and docs — no consumer uses them. migrable-the-project's retirement is DECOUPLED from this plan (decision 6; its one remaining consumer is outside this corpus) | go/ migration engine |
| pgdesign `pkg/diagnostic` | Diagnostic model; Table/Column generalized to Path. Live donor: pgdesign keeps its severities/suppression consumer-side, fed by strictspec diagnostics | go/ runtime; the specification |
| pgdesign `parse.go:2036-2130` | Six node coercers, extended per scalar rules | go/ runtime |
| PixelWeaver generator | Emitter design + templates (TS ~301 lines, typed-model ~120, cross-field reworked to any-scope); Python target retargeted to frozen dataclasses | go/ generator |
| PixelWeaver documents | Fixture corpus — ONLY documents that map to its three drafted schemas; its project.json/history.json save format is OUT of scope | conformance/, examples/ |
| wavescript | 47 score fixtures + the golden render-hash manifest (158 pairs) — conformance seed + regression oracle | conformance/, examples/ |
| predraw scene corpus | Scene documents bounding the construct set (source of the `number` scalar and node-kind unions) | the specification, examples/, conformance/ |
| incantino schema notation | Header + per-property notation, battle-tested across 64 schemas (compatibleSince = documentation only) — donor only, not a consumer | the specification |
| strictcli | The CLI framework itself (decision 10), the dual-implementation parity harness pattern, AND the Go companion-tag release pattern | go/ CLI; conformance/ |
| claudestream budget change | Flagship shape migration used as a conformance FIXTURE: rename_field + wrap_in_array (down: partial) | the specification, examples/ |
| rlsbl dev_node rename chain | Pure-rename migration example | examples/ |
| tomlkit | Comment-preserving TOML for the PYTHON RUNTIME only; round-trip conformance within-backend | python/ |

## Migration roadmap (dependency-ordered)

Adoption ticket for every pre-versioning consumer: a one-time conversion script (per the
bootstrap contract) stamping `format_version` into existing files before strictspec reads them.

Consumer corpus (AMENDED 2026-07-27): claudestream, predraw, PixelWeaver, rlsbl, pgdesign,
orxtra, wavescript, plus selfdoc directives (late slot). REMOVED from the corpus, with reasons:
wakethemup and howmuchleft (no declarability relevance); tunebox (dead — superseded by
wavescript); toolstream (owner decision — stays code-first); incantino, demobl, F, and step
(dead/archived). BetterClaude and imagine are corpus-DRAFT sources (paper schemas that stress
the construct set; not yet consumers). mage is independent — evaluate later.

ADOPTION WAVE 1:

1. DONE — PixelWeaver — deleted its 811-line dual-target generator
   (`scripts/generate-manifest-types.py`); the acceptance-test source.
2. DONE — predraw — schema translation; aliases declared; `format_version` added net-new.
3. DONE — claudestream — greenfield `.agent.json` schema + gate; ZERO at-rest corpus, so the deploy
   gate discharged via the FIRST REAL adjudication file; the budget-rename "flagship migration"
   is a conformance FIXTURE, not a live migration.
4. BLOCKED — wavescript — replaces its strictdecode/registry validation; its 158-pair golden
   render-hash suite is the regression oracle; synthesis engine/specgen stay consumer-side. Note
   (decision 30, not reopened): the registry gates on EFFECTIVE (default-backed) values but
   strictspec gates on WRITTEN values only, so at adoption wavescript's format NARROWS —
   gate-relevant optional keys become required-explicit via a `format_version` 2→3 migration that
   uses `merge_defaults` to stamp current effective values into existing scores.
   BLOCKED (2026-07-28) on three recorded design walls awaiting owner decisions: (a) the absence
   of a map-iteration migration op (needed to migrate keyed score collections); (b) a
   validation-seam refactor (where strictspec's verdict boundary sits relative to wavescript's
   existing registry validation); (c) bank scope (how far the score-bank construct set extends).

ADOPTION WAVE 2:

5. DONE — rlsbl (low-risk surfaces first) — release-file gate + config validation + the
   certificate deploy gate wired via rlsbl's external-checks mechanism.
6. DONE — orxtra — ~1200–1600 lines of hand-rolled TOML loaders across 7+ packages collapsed; the
   tagged-value entry point serves its compose flow.
7. DONE — rlsbl changelog JSONL (the high-risk half) — the changelog engine gates on strictspec;
   the fleet-wide per-line `format_version` backfill is COMPLETE (finalized chmod-444 JSONL files
   across the fleet stamped, enforcement flipped on, rlsbl's OWN repo last), with the red-green
   test proving stamped-file regeneration byte-identity + `.validated` cache behavior.
8. DONE — pgdesign — gate-not-swap adoption (strictspec gates; pgdesign keeps its own model);
   registered THREE custom scalars (identifier/pgtype/sql-expression); its `pkg/diagnostic`
   (severities/suppression) stays consumer-side, fed by strictspec diagnostics.

LATE:

9. DONE — selfdoc directives — the DirectiveSpec catalog became a strictspec schema; the last
   committed consumer, for blast-radius reasons.
10. CHECKPOINT — BetterClaude — paper contracts standing; its monorepo adoption UNSTARTED
    (adopts generated validators when its schemas phase arrives).
11. CHECKPOINT — imagine (corpus-only standing) / mage (independent standing) re-evaluation.

## Defects found during sizing

Filed as todos in each affected project (2026-07-11), described standalone per todo
confidentiality. Where a migration to strictspec fixes one structurally, the migration is the fix;
the todos exist so the bugs survive any change of plan here.

## Pre-scaffolding verification (CLOSED)

The Go-tag question is RESOLVED with evidence: rlsbl's monorepo release flow emits Go companion
tags (`{path}/v{version}`) alongside `{name}@v{version}`, proven live in strictcli (both the
`go-strictcli@v0.25.3` and `go/v0.25.3` tag families exist). No layout change is needed before
`rlsbl monorepo init`; the CLI's strictcli dependency (decision 10) follows the ecosystem's
always-latest rule.

## Status

SHIPPED AND ADOPTED (2026-07-28). The toolchain is RELEASED at 0.1.0 across all four
distribution channels: PyPI (Python), npm (TypeScript), Go module (Go), and GitHub Release
assets. The construct set, constraint vocabulary, migration op set, and error-code catalogue are
STABLE BUT GROWING in the growth phase (decision 3); released-surface compatibility from 0.1.0
onward is governed by semver at release boundaries, not by a freeze.

- Four-target conformance is GREEN: 42 fixtures, 168 checks pass (42 × 4 targets), cross-target
  parity zero findings (verdict + code + path + message identical across Python, Go, TS, and the
  reference target).
- The acceptance test is GREEN with the 3-entry LOCKED waiver list (the three recorded,
  intentionally-waived divergences; no additions permitted without an explicit owner decision).
- Adopted consumers (roadmap items closed): predraw; claudestream (deploy gate discharged via its
  FIRST REAL adjudication file); PixelWeaver (dual-target generator DELETED); orxtra; rlsbl
  (release-file gate + certificate deploy gate + changelog engine, with the fleet-wide per-line
  `format_version` backfill COMPLETE); pgdesign (gate-not-swap + three custom scalars); and
  selfdoc directives.
- wavescript adoption is BLOCKED on three recorded design walls awaiting owner decisions:
  map-iteration migration op absence, the validation-seam refactor, and bank scope (see roadmap
  item 4).
- Checkpoints recorded (not yet consumers): BetterClaude (paper contracts standing; its monorepo
  adoption unstarted), imagine (corpus-only standing), mage (independent standing).

Below is the pre-release construct-set-stability record, retained for provenance (it uses the
original "freeze" framing, retired 2026-07-28 per decision 3):

CONSTRUCT SET FROZEN (soft-freeze regime, decision 3) as of Phase 3.3 (2026-07-27): the specification is
redrafted in full (Phase 1), all fifteen examples/ drafts came back clean with their gap notes
resolved (Phase 2), and Phase 3.3 absorbed every finding into the specification — the concrete TOML surface
syntax is pinned (appendix-surface-syntax.md), the vocabulary gained conditional-value +
collections-disjoint + the closed condition set, and the aggregate/ranges/migration/enum-selector/
rendering pins landed. NEXT: the scaffolding and build phases. The BINDING freeze remains the first
release (no new construct enters without the examples/ gap-note process; implementation-driven
amendments before the first release are still normal and recorded).

Phases, in order (all complete as of 2026-07-28):

1. ~~Redraft the specification in full.~~ DONE (Phase 1).
2. ~~Draft examples/ and resolve the gap notes to reach the construct-set stability gate.~~ DONE
   (Phase 2 + Phase 3.3; construct set settled, stable-but-growing per decision 3).
3. ~~Scaffold the monorepo (the Go-tag question is closed — see Pre-scaffolding verification).~~ DONE.
4. ~~Build toward the acceptance test.~~ DONE (acceptance test green with the 3-entry locked waiver list).
5. ~~A SINGLE release at the very end (the first release that ships the construct set and appendices).~~ DONE (0.1.0 on
   PyPI + npm + Go module + GitHub Release assets; released-surface compatibility now governed by semver at release boundaries).
6. ~~Then the adoption waves (wave 1, wave 2, late).~~ DONE except wavescript (BLOCKED) and the
   BetterClaude/imagine/mage checkpoints (recorded) — see the migration roadmap above.
