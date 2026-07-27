# Gap note — wavescript JSON score (format_version 2)

Source: `wavescript/internal/score/score.go` (format_version gate ~L999),
`internal/registry/registry.go` (`Spec`, `Cond`, `Pin`, `ValidateValue`,
`ValidateEffective`, `KeyApplicability`, `effectiveString`),
`internal/registry/topo_subtractive.go`, `internal/registry/lfosync.go`.
Schema: `score.schema.toml`. Samples: `valid-01-embedded-bank.json`,
`invalid-01-gate-violations.json`.

This is the strongest conditional-required stress test in the corpus. The
registry is a per-topology table where each parameter carries declarative
applicability rules (`Cond`: required/optional/forbidden gated on another key)
and value pins (`Pin`: a key must equal a literal while a gate matches). The
question the draft answers: **does the constraint vocabulary carry the registry's
gate semantics?** Answer: it carries the SIMPLE gates and the structure cleanly,
but four registry mechanisms fall outside it — one of them (effective-value
gating) is structural and central.

## Expressed cleanly

- **The `format_version` = 2 integer gate.** score.go checks a required integer
  `format_version == 2` before anything else, with a fast-fail remediation
  message. This maps EXACTLY onto strictspec's format_version gate: gate runs
  first, `STRICTSPEC_GATE_ABSENT` / `STRICTSPEC_GATE_WRONG_TYPE` /
  `STRICTSPEC_GATE_UNSUPPORTED` carry the structured remediation payload. This is
  the one already-integer-versioned format in the fleet and it is a perfect fit —
  a strong positive.
- **Value constraints.** `sample_rate` int-enum {44100,48000}; `seed` integer
  [0,2^32-1]; `master_gain` exclusive-lower / inclusive-upper (registry
  `MinExclusive`); float ranges on every param. All map onto enum / numeric-range
  constructs (§3.13, §3.15) directly.
- **Topology as a discriminated union.** A preset is `{topology, params}` where
  `topology` is a literal-valued discriminator selecting the `params` shape. This
  is a clean discriminated union (§3.5): a bad topology is
  `STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN` with did-you-mean; the matched arm's
  `params` validate at natural paths with the arm as context. `embedded_bank` is a
  typed map (name regex → Preset). Structurally the whole thing fits.
