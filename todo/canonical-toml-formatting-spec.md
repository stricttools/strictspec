# Author the document-level canonical TOML formatting specification

## Context

The spec/ tree pins value-level rendering (the write-side value table in
the rendering appendix: float form, string escaping, lexeme retention for
untouched values) and a diagnostic path grammar. What does not exist
anywhere is a DOCUMENT-LEVEL canonical formatting specification for TOML
1.0: the single prescribed output form covering table style (standard vs
inline, when each), key ordering policy, indentation, blank-line policy,
key-value spacing, array wrapping rules, string style selection, and
date-time precision (always-write-seconds and related choices).

Ecosystem tooling treats such a spec as the authority that formatters
conform to; a Go-side comment-preserving TOML editor (go-toml-edit, a
dependency of this project's Go implementation) has a formatter whose
conformance work is explicitly blocked on this spec existing, and carries
an inventory of its current hard-coded formatting decisions that can serve
as raw material for the authoring.

## Problem

The spec has no author and no timeline, so everything downstream of it
waits on an unscheduled event. Formatter behavior across the ecosystem is
de facto (whatever each implementation does) rather than prescribed.

## Proposed work

Author the canonical form as a spec/ appendix (or standalone spec file)
in this project's normal spec style: one prescribed output per input
document, no options. Decisions to make while authoring, roughly: table
style policy; key ordering (document order vs sorted vs preserved);
indentation of table bodies and nested arrays; blank-line policy between
and within tables; string style selection rules (when literal vs basic vs
multi-line); array single-line vs multi-line threshold and trailing-comma
policy; date-time rendering precision. The existing value-level appendix
rules are incorporated by reference, not restated.

Write it as policy (what canonical form SHOULD be), not as a description
of any existing formatter's behavior.

## Affected files

- A new spec/ document (appendix or standalone), plus index/registry
  updates per the spec tree's conventions.
- Changelog entry (user-facing: a new normative spec section).

## Effort

Medium: the decisions are the work; the writing is small once they are
made. No code changes in this project are required by the authoring
itself (conformance work in formatters is downstream and belongs to the
formatter projects).
