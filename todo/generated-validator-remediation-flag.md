# Generated validators suggest a CLI flag the CLI rejects

## Context

strictspec-generated document validators produce diagnostics that include a
remediation hint: the command a user should run to regenerate or re-check
against the spec.

## Problem

The generated remediation text suggests invoking the strictspec CLI with a
`--schema` flag, and the CLI rejects that flag as unknown. A user (or an
agent, or a downstream tool relaying the message) who follows the printed
remedy gets a second error instead of a fix. This was observed through a
consumer tool that surfaced the generated message verbatim in its own
validation output; the wrong-command text traced back to the generated
validator, not the consumer.

## Solutions

1. **Derive the remediation text from the real CLI surface (most correct).**
   Whatever emits the hint should read the actual command/flag registry (or
   a constant the CLI itself also consumes) so the hint can never name a
   flag that does not exist — a single authority instead of a hand-typed
   command string in a template.
2. **Fix the template string.** Replace the suggested invocation with the
   spelling the CLI actually accepts. Small and immediate, but the hint can
   drift again the next time the CLI surface moves.
3. **Add the flag, if it was the intended surface.** Only right if the flag
   was designed and never wired; otherwise this grows CLI surface to match a
   typo.

Whichever option: per the red-green convention, a regression test should pin
that every remediation command a generated validator prints parses against
the CLI's own registration (an assertion the generator's tests can make
mechanically under option 1).

## Affected area

The validator-generation templates (wherever remediation hints are
composed) and, under option 1, the CLI surface registry they should derive
from.

## Effort

Option 2 is trivial; option 1 is small and closes the drift class.
