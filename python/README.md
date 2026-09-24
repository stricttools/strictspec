# strictspec (Python)

Define a schema once in TOML, and generate the full toolchain in your language: validation, versioning, migrations, etc. -- First-class support for Go, Python, and TypeScript.

This package is the Python runtime library and CLI for
[strictspec](https://github.com/stricttools/strictspec).

It holds the runtime (document I/O, diagnostics, tagged values, constraint
engine) and the `strictspec` CLI launcher.

## Versioning

This package, the `strictspec` npm package and the Go module
`github.com/stricttools/strictspec/go` are one release unit: they always carry the
same version and are published together, so the runtime always matches the
toolchain that generated its code — and the first-run launcher can fetch the
binary that pairs with it. A version therefore moves for all three even when
only one of them changed.

## Development

```bash
uv sync
uv run pytest
```
