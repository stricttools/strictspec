# Register spec/ as a member; ownership normalization

## Background

rlsbl is adopting single-owner workspace attribution: every file has exactly
one owning member (most specific path wins; a mandatory root member owns the
remainder), the `watch` key is removed, and CI triggering derives from
declared `depends_on` edges (all dependency scopes trigger) plus built-in
rules.

In this workspace, the `spec/` directory — the specification the conformance
suite validates all implementations against — is owned by no member and
reached only via the conformance member's watch globs. Under the new model,
if `spec/` falls to residual root ownership, a spec change would no longer
re-trigger the conformance suite: a regression on exactly the files
conformance exists to police.

## What to consider doing

- Register `spec/` as a dev-node member (its path is the directory; no file
  moves needed).
- Have the conformance member declare `depends_on` on the spec member and on
  the three implementation members, replacing its watch globs.
- Delete the redundant self-glob watch entry on the Python member (it
  duplicates the member's own path and has no effect).
- Add the root member the new model requires (dev node).
- If the specification ever gains consumers of its own versions, promoting
  the spec member to a releasable member (own version line and changelog) is
  a small mechanical change; nothing today needs it.

## Why

Membership plus declared edges is the only arrangement under the new model
in which spec edits re-run conformance. It also gives the spec directory a
first-class identity matching what it is: the reference the implementations
are validated against.
