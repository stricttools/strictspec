# strictspec (TypeScript)

TypeScript runtime library and CLI stub for
[strictspec](https://github.com/smm-h/strictspec), published to npm as
`strictspec`.

This is a placeholder skeleton (currently `"private": true` so nothing can
publish before the first real release). The runtime (lossless JSON/TOML/JSONL
parsers producing tagged document-model values, diagnostics, constraint engine)
and the CLI launcher stub land in a later phase; see [ts/DESIGN.md](./DESIGN.md).

## Versioning

This package, the `strictspec` PyPI package and the Go module
`github.com/smm-h/strictspec/go` are one release unit: they always carry the
same version and are published together, so the runtime always matches the
toolchain that generated its code — and the first-run launcher can fetch the
binary that pairs with it. A version therefore moves for all three even when
only one of them changed.

## Development

```bash
npm install
npm test
```
