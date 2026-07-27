# strictspec conformance

Shared fixtures, runner, and parity checkers over the four conformance targets
(Python, Go, TypeScript, and the internal interpreter), including the constraint
engine and evidence-resolver parity.

This is a `dev_node` project: it has no changelog, is never released
independently, and sits at the edge of the dependency graph. The runner and
fixtures land in a later phase; see [conformance/DESIGN.md](./DESIGN.md).
