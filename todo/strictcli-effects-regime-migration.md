# Migrate the CLI to the strictcli effects regime

Filed 2026-08-09 from an external investigation of this project's CLI surface. Read-only
analysis; no code was changed.

## Context

`go/go.mod` requires `github.com/smm-h/strictcli/go v0.27.0`. The current release is
**v0.30.0**, and the intervening versions shipped a breaking "effects regime":

- **v0.28.0** — per-command `effect` classification becomes mandatory, side effects flow
  through a closed `ctx.Effects()` method set, and `--dry-run` becomes framework-owned:
  in dry mode effect calls RECORD instead of performing, and a would-do log is rendered.
- **v0.29.0** — `WithConsequential()`: a separate declaration that is the only thing which
  triggers a confirmation prompt. Classification answers "record or perform?"; consequential
  answers "worth interrupting a human?".
- **v0.30.0** — `WithDryRunUnsupported(reason)` for commands that genuinely cannot preview.

The reserved flag quartet (`--dry-run`, `--approve-consequential`, `--quiet`, `--verbose`) is
now framework-owned at every level; declaring one is a registration-time panic. `--yes` is
banned outright.

The Python and npm packages need no work — both are argv-passthrough launchers that download
and exec the Go binary.

## Problem

Two distinct issues.

**1. The pin blocks the upgrade path.** None of the eight commands declares `effect`, so the
project gets no framework preview, no confirmation gate, and no `effects-bypass` lint. The
binary is already published at 0.1.0, so consumers have the pre-effects behavior in hand.

**2. Writing commands have no preview at all.** Verified good news first: there is no
"accepted but ignored" flag anywhere. `gen --dry-run`, `init --dry-run` and `export --dry-run`
reject the flag at parse time (`error: unknown flag '--dry-run'`), and `migrate`'s hand-rolled
`--dry-run` genuinely returns before the rename sweep. The exposure is *absence*:

- **`gen` overwrites generated files in consumer repositories with no preview of any kind.**
  `writeGenerated` chmods each target to 0644 **before** writing (`cmd/strictspec/handlers.go`
  around :660-670), which deliberately defeats the 0444 read-only bit that would otherwise
  stop an unintended overwrite. A wrong `--manifest`, or a manifest whose target output path
  points at a hand-written file, silently clobbers it. It also appends to the consumer's
  `.gitattributes` (`ensureGitattributes`, ~:691).
- **`migrate` rewrites the user's source documents in place** (temp write + rename,
  `internal/.../phase6.go` ~:121-125) with no confirmation gate. Its preview is opt-in
  convention rather than framework-enforced: nothing prevents a future edit from moving a
  write above the `if dryRun` early return (~:107).
- **`export --output` overwrites any path unconditionally** (~:573) with no existence check —
  inconsistent with `init`, which does refuse (~:528).

## Command inventory (as it would classify)

| Command | Writes / mutates? | Effect class | Notes |
|---|---|---|---|
| `gen` | yes — mkdir, chmod 644, write, chmod 444, append to `.gitattributes` | mutating | no preview today |
| `init` | yes — writes manifest + gitattributes; refuses overwrite | mutating | |
| `export` | conditionally — writes when `--output` is set, else stdout | mutating | unconditional overwrite |
| `migrate` | yes — temp write + rename over user documents | mutating, plausibly **consequential** | in-place rewrite, no backup |
| `validate` | no | read_only | |
| `check` | no — reads and byte-compares | read_only | see the name collision below |
| `diff` | no — certificate to stdout | read_only | |
| `doc-diff` | no — JSON to stdout | read_only | |

## Work

1. Bump `go/go.mod` to go-strictcli v0.30.0 (unpinned/floor per the always-latest rule).
2. Add `WithEffect(...)` to all **8** registration sites in `cmd/strictspec/main.go` (~:25-133);
   omission panics at registration, so this is mandatory, not optional.
3. **Delete `migrate`'s `BoolFlag("dry-run", ...)`** (~main.go:102) — a reserved name now
   panics — and replace its read (~phase6.go:29) with `ctx.DryRun()`.
4. Route the **9 direct effect call sites across 5 functions** (`initHandler`, `exportHandler`,
   `writeGenerated`, `ensureGitattributes`, `migrateHandler` — all in `cmd/strictspec/`) through
   `ctx.Effects()`.
5. Consider `WithConsequential()` for `migrate` (in-place rewrite of user documents).
6. **No command needs `WithDryRunUnsupported`.** Both writers already compute everything in
   memory before touching disk (`gen` emits to a string; `migrate` builds an all-or-nothing
   pending slice), and no later step reads state an earlier step wrote.

### Two structural items, both small

- `ensureGitattributes` uses `OpenFile(O_APPEND)` + `WriteString`, and the closed effects
  method set has **no append member** (Run, Spawn, Write, Mkdir, Remove, Rename, Chmod, HTTP).
  It must become read-existing plus a full-content `Write`.
- `Effects().Write` hardcodes 0644, so `gen`'s 0444 must ride an explicit `Chmod` — mechanical,
  since it already chmods separately.

Not blockers: the subprocess and writes in `internal/emit/build.go` and `tools/gencodes` are
not reachable from the CLI (they serve the conformance adapter, which is not a strictcli app),
and the bypass lint scans within a package.

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

## Affected files

- `go/go.mod`, `go/go.sum`
- `go/cmd/strictspec/main.go` (registrations, the reserved-flag deletion)
- `go/cmd/strictspec/handlers.go` (`writeGenerated`, `ensureGitattributes`, `initHandler`,
  `exportHandler`)
- the migrate phase file containing `migrateHandler` (temp write + rename sweep)
- the charter and CLI reference, if the `check` rename is adopted

## Effort estimate

The upgrade itself is a single-pass, one-package change: 8 annotations, 1 flag deletion, 9 call
sites, 2 small structural fixes, no design work left open. Hours, not days. The `check` rename
and the per-file structured diff for `migrate` are separate, larger decisions that need their
own design pass.
