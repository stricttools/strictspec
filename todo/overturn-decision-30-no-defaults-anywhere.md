# Overturn decision 30: no defaults anywhere, not smuggled into consumers

## Context

Decision 30 bans default values inside schemas and says the meaning of an absent
field is "handled consumer-side". Schemas still allow optional fields
(`required = false`), so every optional field forces some consumer to decide
what its absence means.

## Problem

The defaults did not go away. They moved into each consumer's code, where no
schema, validator, reader, or agent can see them. predraw is the clearest case:
its scene schema marks most element fields optional, and `predraw/model.py`
silently supplies:

| Absent field | Value predraw uses |
|---|---|
| `fill` | no fill (the renderer writes `fill="none"`) |
| `x`, `y` | 0 |
| `width`, `height` | 0, so a rect without a size renders as an invisible 0x0 box with no error |
| `opacity` | 1.0 |
| `anchor` | `"start"` |
| `letterSpacing` | 0 |
| font `weight` | 400 (the schema's own comment records the dropped source default) |

selfdoc's `selfdoc.json` handling injects dozens of defaults and transforms the
same way. The owner's intent is that there be no defaults at all, not defaults
relocated to where they are invisible.

## Solutions

### A. Declared defaults applied by strictspec

The schema declares `default = 1` and the generated reader fills it in.

- Pro: defaults are visible and single-sourced.
- Con: defaults still exist, which contradicts the intent.

### B. Optional fields with bindings that force handling

Absent fields surface in generated bindings as an explicit absent state, never a
zero value.

- Pro: small change to documents.
- Con: consumers can still write the equivalent of `OrElse(0)`; nothing
  structural stops the smuggling.

### C. Total documents: `required = false` is banned

Every field is always written, including ones meaning "nothing"
(`"fill": "none"`). Bindings have no optional types, so there is nothing for a
consumer to substitute. A missing `width` becomes an error.

- Pro: no defaults can exist anywhere.
- Con: "nothing" is spelled ad hoc per field (`"none"`, `0`, empty string),
  which reintroduces ambiguity.

### D. Total documents plus a first-class none (recommended)

As C, plus "nothing" is a real, typed value. A field that may hold nothing
declares it in the schema (for example as a union of its real type with none).
Bindings expose it as a sealed choice, so a strictgo consumer must switch over
both cases and the checker refuses a switch that skips one.

- Pro: the schema is the only place "may be nothing" is stated; nothing cannot
  be confused with zero or an empty string; consumers have no room to invent a
  meaning.
- Con: longer documents, and every schema and document with optional fields
  must be migrated.

## Decisions to make

1. Solution D or C.
2. How none is spelled so that TOML, JSON, and JSONL give identical results:
   TOML has no null value. Options include a reserved literal declared through a
   none type, a reserved key form, or JSON `null` with a TOML-specific spelling.
3. Layered configuration (for example strictcli's app config file): an absent
   key there means "this layer does not override", with the value coming from
   another layer or the flag's declared value in the app's registration. Decide
   whether a layer must write an explicit inherit value, or whether "not in this
   layer" stays legitimate because the fallback is declared visibly in one place.
4. Whether a `complete`-style command writes every field of a new document with
   a required choice left for the author (never a filled-in value).

## Consequences

- Records meaning "one and only one of several keys" can no longer use optional
  fields; they become key-selected unions (decided separately, being designed).
- A migration that adds a field must carry an explicit value for existing
  documents, which the migration ops already require.
- A new decision in `.stricttools/docs/DESIGN.md` supersedes decision 30, with
  this reasoning; the generated-code format is bumped.

## Affected files

- `.stricttools/docs/DESIGN.md` and the surface syntax, semantics, rendering,
  and error-code appendices: optionality removed, none added.
- The schema readers, validators, emitters, and bindings in Go, Python, and
  TypeScript.
- JSON Schema export (every field required; none as a declared alternative).
- Conformance fixtures for none, for refused optional fields, and for missing
  fields.
- Every family schema using `required = false`, and the documents they govern
  (predraw scenes, rlsbl config, selfdoc's `selfdoc.json`, howmuchleft's config,
  and others), migrated in one sweep.

## Effort

The strictspec change: about a week across three languages with fixtures. The
family migration: several days, mostly mechanical, largest in predraw and
selfdoc.
