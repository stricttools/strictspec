# strictspec (Python)

Python runtime library and CLI stub for [strictspec](https://github.com/smm-h/strictspec),
a strict multi-language schema toolchain for declarative spec files.

This is a placeholder skeleton. The runtime (document I/O, diagnostics, tagged
values, constraint engine) and the CLI launcher stub land in a later phase.

## Versioning

This package, the `strictspec` npm package and the Go module
`github.com/smm-h/strictspec/go` are one release unit: they always carry the
same version and are published together, so the runtime always matches the
toolchain that generated its code — and the first-run launcher can fetch the
binary that pairs with it. A version therefore moves for all three even when
only one of them changed.

## Development

```bash
uv sync
uv run pytest
```
