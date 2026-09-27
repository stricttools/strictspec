"""The options built-ins (stricttools/docs/appendix-options.md).

Three toolchain-shipped built-in schemas -- options-entries (a subject document
under .strictmetadata/options/), options-registry (a tool's registry of the
options it offers), and upstream (.strictmetadata/upstream/upstream.toml) --
plus the readers that validate a document's SHAPE against them and bind it to
typed values. Shape diagnostics are ordinary catalogued STRICTSPEC_* diagnostics
from the shared executor, identical across the Go, Python, and TypeScript
runtimes.

The rules beyond shape (the ranking parser, the registry rules, the
per-namespace entry validator, and the classification) are the private
functions at the end of this module. They stay private because every refusal
they report needs a catalogued STRICTSPEC_* code with a pinned message
template, and appendix-error-codes.md has no area for them yet. The same rules,
over the same shared test cases, exist in the Go and TypeScript runtimes.

This module is imported by the package __init__ after the names it uses are
defined there.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from pathlib import Path

from . import Diagnostic, Program, Value, compile_embedded
from . import _builtins
from . import _doc
from . import _tomldoc

OPTIONS_ENTRIES_SCHEMA = _builtins.OPTIONS_ENTRIES_SCHEMA
OPTIONS_REGISTRY_SCHEMA = _builtins.OPTIONS_REGISTRY_SCHEMA
UPSTREAM_SCHEMA = _builtins.UPSTREAM_SCHEMA

# Where a repository's option subject documents live, relative to its root.
OPTIONS_DIR = ".strictmetadata/options"
# Where a fork declares its upstream, relative to the repository root.
UPSTREAM_FILE = ".strictmetadata/upstream/upstream.toml"
# The options directory's ownership manifest; not a subject document.
_OPTIONS_MANIFEST_FILE = "manifest.toml"

_programs: dict[str, Program] = {}


def _builtin(file_name: str, src: str) -> Program:
    p = _programs.get(file_name)
    if p is None:
        try:
            p = compile_embedded({file_name: src}, file_name)
        except ValueError as e:
            # The built-ins are part of this runtime; one failing the
            # meta-schema is a strictspec bug, never a consumer condition.
            raise RuntimeError(
                f"strictspec: built-in schema {file_name} fails the meta-schema: {e}"
            ) from e
        _programs[file_name] = p
    return p


def options_entries_program() -> Program:
    """The compiled built-in options-entries schema."""
    return _builtin("options-entries.schema.toml", OPTIONS_ENTRIES_SCHEMA)


def options_registry_program() -> Program:
    """The compiled built-in options-registry schema."""
    return _builtin("options-registry.schema.toml", OPTIONS_REGISTRY_SCHEMA)


def upstream_program() -> Program:
    """The compiled built-in upstream schema."""
    return _builtin("upstream.schema.toml", UPSTREAM_SCHEMA)


@dataclass(frozen=True)
class OptionDeclaration:
    """One [[option]] of a tool's options registry."""

    name: str
    subject: str
    values: str
    default: str
    scope: str
    description: str


@dataclass(frozen=True)
class OptionsRegistry:
    """A tool's shape-valid options registry."""

    options: tuple[OptionDeclaration, ...]


@dataclass(frozen=True)
class OptionsEntry:
    """One [[entry]] of a subject document. file is the subject document's file
    name (for example "changelog.toml") and index the entry's position in it;
    scope is None when the entry carries none.
    """

    file: str
    index: int
    id: str
    scope: str | None
    current: str
    ideal: str
    reason: str


@dataclass(frozen=True)
class Upstream:
    """A fork's shape-valid upstream declaration."""

    host: str
    owner: str
    repo: str
    branch: str


@dataclass(frozen=True)
class FileDiagnostics:
    """The shape diagnostics of one document, attributed to its file."""

    file: str
    diagnostics: tuple[Diagnostic, ...]


@dataclass(frozen=True)
class OptionsEntriesLoad:
    """Every subject document of a repository's options directory: the entries
    of the shape-valid documents, and the diagnostics of every document that is
    not. A document with diagnostics contributes no entries.
    """

    entries: tuple[OptionsEntry, ...]
    invalid: tuple[FileDiagnostics, ...]