- **Per-topology parameter whitelist = closed record + unknown-key.** A param
  invalid for a topology (registry: the key is not in the topology's table) is
  just an unknown key against the arm's closed `params` record —
  `STRICTSPEC_KEY_UNKNOWN`. The registry's "forbidden because not in this
  topology" is the unknown-key invariant for free. Clean.
- **Positive-polarity forbidden-when.** `forbiddenWhen(gate, v...)` where the gate
  is an UNCONDITIONALLY REQUIRED key (`source`, `filter_type`) maps exactly onto
  `forbidden-when` with an `in`-set condition (§3.24): `sub_level` forbidden when
  source ∈ {noise,pink,brown}; `q` forbidden when filter_type ∈ {none,ladder_lp};
  `filter_keytrack`/`cutoff` forbidden when filter_type == none. Because the gate
  is required it is always explicit, so effective-value == written-value and there
  is no defaults problem (see FINDING 1). These are the clean core of the system.
- **Opaque domain strings.** `pattern` (track DSL) and `fixed_pitch` (pitch
  grammar) are validated by named consumer-native checks — modeled as opaque
  string leaves with `consumer_check`, inventoried by `strictspec check`. The
  pitch/pattern grammars are not RE2 lexemes, so this is the correct stance
  (custom-scalar registration is the alternative if a lexeme rule is wanted).

### Expected diagnostics — `invalid-01-gate-violations.json` (phase 2)

Phase 1 is clean; phase-2 forms fire in containing-record traversal order
(`bad_filter` before `noise_unison`), then constraint-declaration order:

1. `STRICTSPEC_INTRA_FORBIDDEN_WHEN`
   · path `$.embedded_bank["bad_filter"](subtractive).params.q`
   · slots: key="q", condition="filter_type in [none, ladder_lp]"
2. `STRICTSPEC_INTRA_FORBIDDEN_WHEN`
   · path `$.embedded_bank["bad_filter"](subtractive).params.harm2_level`
   · slots: key="harm2_level", condition="source in [saw, square, triangle, noise, pink, brown]"
3. `STRICTSPEC_INTRA_CONDITIONAL_REQUIRED`
   · path `$.embedded_bank["noise_unison"](subtractive).params`
   · slots: key="cutoff", condition="filter_type in [lowpass, …, ladder_lp]"

**MISSED (FINDING 2):** `noise_unison` sets `unison = 4` while `source = noise`;
the registry Pin requires `unison == 1`. strictspec emits NO diagnostic — the
conditional-literal form does not exist. wavescript errors
`unison must be 1 when source = noise`. This is a real, silent expressiveness
gap, demonstrated as a miss in the sample.

## INEXPRESSIBLE (findings)

### FINDING 1 — effective-value gates depend on defaults, which the language deleted

The registry evaluates a gate on the key's EFFECTIVE value:
`effectiveString(explicit, gate) = explicit[gate] if set else registry-default`
(registry.go). `ValidateEffective` and `KeyApplicability` both read this. So a
`Cond`/`Pin` gated on an OPTIONAL key with a default sees the DEFAULT when the key
is absent.

strictspec removed defaults entirely (decision 30): an absent optional field binds
ABSENT, and conditional-required/forbidden-when "test presence/equality only"
(§3.24) of the WRITTEN value. There is no notion of "the value this field would
have if omitted".

Consequence, concretely with `lfo_rate` forbidden-when `lfo_sync ∈ SyncNoteValues`
(`lfo_sync` is optional, default `""`):
- Registry: `lfo_sync` absent ⇒ effective `""` ⇒ `"" ∉ SyncNoteValues` ⇒ `lfo_rate`
  allowed.
- strictspec: `lfo_sync` absent ⇒ condition "`lfo_sync ∈ {…}`" is false (no value)
  ⇒ `lfo_rate` allowed.

These COINCIDE — but only because the default `""` lies in the same branch that
"absent" maps to. **The equivalence is a property of the corpus data, not a
guarantee of the vocabulary.** The moment a topology adds a `Cond` gating on an
optional key whose default is a MATCHING value — e.g. a hypothetical `requiredWhen`
gated on a `mode` key that defaults to `"advanced"` — the two diverge and
strictspec CANNOT reproduce the registry: an absent `mode` should evaluate the
gate as if `mode == "advanced"` (its default) and trigger the rule, but strictspec
sees no value and does not trigger. The registry's `effectiveString` is exactly
the mechanism strictspec lacks.

This is the deepest finding: the registry's gate semantics are defined over
*effective* values, and strictspec's conditional forms are defined over *written*
values. For the current corpus every optional gate key's default falls in the
absence-equivalent branch, so the drafts validate — but the vocabulary does not
CARRY the registry's semantics, it happens to agree on today's data. Options:
(a) accept the limitation and require that any strictspec adoption of this format
forbid `Cond`/`Pin` gates on defaulted-optional keys whose default is a matching
value (a real narrowing of the registry's expressiveness); or (b) reintroduce a
constrained "condition may reference a schema-declared literal fallback for an
absent gate key" — which is a narrow, opt-in re-admission of a default *for the
purpose of gate evaluation only*, not a bind-time injection. (a) is honest and
cheap; (b) reopens decision 30 partially. Flagged for owner decision — do NOT
silently pick.

### FINDING 2 — Pin is a conditional literal constant, with no vocabulary form

`Pin{Gate, Values, Value}` = "while gate ∈ Values, an explicitly-set key must
equal `Value`" (registry.go; e.g. `unison` must be 1 when source ∈ {noise,pink,
brown}). The vocabulary has:
- `literal constant` (§3.14) — unconditional value equality;
- `conditional-required` / `forbidden-when` (§3.24) — conditional PRESENCE.

