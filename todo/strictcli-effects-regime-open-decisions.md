# strictcli effects regime: the three decisions the migration did not make

SPLIT from `strictcli-effects-regime-migration.md` (filed 2026-08-09). The mechanical
migration — effect classification on all eight commands, the effects handle, the deleted
`--dry-run` BoolFlag, the two structural fixes — is done and moved to `todo/.done/`. These
three items were never mechanical, and each needs a decision before anyone writes code.
Nothing below is reworded from the original file.

## 1. `WithConsequential()` for `migrate`

From the original Work list, item 5, verbatim:

> 5. Consider `WithConsequential()` for `migrate` (in-place rewrite of user documents).

And from the command inventory's `migrate` row: *"mutating, plausibly **consequential** —
in-place rewrite, no backup"*.

The migration left `migrate` mutating-but-not-consequential, so it never prompts. Declaring it
consequential means every non-interactive `strictspec migrate` must pass
`--approve-consequential` or be refused, which reaches consumer CI. That is a real
user-facing decision, not a cleanup.

## Blocker: the `check` command name collision

The `effects-bypass` lint only materializes when the check system is enabled
(`WithChecks`/`WithChecksEmbed`), and enabling it **auto-registers a framework command named
`check`**. This project already owns `check` as one of its eight documented commands, and
strictcli has no duplicate-command guard — so the lint is unreachable as things stand.

**Recommendation: rename this project's `check` command** so the check system can be enabled
and the lint wired up. It is a breaking CLI change on a 0.1.0 tool, which is the cheapest it
will ever be; the alternative is permanently forgoing the one static guard against effect-seam
drift. The rename needs a decision on the new spelling (something like `verify` or
`check-generated`) plus updates to the charter, the CLI reference, and any consumer scripts.

## Requirement: do not regress the migrate preview

Today `migrate --dry-run` prints the full would-be document bytes. The framework's would-do log
prints `write <path> (N bytes)` instead, so a naive migration **loses detail**. The charter
promises a "per-file structured diff", which neither the current code nor the framework log
delivers — so this is the moment to build it rather than a regression to tolerate. Treat the
structured per-file diff as the target output for `migrate` under preview.

Status after the migration: the regression was avoided rather than the requirement met.
`migrateHandler` still renders the full would-be bytes per file under `--dry-run`, on top of
the framework's would-do log. The charter's "per-file structured diff" claim
(`go/DESIGN.md`, the `strictspec migrate` bullet; root `DESIGN.md` decision 9) is still
unfulfilled. `doc-diff` already computes a per-path typed delta with a pinned output shape,
so the engine exists — what is undecided is the output shape `migrate --dry-run` should print.

## Affected files

- `go/cmd/strictspec/main.go` (the `check` registration; a `WithConsequential` on `migrate`)
- `go/cmd/strictspec/phase6.go` (`migrateHandler`'s preview rendering)
- the charter and CLI reference, if the `check` rename is adopted