def _validate_toml(p: Program, input: bytes) -> tuple[Value | None, tuple[Diagnostic, ...]]:
    res = p.validate(input, "toml")
    if not res.valid:
        return None, res.diagnostics
    return Value(_tomldoc.parse(input).root, _doc.FORMAT_TOML), ()


def _str(rec: Value, key: str) -> str | None:
    f, ok = rec.field(key)
    if not ok:
        return None
    s, _ = f.string()
    return s


def read_options_registry(input: bytes) -> tuple[OptionsRegistry | None, tuple[Diagnostic, ...]]:
    """Validate a registry document's shape against the built-in
    options-registry schema and bind it. Non-empty diagnostics mean None.
    """
    root, diags = _validate_toml(options_registry_program(), input)
    if root is None:
        return None, diags
    opts, _ = root.field("option")
    return (
        OptionsRegistry(
            options=tuple(
                OptionDeclaration(
                    name=_str(o, "name"),
                    subject=_str(o, "subject"),
                    values=_str(o, "values"),
                    default=_str(o, "default"),
                    scope=_str(o, "scope"),
                    description=_str(o, "description"),
                )
                for o in opts.items()
            )
        ),
        (),
    )


def load_options_registry(path: str | os.PathLike) -> tuple[OptionsRegistry | None, tuple[Diagnostic, ...]]:
    """Read and shape-validate the registry file at path. An unreadable file
    raises OSError.
    """
    return read_options_registry(Path(path).read_bytes())


def read_options_entries(file: str, input: bytes) -> tuple[tuple[OptionsEntry, ...], tuple[Diagnostic, ...]]:
    """Validate one subject document's shape against the built-in
    options-entries schema and bind its entries, attributing each to file (the
    document's file name, for example "changelog.toml"). Non-empty diagnostics
    mean no entries.
    """
    root, diags = _validate_toml(options_entries_program(), input)
    if root is None:
        return (), diags
    items, _ = root.field("entry")
    return (
        tuple(
            OptionsEntry(
                file=file,
                index=i,
                id=_str(e, "id"),
                scope=_str(e, "scope"),
                current=_str(e, "current"),
                ideal=_str(e, "ideal"),
                reason=_str(e, "reason"),
            )
            for i, e in enumerate(items.items())
        ),
        (),
    )


def load_options_entries(repo_root: str | os.PathLike) -> OptionsEntriesLoad:
    """Read every subject document of repo_root's .strictmetadata/options/
    directory, in file-name order: each *.toml file other than the directory's
    manifest.toml. A missing directory means no entries. An unreadable
    directory or file raises OSError.
    """
    d = Path(repo_root) / OPTIONS_DIR
    if not d.exists():
        return OptionsEntriesLoad(entries=(), invalid=())
    names = sorted(
        p.name
        for p in d.iterdir()
        if not p.is_dir() and p.name.endswith(".toml") and p.name != _OPTIONS_MANIFEST_FILE
    )
    entries: list[OptionsEntry] = []
    invalid: list[FileDiagnostics] = []
    for n in names:
        es, diags = read_options_entries(n, (d / n).read_bytes())
        if diags:
            invalid.append(FileDiagnostics(file=n, diagnostics=diags))
            continue
        entries.extend(es)
    return OptionsEntriesLoad(entries=tuple(entries), invalid=tuple(invalid))


def read_upstream(input: bytes) -> tuple[Upstream | None, tuple[Diagnostic, ...]]:
    """Validate an upstream document's shape against the built-in upstream
    schema and bind it. Non-empty diagnostics mean None.
    """
    root, diags = _validate_toml(upstream_program(), input)
    if root is None:
        return None, diags
    return (
        Upstream(
            host=_str(root, "host"),
            owner=_str(root, "owner"),
            repo=_str(root, "repo"),
            branch=_str(root, "branch"),
        ),
        (),
    )


def load_upstream(repo_root: str | os.PathLike) -> tuple[Upstream | None, bool, tuple[Diagnostic, ...]]:
    """Read repo_root's .strictmetadata/upstream/upstream.toml, returning
    (upstream, found, diagnostics). found is False when the repository declares
    no upstream (the file is absent). An unreadable file raises OSError.
    """
    path = Path(repo_root) / UPSTREAM_FILE
    if not path.exists():
        return None, False, ()
    u, diags = read_upstream(path.read_bytes())
    return u, True, diags


