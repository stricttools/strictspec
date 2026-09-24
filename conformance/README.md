# strictspec conformance

Shared fixtures, runner, and parity checkers over the four conformance targets
(Python, Go, TypeScript, and the internal interpreter), including the constraint
engine and evidence-resolver parity.

This is a `dev_node` project: it has no changelog, is never released
independently, and sits at the edge of the dependency graph. The runner lives in
`run.py` and `harness/`, the fixtures in `fixtures/`; see
[conformance/DESIGN.md](./DESIGN.md) for the design.
