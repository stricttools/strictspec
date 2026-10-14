+++
title = "README.md"
+++
# strictspec

Define a schema once in TOML, and generate the full toolchain: validation, versioning, migrations, etc. -- for Go

strictspec is a schema toolchain that turns one TOML schema into a Go validator
reporting verdicts, error codes, paths, and messages for JSON, TOML, and JSONL
files. It is for projects whose config and data files are increasingly written
by AI agents, where a document must be accepted or rejected for one stated
reason, every time. Its distinctive property is exactness rather than
approximation: values are tagged and lexeme-retaining, and the generated
validator and the reference interpreter run one shared executor, held by the
test suite to the same verdict, the same error code, the same path, and the
same rendered message text.

## Install

The Go binary is the toolchain. Install it directly:

```
go install github.com/stricttools/strictspec/go/cmd/strictspec@v0
```

`@v0`, not `@latest`: strictspec issues no 1.x tags, so `@latest` cannot resolve
to a real release. Pin an exact version (`@v0.2.5`) when you need one.
Cross-compiled archives are attached to each
[GitHub Release](https://github.com/stricttools/strictspec/releases).

Generated validators import the runtime from the same module:
`github.com/stricttools/strictspec/go/strictspec`.

## Usage

Generation is file-driven: a `strictspec.toml` manifest names each schema and
the Go files it generates (`lang = "go"`, the only generation target).

```
strictspec init --manifest strictspec.toml   # scaffold strictspec.toml
strictspec gen --manifest strictspec.toml    # generate a validator for every declared target
strictspec check --manifest strictspec.toml  # schema authoring validity + generated-code freshness
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

The Go module is the release unit: the CLI and the runtime always carry the
same version. strictspec 0.7.0 was the last release with Python and TypeScript
runtimes (the `strictspec` PyPI and npm packages); their validators keep working
against those runtimes, and they receive no further releases.

Generated code is paired to its runtime by GENERATED-CODE FORMAT, not by
release. A generated validator declares an integer `GENERATED_CODE_FORMAT` — the
shape of the emitted code, bumped only when that shape changes — and the
runtime declares the inclusive range of formats it reads. Pairing succeeds
whenever the declared format is in that range, whatever release produced the
file, so an ordinary strictspec release does not stop a runtime from reading
validators an earlier release generated. The `GENERATED_BY` release stamp a
generated file carries is informational, and no tool derives a dependency floor
from it. A format outside the range is refused with a message naming the
declared format, the accepted range, both releases, and the remedy: regenerate
with `strictspec gen`, never pin.

## The language reference

The constitution under `.strictmetadata/docs/` is the definition of the schema
language, the constraint vocabulary, the migration op set, the error model, and
the normative appendices. The generated Go validators and the reference
interpreter implement it, and the validation fixtures under
`go/testdata/fixtures/` hold both to it.
