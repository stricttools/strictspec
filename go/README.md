# strictspec (Go)

Define a schema once in TOML, and generate the full toolchain: validation, versioning, migrations, etc. -- for Go.

This module is the strictspec toolchain and Go runtime; its module path is
`github.com/stricttools/strictspec/go`.

It holds the toolchain (generator, interpreter, migration engine, constraint
engine, diff engine, `cmd/strictspec` CLI) and the Go runtime; see
[go/DESIGN.md](./DESIGN.md) for the design.

## Versioning

The toolchain and the runtime are one Go module and one release unit: they
always carry the same version, so a runtime always matches the toolchain that
generated its code.

## Development

```bash
go test ./...
```
