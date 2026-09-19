# strictspec (Go)

Define a schema once in TOML, and generate the full toolchain in your language: validation, versioning, migrations, etc. -- First-class support for Go, Python, and TypeScript.

This module is the strictspec toolchain and Go runtime; its module path is
`github.com/stricttools/strictspec/go`.

This is a placeholder skeleton. The toolchain (generator, interpreter, migration
engine, constraint engine, diff engine, `cmd/strictspec` CLI) and the Go runtime
land in later phases; see [go/DESIGN.md](./DESIGN.md).

## Versioning

The Go module, the `strictspec` PyPI package and the `strictspec` npm package
are one release unit: they always carry the same version and are published
together, so a runtime always matches the toolchain that generated its code.
A version therefore moves for all three even when only one of them changed.

## Development

```bash
go test ./...
```
