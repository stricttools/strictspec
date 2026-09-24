# strictspec (TypeScript)

Define a schema once in TOML, and generate the full toolchain in your language: validation, versioning, migrations, etc. -- First-class support for Go, Python, and TypeScript.

This package is the TypeScript runtime library and CLI for
[strictspec](https://github.com/stricttools/strictspec), published to npm as
`strictspec`.

It holds the runtime (lossless JSON/TOML/JSONL parsers producing tagged
document-model values, diagnostics, constraint engine) and the `strictspec` CLI
launcher; see [ts/DESIGN.md](./DESIGN.md) for the design.

## Versioning

This package, the `strictspec` PyPI package and the Go module
`github.com/stricttools/strictspec/go` are one release unit: they always carry the
same version and are published together, so the runtime always matches the
toolchain that generated its code — and the first-run launcher can fetch the
binary that pairs with it. A version therefore moves for all three even when
only one of them changed.

## Development

```bash
npm install
npm test
```
