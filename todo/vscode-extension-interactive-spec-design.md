# A VS Code extension for interactive spec design

## Context

strictspec turns one TOML schema into Go, Python, and TypeScript
validators that report identical verdicts, codes, paths, and message text.
The Go binary (`go/cmd/strictspec`, built on strictcli) is the whole
toolchain: `gen`, `check`, `validate`, `migrate`, `export`, `init`, `diff`,
and `doc-diff`. The schema language is defined by the constitution under
`stricttools/docs/` (surface syntax in `appendix-surface-syntax.md`: the
header, `[types.<Name>]` type sites, `fields`, constraints, `when`
conditions, imports, enum sourcing, and migration files). A schema file is
itself checked against a built-in meta-schema (authoring diagnostics in
`go/internal/emit/load.go`), and `strictspec.toml` is a document of a
toolchain-shipped schema.

Designing a spec is an iterative loop: write a type site, run `check`,
write a sample document, run `validate`, look at generated code, write a
migration, run `migrate --dry-run`, run `diff`. Every step is a separate
command, and the feedback (paths such as `types.Foo.fields.bar`, rendered
per `appendix-rendering.md`) has to be mapped back to TOML lines by hand.

The toolchain already has what an editor integration needs: lossless,
lexeme-retaining document parsers with source positions (`go/internal/doc`
`Position`, used by `go/internal/tomldoc` and `go/internal/jsondoc`), a
path-addressed diagnostic model (`go/internal/diag`), the canonical
renderer (`go/internal/render`), the interpreter (`go/internal/interp`),
emitters (`go/internal/emit`), the JSON Schema exporter
(`go/internal/export`), and the migration, diff, and doc-diff engines.

## Problem

- No editor feedback while authoring a schema, a migration file, a
  manifest, or a document governed by a schema.
- The structure of a spec (named types, references between them, unions,
  imports across schema files) is only visible by reading TOML.
- The effect of a schema change on documents, on generated code, and on
  format evolution is only visible after running several commands.

## Proposal

A language server and a set of design views, shipped together.

### A language server in Go, inside the toolchain

A Language Server Protocol server hosted by the strictspec binary (a new
command whose name is to be chosen, declared through strictcli as a command
that owns stdout, speaking JSON-RPC over stdio), reusing the internal
packages directly, so the editor sees the same verdicts, codes, and
rendered messages as every other target.

- **Diagnostics on schema files**: meta-schema authoring diagnostics and
  `check` results, each diagnostic's path resolved to a source range
  through the lossless TOML parser's positions. A path that cannot be
  resolved to a node (for example, a missing required key) attaches to the
  nearest enclosing node that exists, and that rule is written down and
  tested rather than improvised.
- **Diagnostics on documents**: for every document the manifest associates
  with a schema, run the structural checks, and the domain checks when the
  user has selected them. The association comes only from explicit
  declarations (the manifest, or an explicit per-workspace setting); the
  server never guesses a schema from a file name. A document with no
  declared schema gets no strictspec diagnostics, and a document claimed by
  two declarations is an error naming both.
- **Diagnostics on migration files and on `strictspec.toml`**, against
  their built-in schemas.
- **Hover**: on a schema key, its meaning from the constitution (the
  surface-syntax and semantics appendices); on a `type` value naming a
  type, that type's definition; on a document field, the field's schema
  site and description.
- **Go to definition and references** for named types, across imports.
- **Completion**: the closed sets of the language (scalar kinds, type
  kinds, constraint vocabulary, `when` predicates, migration ops) at the
  positions where they are legal; named types where a type reference is
  expected; field names in a document from its schema. The sets come from
  the toolchain's own tables, never from a copy in the extension.
- **Code actions** tied to diagnostic codes, where the constitution
  defines one fix (for example the did-you-mean suggestion from
  `appendix-rendering.md` for an unknown key). A fix whose outcome is not
  determined is not offered.
- **Formatting**, only if the toolchain defines a canonical form for schema
  TOML; otherwise out of scope.

### The interactive design surface (webviews)

A VS Code extension in TypeScript that starts the server
(`vscode-languageclient`) and adds design views, each backed by requests
to the server (custom LSP methods), so no strictspec logic lives in
TypeScript.