It has no conditional VALUE equality ("field, when present, must equal literal L
if a sibling gate matches"). Pin is not composable from the existing forms:
forbidden-when controls presence, not value; a literal constant is unconditional.
Nor can it be pushed into a sub-discriminated union — pins gate on several
different keys (source, filter_type, …) independently, so sub-discriminating
`params` on each would be a combinatorial product of arms. Inexpressible; a new
`conditional-literal` intra-document form (present in the schema as a FICTIONAL
placeholder, flagged) would close it, and it is a small, decidable, portable
addition analogous to conditional-required.

### FINDING 3 — no negative-polarity (`unless`) condition; complement enumeration required

The registry has four polarities: `onlyWhen` (optional while gate matches,
forbidden otherwise), `forbiddenWhen`, `requiredWhen`, `requiredUnless`. Only the
POSITIVE ones (`forbiddenWhen`, `requiredWhen`) map directly. `onlyWhen` and
`requiredUnless` are "forbidden/required UNLESS gate ∈ V" = "forbidden/required
WHEN gate ∈ (Enum ∖ V)". strictspec's condition has no negation/complement
operator, so the schema must ENUMERATE THE COMPLEMENT of the gate's enum (see
`harm2_level`, `pulse_width`, `resonance`, `cutoff`'s required half). This works
but is brittle: adding an arm to the gate enum silently breaks every complement
list that forgot to include it (the new value would wrongly be treated as "matches
the unless"). strictspec's narrowing flip-scan catches SOME of these (an enum
arm added to the gate is a widening, and a stale complement makes a previously-
valid doc invalid — an un-bumped narrowing caught by same-version flip-scan) but
not all (a doc that never exercises the new arm flips nothing). A native
`condition = { field, not_in = [...] }` / an `unless` polarity on the conditional
forms removes the whole class. Recommend adding negative-polarity conditions to
the conditional-required / forbidden-when forms.

### FINDING 4 — the effective preset is a CROSS-RECORD MERGE

`ValidateEffective` runs on the bank preset OVERLAID with score-level overrides
(`Preset.With(overrides)`, registry.go): a track/section may override preset
params, and the gate rules run on the MERGED map. The merged effective preset is
not a single record in the document — it is (embedded_bank preset params) ∪
(track override params), two different subtrees, and when the preset comes from an
EXTERNAL bank file it spans two documents. strictspec intra-document forms operate
on ONE typed containing record (§3.24, "the typed containing record"); they cannot
validate a computed merge of two subtrees. So the applicability rules can be
checked on the bank preset in isolation, but NOT on the effective (overridden)
preset the engine actually renders. This is genuinely outside the intra-document
vocabulary; it is a consumer-native check over the merged typed values (or, if the
override lived in the same document as a nested record, a bespoke form that reads
two sibling scopes — not currently in the language).

## Notes (not counted)

- **`track.instrument` cross-store resolution.** Resolves against `embedded_bank`
  (intra-document reference, clean) OR an external `bank` store (out of document).
  Same open-namespace shape as rlsbl-config's `depends_on`: partly in-document,
  partly in a resolver-provided store. A `bank-presets(bank)` evidence resolver
  would make the external half a `named-reference-must-resolve`. Noted; overlaps
  the rlsbl-config F3 pattern.
- **Retired enum values.** The registry maps removed enum arms to bespoke
  remediation messages (`Retired`, e.g. the retired fluidsynth engine). strictspec
  removing an arm is a narrowing bump (§3.13) and a bad value gets the generic
  `STRICTSPEC_TYPE_NOT_ENUM_MEMBER` + did-you-mean — the per-arm migration prose
  is lost (would live in a migration's remediation or consumer-native). Consistent
  with the language; noted.
- **`Contested` / `NotAutomatable`.** Required-when-used-with-no-default, and
  automation-lane restrictions — both consumer-native (the latter is not document
  validation at all). Out of scope by declaration.

## Verdict

FINDINGS: 4
- F1 (structural, central): the registry gates on EFFECTIVE (explicit-or-default)
  values; strictspec deleted defaults and gates on WRITTEN values. Agreement on
  the current corpus is coincidental, not carried by the vocabulary. Needs an
  owner decision (narrow the format, or admit a gate-only literal fallback).
- F2: `Pin` is a conditional literal constant with no vocabulary form; add a
  `conditional-literal` intra-document form.
- F3: no negative-polarity (`unless` / `not_in`) condition; add it to
  conditional-required / forbidden-when to avoid brittle complement enumeration.
- F4: the effective preset is a cross-record (and sometimes cross-document) merge
  the intra-document forms cannot validate; consumer-native or a new two-scope
  form.
