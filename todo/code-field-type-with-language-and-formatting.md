# A `code` field type: multi-line source text with a declared language and a formatting stance

## Context

Consumers keep source snippets inside documents that strictspec validates: a
TOML table whose rows carry a few lines of Go, Python, or shell in a
multi-line literal string, next to a name and a one-line description. Today
such a field is declared as `type = "string"`, so the schema cannot say that
the value is code, in which language, or whether it must be formatted, and
every consumer invents its own check for those three facts.

## Problem

- A schema has no way to state that a string is source text. Tools that
  read the schema (renderers, documentation generators, editors, agents
  writing rows) cannot tell a snippet from prose.
- Formatting is unenforced. Two rows written by two sessions differ in
  indentation, trailing whitespace, and final newline, and nothing refuses
  the drift. A consumer that wants uniform snippets must run the language's
  formatter itself and diff, outside strictspec.
- The identity guarantee limits what a validator may do. "The value equals
  the formatter's output" is decidable in one language only: `gofmt` is a Go
  library, `black` is Python, `prettier` is Node. A validator that ran a
  formatter in one target and not the others would report different verdicts
  for the same document, which is the defect strictspec exists to remove.
  The design's drift-gate decision also states that strictspec runs no
  external formatters anywhere.

## Solutions

### A. `code` type with decidable text rules, plus a declared formatter enforced outside the validators (recommended)

Add a scalar type `code` with a required `language` attribute (a closed list
starting with go, python, typescript, shell, toml, json, sql) and an optional
`formatter` attribute naming the canonical formatter for that language. The
generated validators enforce only what all targets can decide identically:
non-empty, one newline convention, no trailing whitespace on any line, a
final newline, and consistent leading indentation that readers strip. The
`formatter` attribute is metadata: a separate command with effects (for
example a `fmt` or `check` subcommand beside `validate`) reads the schema,
finds every `code` field that declares a formatter, runs it, and refuses any
value that changes, with a message naming the field, the language, and the
formatter.

- Pro: keeps the verdict identical across targets; the validators stay pure.
- Pro: one declaration governs every snippet of a language across the fleet.
- Pro: the refusal is in strictspec's voice with a remedy, not a consumer
  grep.
- Con: two mechanisms, a type and a command, for one concern.
- Con: the command depends on a formatter binary being installed, which the
  drift-gate decision avoids for generated code; the todo proposes that the
  no-external-formatter rule stays for emitted code and that this command is
  the one declared exception, opt-in per field.

### B. `code` type whose validators run the formatter

The same type, but the verdict includes "formatted".

- Pro: one mechanism.
- Con: breaks verdict identity unless every target spawns the same binary,
  which turns a pure validator into a process runner and contradicts the
  drift-gate decision.

### C. No new type; consumers own the check

Leave `string` as is; each consumer runs its formatter over its own snippet
fields.

- Pro: no strictspec change.
- Con: the schema keeps lying about what the field is, and the check is
  reinvented per consumer, differently.

## Open choices for whoever picks this up

- The type name (`code` versus `source`), and whether `language` is an
  attribute of the type or a separate required field beside it.
- Whether the decidable rules are fixed or per-language (Python and
  TypeScript care about indentation width; Go does not).
- Whether the formatting command lives in strictspec or is left to the
  consumer, with strictspec providing only the metadata.

## Affected files

- `go/internal/ir/scalars.go` and `go/internal/schema/read.go`: the scalar
  type set and its attributes.
- `go/internal/emit/emit.go` and `go/internal/export/jsonschema.go`: the
  per-type emission and the JSON Schema export.
- `python/src/strictspec/_ir.py` and `python/scripts/gencodes.py`, and the
  TypeScript counterparts under `ts/`: the same type in the other targets.
- `conformance/fixtures/_inputs/meta/`: fixtures for the new type and its
  refusals, in all targets.
- `DESIGN.md`: the error-output and drift-gate decisions, amended to record
  the exception if solution A is taken.

## Effort

Solution A: a few days for the type across three targets with conformance
fixtures, plus one to two days for the formatting command and its
remedy-truthfulness test. Solution C: none. Solution B: not recommended.
