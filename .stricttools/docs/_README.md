+++
title = "README.md"
+++
# strictspec

Define a schema once in TOML, and generate the full toolchain in your language: validation, versioning, migrations, etc. -- First-class support for Go, Python, and TypeScript

strictspec is a schema toolchain that turns one TOML schema into Go, Python and
TypeScript validators reporting identical verdicts, error codes, paths and
messages for JSON, TOML and JSONL files. It is for projects whose config and
data files are read by more than one language — and increasingly written by AI
agents — where a document one reader accepts must never be a document another
reader rejects. Its distinctive property is identity rather than approximation:
values are tagged and lexeme-retaining, and a shared conformance suite holds
every target (Go, Python, TypeScript, and the reference interpreter) to the same
verdict, the same error code, the same path, and the same rendered message text.

## Install

The Go binary is the toolchain. Install it directly:

```
go install github.com/stricttools/strictspec/go/cmd/strictspec@v0
```

`@v0`, not `@latest`: strictspec issues no 1.x tags, so `@latest` cannot resolve
to a real release. Pin an exact version (`@v0.2.5`) when you need one.
Cross-compiled archives are attached to each
[GitHub Release](https://github.com/stricttools/strictspec/releases).

The runtime packages carry the same CLI behind a first-run launcher that
downloads the exact-version binary from the matching GitHub Release, verifies
its SHA-256, and caches it. Installing them performs no network access; only the
first CLI invocation does.

```
uv add strictspec        # Python runtime + `strictspec` console script
npm install strictspec   # TypeScript runtime + `strictspec` bin
```

Go consumers import the runtime from the same module:
`github.com/stricttools/strictspec/go/strictspec`.

## Usage

Generation is file-driven: a `strictspec.toml` manifest names each schema and
the targets it generates.

```
strictspec init      # scaffold strictspec.toml
strictspec gen       # generate validators for every declared target
strictspec check     # schema authoring validity + generated-code freshness
```

Documents are validated against a schema directly, with an explicit choice of
which check phases run — there is no default:

```
strictspec validate --structural-only schema.toml doc.toml
strictspec validate --with-domain-checks schema.toml doc.toml
```

The CLI is `gen`, `check`, `validate`, `migrate`, `export`, `init`, `diff` and
`doc-diff`. Format evolution is declarative: migrations are
closed op lists executed by `strictspec migrate`, never applied automatically by
a receiver, and `strictspec diff` compares a schema at two `format_version`s
over a corpus and emits a certificate.

## Versioning

The Go module, the `strictspec` PyPI package and the `strictspec` npm package
are one release unit: they always carry the same version and are published
together, so the first-run launcher can fetch the binary that pairs with the
runtime shipped beside it. A version therefore moves for all three even when
only one of them changed.

Generated code is paired to its runtime by GENERATED-CODE FORMAT, not by
release. A generated validator declares an integer `GENERATED_CODE_FORMAT` — the
shape of the emitted code, bumped only when that shape changes — and every
runtime declares the inclusive range of formats it reads. Pairing succeeds
whenever the declared format is in that range, whatever release produced the
file, so an ordinary strictspec release does not stop a runtime from reading
validators an earlier release generated. The `GENERATED_BY` release stamp a
generated file carries is informational, and no tool derives a dependency floor
from it. A format outside the range is refused with a message naming the
declared format, the accepted range, both releases, and the remedy: regenerate
with `strictspec gen`, never pin.

## The language reference

The constitution under `.stricttools/docs/` is the language-neutral definition of the schema
language, the constraint vocabulary, the migration op set, the error model, and
the normative appendices. Every backend and the internal interpreter implement
it, and the conformance suite enforces it across every target.
