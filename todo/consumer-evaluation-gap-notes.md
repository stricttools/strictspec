# Gap notes from a prospective greenfield consumer evaluation

## Context

A prospective greenfield consumer evaluated dclrbl's design (all seven DESIGN.md files, state as of
2026-07-12) for adoption. Its shape: large collections of small agent-authored JSON/TOML content
documents, validated by a Go program (authoritative, at load time) and a TypeScript browser client
(same formats, fetched over HTTP), with a file-watch hot-reload loop during authoring. No legacy
pre-versioning files — a clean bootstrap-free adopter.

The verdict was "adoptable as-is," with consumer-side adaptations accepted for the hard exclusions
(no defaults — fallbacks live visibly in consumer code; bespoke checks — consumer-native code over
typed values). The evaluation surfaced two candidate spec changes and two design questions. All
four are filed here together because the construct-set freeze has not happened yet and the
examples/ gap-note process explicitly solicits expressiveness evidence before it does. This todo
is that evidence.

## Item 1 — Aggregate/budget constraint forms (change request)

### Problem

The cross-field/cross-document constraint vocabulary (spec/DESIGN.md, "Cross-field and
cross-document constraint vocabulary"; root decision 23) covers references, uniqueness, coverage,
co-presence, ordering, and per-field numeric ranges — but has no aggregate forms: no count of
matching entries vs a limit, no sum of a field vs a budget. Consumers whose documents are consumed
by resource-bounded runtimes need budget caps ("count of entries of kind X across the collection
<= N", "sum of field Y <= B") as validation, not convention: a structurally valid document set
that exceeds a resource budget is a denial-of-service on the consuming runtime.

Today this lands in the "bespoke tail" (consumer-native code), which works but loses exactly what
dclrbl exists to provide: cross-target conformance (the Go and TS sides of the same consumer must
duplicate the budget checks and can drift) and declared-in-the-schema visibility.

### Proposed addition

Two closed forms, in the spirit of the existing vocabulary (declarative, non-computational,
statically checkable, no expressions):

- `count-limit`: count of elements in a named collection (optionally filtered by a literal
  field=value match) compared against a literal integer bound (<=, >=, ==).
- `sum-limit`: sum of a named numeric field across a collection compared against a literal bound.

No arithmetic beyond the aggregation itself, no cross-references in the bound (literal only), no
nesting. This stays firmly on the "vocabulary evolution" side of decision 23's line rather than
the rejected CEL-class side.

### Pros / cons

- Pro: closes a real conformance gap for any consumer with resource-bounded document collections;
  the forms are as portable and enumerable as unique-by/set-coverage.
- Pro: literal-bound-only keeps the constraint engine's evaluation model trivial (single pass,
  no dependency resolution beyond the existing collection evidence).
- Con: first numeric-aggregation forms in the vocabulary — a new category, and the filter syntax
  ("count entries where kind == X") needs careful bounding to avoid becoming a query language.
- Con: cross-document sums interact with the evidence-resolver model (which documents are in
  scope) and need the same scoping rules as existing cross-document forms.

### Affected files

- spec/DESIGN.md (constraint vocabulary tables, constraint engine section)
- root DESIGN.md (decision 23 wording)
- conformance/DESIGN.md (fixture classes for the new forms)

### Effort estimate

Small-to-medium spec change (two vocabulary rows + evaluation semantics + fixtures); the
constraint engine already iterates collections for unique-by, so evaluation machinery is
mostly present.

## Item 2 — Cross-file shared named types (evidence-backed reopening of decision 21)

### Problem

Decision 21 locks single-file schemas with no imports/composition. Named types give reuse within
one schema file only. The evaluating consumer has many document formats (roughly a dozen) that are
required — by that consumer's own validation policy — to share one identical mandated block
structure. Under current dclrbl this means maintaining N copies of the same named-type definition
across N schema files, which reintroduces the exact cross-implementation drift problem (now at the
schema layer) that dclrbl was built to kill at the code layer: edit the shared block in one schema,
forget another, and two formats silently accept different shapes of "the same" structure.

This is filed as corpus evidence for the pre-freeze sufficiency claim, not a demand: the pattern
"many formats, one mandated common block" did not appear in the analyzed corpus, and the
examples/ method says exactly this kind of gap should be fed back before freeze.

### Options

1. **Shared named-type files** (import of type definitions only — not schema composition, not
   inheritance): a schema may reference named types from a sibling definitions file. Narrowest
   possible opening of decision 21.
   - Pro: kills the drift; keeps one-format-one-schema intact; types-only import is far short of
     general composition.
   - Con: schemas stop being self-contained single files (tooling, hashing, and the "read one file,
     know the format" property all get caveats).
2. **Keep decision 21; bless consumer-side schema generation** — consumers with shared blocks
   generate their N schema files from their own template source, and dclrbl documents this as the
   intended pattern.
   - Pro: dclrbl stays pristine; zero spec change; the consumer's generator is ordinary code.
   - Con: the generated schemas are the ones dclrbl sees, so dclrbl's own tooling (diff,
     migrations, docs) operates on artifacts one step removed from what the consumer authors;
     every such consumer reinvents the generator.
3. **Reject with rationale** — record the pattern in decision 21 as a known, deliberately unserved
   case, so future consumers find the answer instead of re-filing this.

Any of the three is a valid resolution; the current state (pattern unaddressed, decision silent on
it) is the only bad outcome.

### Affected files

- root DESIGN.md (decision 21)
- spec/DESIGN.md (Construct set, schema file model) if option 1
- examples/DESIGN.md (gap-note record) regardless

### Effort estimate

Option 1: medium (schema loading, hashing, and conformance identity all touch it). Option 2 or 3:
documentation-only.

## Item 3 — What obligates a format_version bump? (design question + small tooling request)

### Problem

The gate mechanics are crisp (exact-match, hard-refuse, decision 13), but the docs never state the
rule for which schema edits REQUIRE a bump. Additive edits (new optional field, new enum variant)
appear bump-free: existing documents still carry the current integer and still validate. But
nothing tool-enforced catches the dangerous inverse — a NARROWING schema edit made without a bump
(field made required, range tightened, variant removed), which fails previously-valid documents
with ordinary validation errors instead of a version-gate error and its structured remediation
payload. That failure mode lands on exactly the users the version gate was designed to protect,
with the wrong diagnostic class.

### Questions and proposed addition

1. State the normative rule in spec/DESIGN.md: which classes of schema edit require a bump
   (proposal: any edit that shrinks the accepted-document set), and which are explicitly
   bump-free (any edit that only grows it).
2. Small tooling request: `dclrbl diff` already runs flip-scan over a corpus for versioned
   changes. Expose the same flip-scan for SAME-VERSION schema edits (old schema, new schema, same
   integer, plus corpus): if any corpus document flips valid-to-invalid, the edit needed a bump —
   hard error. This makes the normative rule mechanically enforceable instead of conventional,
   which matches the project's own "hard constraints, not soft guidance" philosophy.

### Affected files

- spec/DESIGN.md (Versioning section — normative bump rule)
- root DESIGN.md (decisions 9/13 vicinity)
- CLI surface (diff subcommand or a new check mode)

### Effort estimate

Rule wording: small. Same-version flip-scan: small (reuses the existing diff machinery with the
version-pairing check relaxed).

## Item 4 — Validation performance posture for watch-loop revalidation (design question)

### Problem

No design document addresses validation cost. The evaluating consumer's authoring loop revalidates
on every file save (watch/hot-reload over a few-hundred-file document set) and needs per-edit
revalidation to be effectively instant. Structural signals are encouraging (compiled zero-dep
generated validators, one compiled artifact reused across many inputs, streamed JSONL), but
nothing is a stated property, so consumers cannot tell whether hot-path revalidation is a
supported use or an accident of implementation.

### Proposed addition

A short performance-posture paragraph in the root charter or spec: whether generated validators
are intended to be cheap enough for per-edit revalidation loops, whether cross-document constraint
evaluation is incremental or whole-set, and what (if anything) consumers should expect to cache.
No benchmarks needed at design phase — just a stated intent so the property is protected during
implementation rather than discovered.

### Affected files

- root DESIGN.md or spec/DESIGN.md (new short section)

### Effort estimate

Documentation-only at this stage.

## Explicitly NOT requested

- Defaults (decision 30): the evaluation accepts absence-binds-as-absent; consumer code owns
  fallbacks visibly. No change wanted.
- Any expression language, computed constraints, or plugin surface (decision 23's permanent
  rejections): not wanted, not needed.