# --- the rules beyond shape (private; see the module docstring) --------------

_NON_EXISTENT = "non-existent"
_SCOPE_NONE = "none"
_VALUE_NAME = re.compile(r"[a-z0-9-]+")

RULE_RANKING_MALFORMED = "ranking-malformed"
RULE_RANKING_INVALID_VALUE_NAME = "ranking-invalid-value-name"
RULE_RANKING_RESERVED_VALUE = "ranking-reserved-value"
RULE_RANKING_DUPLICATE_VALUE = "ranking-duplicate-value"
RULE_REGISTRY_DEFAULT_UNDECLARED = "registry-default-undeclared"
RULE_REGISTRY_SUBJECT_INVALID = "registry-subject-invalid"
RULE_ENTRY_UNKNOWN_OPTION = "entry-unknown-option"
RULE_ENTRY_WRONG_SUBJECT = "entry-wrong-subject"
RULE_ENTRY_SCOPE_NOT_ACCEPTED = "entry-scope-not-accepted"
RULE_ENTRY_UNDECLARED_CURRENT = "entry-undeclared-current"
RULE_ENTRY_UNDECLARED_IDEAL = "entry-undeclared-ideal"
RULE_ENTRY_REDUNDANT = "entry-redundant"
RULE_ENTRY_CURRENT_ABOVE_IDEAL = "entry-current-above-ideal"
RULE_ENTRY_DUPLICATE = "entry-duplicate"

CLASS_SETTLED = "settled"
CLASS_DEBT = "debt"
CLASS_WAITING_ON_TOOL = "waiting-on-tool"


@dataclass(frozen=True)
class _Refusal:
    """One refusal. For registry refusals file is "" and index is the [[option]]
    position; for entry refusals file and index locate the entry. value is the
    offending token or value; detail carries what the fix needs (the right
    subject file, the first occurrence of a duplicate, the ranking string).
    """

    rule: str
    file: str = ""
    index: int = 0
    value: str = ""
    detail: str = ""


@dataclass(frozen=True)
class _Ranking:
    """A parsed ranking string. Level 0 is the strongest; values of equal rank
    share a level.
    """

    values: tuple[str, ...]
    level: dict[str, int]


def _parse_ranking(s: str) -> tuple[_Ranking | None, list[_Refusal]]:
    """Parse a ranking string: value names separated by single spaces around
    `>` (stronger than) or `=` (equal rank), for example
    "npm = pypi = jsr > none". A string that is not that alternation is one
    ranking-malformed refusal; otherwise every value name outside the grammar,
    every use of the reserved non-existent, and every repeated value is refused.
    """
    toks = s.split(" ")
    malformed = [_Refusal(rule=RULE_RANKING_MALFORMED, value=s)]
    if len(toks) % 2 == 0:
        return None, malformed
    for i, t in enumerate(toks):
        is_op = t in (">", "=")
        if t == "" or (i % 2 == 1) != is_op:
            return None, malformed
    values: list[str] = []
    level: dict[str, int] = {}
    refusals: list[_Refusal] = []
    lv = 0
    for i in range(0, len(toks), 2):
        if i > 0 and toks[i - 1] == ">":
            lv += 1
        v = toks[i]
        if not _VALUE_NAME.fullmatch(v):
            refusals.append(_Refusal(rule=RULE_RANKING_INVALID_VALUE_NAME, value=v))
        elif v == _NON_EXISTENT:
            refusals.append(_Refusal(rule=RULE_RANKING_RESERVED_VALUE, value=v))
        elif v in level:
            refusals.append(_Refusal(rule=RULE_RANKING_DUPLICATE_VALUE, value=v))
        else:
            values.append(v)
            level[v] = lv
    if refusals:
        return None, refusals
    return _Ranking(values=tuple(values), level=level), []


@dataclass(frozen=True)
class _CheckedOption:
    decl: OptionDeclaration
    ranking: _Ranking | None