1. **Type graph**: named types as nodes, fields and references as edges,
   unions and nullable shown as such, imports as clusters. Selecting a node
   reveals the type site in the TOML; editing is text-first.
2. **Type-site editor**: a form for one type site whose controls are
   generated from the meta-schema (the legal keys for that `type`, their
   value sets), writing back through `WorkspaceEdit`s produced by the
   server's comment-preserving TOML writer, so hand-written comments and
   layout are kept. The TOML file stays the only source of truth; the form
   never holds state of its own.
3. **Live sample panel**: a scratch document (JSON, TOML, or JSONL per the
   schema's `document_syntax`) validated on every keystroke with the
   rendered diagnostics listed beside it, plus buttons to save it as a
   valid or invalid fixture.
4. **Generated code preview**: the output of the emitters for each declared
   target, rendered read-only as a diff against the committed generated
   files, which also shows whether `gen` is stale.
5. **Evolution view**: for a schema at two `format_version`s, the migration
   file, a `migrate --dry-run` per-file diff over the documents the
   manifest declares, and the `diff` certificate summarized.
6. **JSON Schema export preview**: the `export` output, labeled advisory
   and lossy, as the constitution says.

## Solutions with pros and cons

1. **Language server plus webviews (proposed above)**.
   - Pro: one source of truth for every verdict; editor features work in
     any LSP client (the server can serve other editors too); webviews add
     design views without duplicating logic.
   - Con: the largest build; custom LSP methods for the design views need
     a small documented protocol.
2. **Webviews only, calling the CLI** (`strictspec check`, `validate`,
   `gen`, and so on with `--json`).
   - Pro: no new server; uses the existing command surface.
   - Con: a process per request; diagnostics carry paths, and turning them
     into ranges would need a position lookup in TypeScript, a second TOML
     and JSON parser that can disagree with the toolchain's.
3. **Rely on JSON Schema export plus existing generic TOML and JSON
   extensions** (the `jsonValidation` contribution point).
   - Pro: almost no code.
   - Con: the export is lossy by design, so the editor would accept
     documents the validators reject and vice versa, which is the drift
     strictspec exists to remove. Not acceptable as the design surface.

Solution 1 is the most correct.

## Distribution

- The extension is TypeScript; the server is the Go toolchain binary. The
  extension needs the binary: either platform-specific extension packages
  bundling it (both registries support per-platform packages), or the
  first-run download and SHA-256 verification scheme the runtime packages
  already use, pinned to the extension's matching version. The extension
  must refuse a binary whose version does not match the one it expects,
  naming both.
- Publish to both the **VS Code Marketplace** (`vsce publish`) and
  **Open VSX** (`ovsx publish`), so VS Code and its derivatives (VSCodium,
  Cursor, and others that install from Open VSX) all get it.
- The project releases through rlsbl, which has no VS Code extension
  publishing target yet; the release path for the extension is to be
  designed (a new rlsbl target, or a separate step).
- The extension name, publisher ID, and package name are to be chosen.

## Affected files and new components

- `go/cmd/strictspec/main.go` and `handlers.go`: the language server
  command.
- New Go package for the server (document store, path-to-range resolution,
  feature handlers, custom design-view methods), with tests driving it over
  an in-memory JSON-RPC pipe.
- `go/internal/diag` and `go/internal/doc`: a path-to-position resolver if
  one does not exist.
- `go/internal/codes`: exposing the closed sets for completion from their
  existing tables.
- New: the TypeScript extension directory (manifest, client, webview code,
  build step), placed as a new project in the rlsbl monorepo or in a
  separate repository, to be decided.
- `stricttools/docs/`: a page documenting the server and the extension, and
  the custom protocol methods, once they exist.

## Effort estimate

- Server with schema and document diagnostics, hover, definition, and
  completion: four to six days.
- Extension shell, binary acquisition, and both registries: one to two
  days.
- Type graph and live sample panel: three to four days.
- Type-site form editor with comment-preserving write-back: three to five
  days.
- Generated code preview and evolution view: three to four days.
