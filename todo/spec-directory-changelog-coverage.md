# spec/ has no changelog coverage — decide its owner

## Context

The workspace's root member is a dev node (`dev_only = true`,
`releasable = false`), so root files need no changelog coverage. But the
repository root owns `spec/` — the normative specification (DESIGN.md and
the appendices) that all three language packages implement. A change to
`spec/` changes the product's contract for every consumer, yet it is
currently changelog-exempt while sibling code changes are covered.

A fleet-wide evidence review of workspace roots rated this repository's
root the highest coverage-deservingness of any dev-node root, precisely
because of `spec/` — and because this workspace has exactly one
releasable (`strictspec`, spanning all three languages), so unlike
multi-releasable workspaces there is an unambiguous owner available.

## Problem

The product's normative definition can change without any changelog
entry, release-notes mention, or coverage enforcement.

## Solutions

### Option A — declare spec/ as its own workspace member

Add a member with `path = "spec"`, target `spec` (rlsbl has a `spec`
release target for exactly this shape), `releasable = "strictspec"`.

- Pros: precise ownership — exactly the normative content gets coverage;
  the root member stays an honest dev node (the root as a whole is a
  container that builds nothing); no root-releasable requirements are
  triggered.
- Cons: one more member entry; `spec/` may need whatever version-file
  convention the `spec` target expects.

### Option B — flip the root member into the releasable

Set the root member's `releasable = "strictspec"`; the loader then
requires that releasable to declare its `tag_format` explicitly.

- Pros: every root file (not just `spec/`) gets coverage; smallest
  conceptual change.
- Cons: coverage extends to root scaffolding that does not deserve it;
  requires the explicit tag-format declaration; the root is a container,
  so this cuts against the structural criterion (root joins a releasable
  only when the root itself is consumed).

### Option C — keep the exemption

- Pros: zero work.
- Cons: the normative contract stays uncovered; the mismatch that
  prompted this todo persists.

## Affected files

`.rlsbl-monorepo/workspace.toml` (member entry or root flip);
possibly a version file under `spec/` (Option A);
`.rlsbl-monorepo/releasables/strictspec/` state is unaffected either way.

## Effort

Small — a workspace.toml edit plus a check run, whichever option is
chosen.
