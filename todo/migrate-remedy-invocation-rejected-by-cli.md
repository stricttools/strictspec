# The pinned migrate remedy invocation is rejected by the migrate CLI

## Context

The spec pins the remedy template for `STRICTSPEC_GATE_UNSUPPORTED`
(spec/appendix-error-codes.md): the `{invocation}` slot is the exact
command `strictspec migrate --schema <schema> --to <expected> <paths>`,
and appendix-rendering.md repeats the same spelling in its example. The
runtime/validators emit this invocation verbatim in the error message.

## Problem

Two defects, one message:

1. **The emitted command does not run.** The real CLI takes `schema` as
   a positional argument (`strictspec migrate <schema> <documents...>
   --to <int>`, verified against the installed 0.2.3 CLI's own help);
   `--schema` is not a recognized flag, so a user who runs the printed
   remedy verbatim gets a usage error. The template is spec-pinned, so
   the fix is a spec edit plus the emitting code, not a code-only patch.
2. **The remedy cannot repair the marker-less case at all.** A document
   MISSING the format-version marker entirely (as opposed to carrying a
   wrong version) is not something `migrate` can act on — there is no
   version to migrate from — yet the same remedy is emitted. That case
   needs its own error text naming the real fix (stamp the marker /
   re-generate the document), or a migrate mode that adopts an unmarked
   document at a stated version.

## Solutions

- Fix the spec template to the CLI's real signature (or change the CLI
  to accept `--schema` — worse: two spellings of the same argument).
- Split the marker-missing case into its own error code with an honest
  remedy. Pros: remedies stay runnable-verbatim; cons: a new code and a
  conformance update.
- A remedy-followability test in the conformance suite: every pinned
  invocation template is executed against a fixture and must not be
  rejected at argument parsing. This prevents the class, not just the
  instance.

The most correct bundle is all three: template fixed, case split,
followability conformance test added.

## Affected

- spec/appendix-error-codes.md (the template row), the emitting
  runtime(s) in each language, appendix-rendering.md's example, the
  conformance suite.

## Effort

Medium (spec + multi-language runtimes + conformance).