def _valid_subject(s: str) -> bool:
    """A registry subject names a subject file the entry loader reads: a
    value-name-grammar stem that is not the options directory's own manifest.
    """
    return bool(_VALUE_NAME.fullmatch(s)) and s + ".toml" != _OPTIONS_MANIFEST_FILE


def _check_registry(reg: OptionsRegistry) -> tuple[dict[str, _CheckedOption] | None, list[_Refusal]]:
    """Apply the registry rules to a shape-valid registry: each option's values
    parse as a ranking, its default is a declared value, and its subject is a
    valid subject file stem. (Option names are unique by the built-in schema's
    unique-by constraint.) Any refusal means None.
    """
    out: dict[str, _CheckedOption] = {}
    refusals: list[_Refusal] = []
    for i, o in enumerate(reg.options):
        rk, rr = _parse_ranking(o.values)
        for r in rr:
            refusals.append(_Refusal(rule=r.rule, index=i, value=r.value, detail=o.values))
        if rk is not None and o.default not in rk.level:
            refusals.append(
                _Refusal(rule=RULE_REGISTRY_DEFAULT_UNDECLARED, index=i, value=o.default, detail=o.values)
            )
        if not _valid_subject(o.subject):
            refusals.append(_Refusal(rule=RULE_REGISTRY_SUBJECT_INVALID, index=i, value=o.subject))
        out[o.name] = _CheckedOption(decl=o, ranking=rk)
    if refusals:
        return None, refusals
    return out, []


def _validate_namespace(
    tool: str, reg: dict[str, _CheckedOption], entries: tuple[OptionsEntry, ...] | list[OptionsEntry]
) -> tuple[list[tuple[OptionsEntry, str]], list[_Refusal]]:
    """Judge the entries of one tool's namespace (`<tool>:*`) against that
    tool's checked registry, in the order given. Entries of other namespaces
    are not judged. Returns the accepted entries with their classification, and
    every refusal; an entry with any refusal is not classified.
    """
    prefix = tool + ":"
    first: dict[tuple[str, str | None], OptionsEntry] = {}
    accepted: list[tuple[OptionsEntry, str]] = []
    refusals: list[_Refusal] = []
    for e in entries:
        if not e.id.startswith(prefix):
            continue
        before = len(refusals)

        def refuse(rule: str, value: str, detail: str = "") -> None:
            refusals.append(_Refusal(rule=rule, file=e.file, index=e.index, value=value, detail=detail))

        k = (e.id, e.scope)
        if k in first:
            f = first[k]
            refuse(RULE_ENTRY_DUPLICATE, e.id, f"{f.file} entry {f.index}")
        else:
            first[k] = e
        opt = reg.get(e.id[len(prefix):])
        if opt is None:
            refuse(RULE_ENTRY_UNKNOWN_OPTION, e.id)
            continue
        want = opt.decl.subject + ".toml"
        if e.file != want:
            refuse(RULE_ENTRY_WRONG_SUBJECT, e.file, want)
        if e.scope is not None and opt.decl.scope == _SCOPE_NONE:
            refuse(RULE_ENTRY_SCOPE_NOT_ACCEPTED, e.scope)
        level = opt.ranking.level
        current_ok = e.current in level
        if not current_ok:
            refuse(RULE_ENTRY_UNDECLARED_CURRENT, e.current, opt.decl.values)
        waiting = e.ideal == _NON_EXISTENT
        ideal_ok = waiting or e.ideal in level
        if not ideal_ok:
            refuse(RULE_ENTRY_UNDECLARED_IDEAL, e.ideal, opt.decl.values)
        if current_ok and ideal_ok:
            if e.current == opt.decl.default and e.ideal == opt.decl.default:
                refuse(RULE_ENTRY_REDUNDANT, e.current)
            elif not waiting and level[e.current] < level[e.ideal]:
                refuse(RULE_ENTRY_CURRENT_ABOVE_IDEAL, e.current, e.ideal)
        if len(refusals) > before:
            continue
        if waiting:
            cls = CLASS_WAITING_ON_TOOL
        elif level[e.current] == level[e.ideal]:
            cls = CLASS_SETTLED
        else:
            cls = CLASS_DEBT
        accepted.append((e, cls))
    return accepted, refusals
