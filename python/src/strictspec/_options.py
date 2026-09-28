"""The options built-ins (stricttools/docs/appendix-options.md).

Three toolchain-shipped built-in schemas -- options-entries (a subject document
under .strictmetadata/options/), options-registry (a tool's registry of the
options it offers), and upstream (.strictmetadata/upstream/upstream.toml) --
plus the readers that validate a document's SHAPE against them and bind it to
typed values. Shape diagnostics are ordinary catalogued STRICTSPEC_* diagnostics
from the shared executor, identical across the Go, Python, and TypeScript
runtimes.

The rules beyond shape (the ranking parser, the registry rules, the
per-namespace entry validator, and the classification) follow the readers.
Every refusal is a catalogued STRICTSPEC_OPTIONS_* diagnostic
(appendix-error-codes.md, section 21a) rendered from its pinned template. The
same rules, over the same shared test cases, exist in the Go and TypeScript
runtimes.

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
from . import _diag
from . import _doc
from . import _render
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
    """One [[option]] of a tool's options registry. requires names the other
    options of the same registry the option depends on.
    """

    name: str
    subject: str
    values: str
    default: str
    scope: str
    requires: tuple[str, ...]
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
                    requires=tuple(r.string()[0] for r in o.field("requires")[0].items()),
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


# --- the rules beyond shape ---------------------------------------------------

# The reserved ideal value meaning "the right value is one the tool does not
# offer yet". A ranking never declares it.
OPTIONS_NON_EXISTENT = "non-existent"
_SCOPE_NONE = "none"
# The one value that switches an option off: an entry setting an option's
# current or ideal to it requires every dependent of the option to be off too.
# No other value affects dependents.
_OFF = "off"
_VALUE_NAME = re.compile(r"[a-z0-9-]+")

# The ranking classification of an accepted entry: current and ideal have equal
# rank (settled), current ranks below ideal (debt), or ideal is non-existent, a
# value the tool does not offer yet (waiting on the tool).
OPTIONS_SETTLED = "settled"
OPTIONS_DEBT = "debt"
OPTIONS_WAITING_ON_TOOL = "waiting-on-tool"


@dataclass(frozen=True)
class OptionsRanking:
    """A parsed ranking string. values lists the declared values in the order
    written; level maps each to its rank, 0 being the strongest, with values of
    equal rank sharing a level.
    """

    values: tuple[str, ...]
    level: dict[str, int]

    def declares(self, v: str) -> bool:
        return v in self.level


def _public(ds: list[_diag.Diagnostic]) -> tuple[Diagnostic, ...]:
    return tuple(Diagnostic(code=d.code, path=d.path.render(), message=_render.render(d)) for d in ds)


def _str_val(s: str) -> _diag.Slot:
    return _diag.SlotValue(_diag.StringVal(s))


def _parse_ranking(s: str, at: _diag.Path) -> tuple[OptionsRanking | None, list[_diag.Diagnostic]]:
    toks = s.split(" ")
    malformed = [_diag.Diagnostic("STRICTSPEC_OPTIONS_RANKING_MALFORMED", at, {"ranking": _str_val(s)})]
    if len(toks) % 2 == 0:
        return None, malformed
    for i, t in enumerate(toks):
        is_op = t in (">", "=")
        if t == "" or (i % 2 == 1) != is_op:
            return None, malformed
    values: list[str] = []
    level: dict[str, int] = {}
    ds: list[_diag.Diagnostic] = []

    def refuse(code: str, v: str) -> None:
        ds.append(_diag.Diagnostic(code, at, {"value": _str_val(v)}))

    lv = 0
    for i in range(0, len(toks), 2):
        if i > 0 and toks[i - 1] == ">":
            lv += 1
        v = toks[i]
        if not _VALUE_NAME.fullmatch(v):
            refuse("STRICTSPEC_OPTIONS_RANKING_VALUE_NAME", v)
        elif v == OPTIONS_NON_EXISTENT:
            refuse("STRICTSPEC_OPTIONS_RANKING_RESERVED", v)
        elif v in level:
            refuse("STRICTSPEC_OPTIONS_RANKING_DUPLICATE", v)
        else:
            values.append(v)
            level[v] = lv
    if ds:
        return None, ds
    return OptionsRanking(values=tuple(values), level=level), []


def parse_options_ranking(s: str) -> tuple[OptionsRanking | None, tuple[Diagnostic, ...]]:
    """Parse a ranking string: value names separated by single spaces around
    `>` (stronger than) or `=` (equal rank), for example
    "npm = pypi = jsr > none". A string that is not that alternation is one
    STRICTSPEC_OPTIONS_RANKING_MALFORMED diagnostic; otherwise every value name
    outside the grammar, every use of the reserved non-existent, and every
    repeated value is refused. The diagnostics' path is "$". Non-empty
    diagnostics mean None.
    """
    rk, ds = _parse_ranking(s, _diag.new_path())
    return rk, _public(ds)


@dataclass(frozen=True)
class CheckedOption:
    """A registry option that passed every registry rule, with its parsed
    ranking.
    """

    declaration: OptionDeclaration
    ranking: OptionsRanking


class CheckedOptionsRegistry:
    """A registry that passed every registry rule. Only
    validate_options_registry makes one.
    """

    __slots__ = ("_options", "_dependents")

    def __init__(self, options: dict[str, CheckedOption], dependents: dict[str, tuple[str, ...]]) -> None:
        self._options = options
        # Each option's dependents: the options that require it, directly or
        # through other options, in declaration order.
        self._dependents = dependents

    def option(self, name: str) -> CheckedOption | None:
        """The checked option named name (the tool's own name for it, without
        the tool prefix), or None.
        """
        return self._options.get(name)

    def names(self) -> tuple[str, ...]:
        """The registry's option names in declaration order."""
        return tuple(self._options)


def _valid_subject(s: str) -> bool:
    """A registry subject names a subject file the entry loader reads: a
    value-name-grammar stem that is not the options directory's own manifest.
    """
    return bool(_VALUE_NAME.fullmatch(s)) and s + ".toml" != _OPTIONS_MANIFEST_FILE


def _rendered_list(names) -> str:
    """The {dependents} / {options} slot text: each name rendered as a value
    slot renders it, joined by ", ", never truncated as a whole.
    """
    return ", ".join(_render.render_value(_diag.StringVal(n)) for n in names)


def _reach(name: str, requires: dict[str, list[str]]) -> set[str]:
    """The options reachable from name through requires, following only
    declared names other than the option itself.
    """
    seen: set[str] = set()
    stack = [name]
    while stack:
        n = stack.pop()
        for r in requires[n]:
            if r not in seen:
                seen.add(r)
                stack.append(r)
    return seen


def validate_options_registry(
    reg: OptionsRegistry,
) -> tuple[CheckedOptionsRegistry | None, tuple[Diagnostic, ...]]:
    """Apply the registry rules to a registry: each option's values parse as a
    ranking, its default is a declared value, its subject is a valid subject
    file stem, and its requires name only other options the registry
    declares, with no cycle among them. Option names are unique by the
    built-in schema, and so are the names in one option's requires; a
    registry handed over directly with a repeat draws the same
    STRICTSPEC_INTRA_UNIQUE_BY or STRICTSPEC_INTRA_PAIRWISE_DISTINCT
    diagnostic the shape reader reports. Paths locate the refused field in the
    registry document. Non-empty diagnostics mean None.
    """
    out: dict[str, CheckedOption] = {}
    ds: list[_diag.Diagnostic] = []
    declared: dict[str, int] = {}
    for i, o in enumerate(reg.options):
        declared.setdefault(o.name, i)
    # Per first-declared option, the declared names it requires other than
    # itself: the edges the cycle rule and the dependents follow.
    requires: dict[str, list[str]] = {}
    for i, o in enumerate(reg.options):
        at = _diag.new_path(_diag.Key("option"), _diag.Index(i))
        if o.name in out:
            ds.append(
                _diag.Diagnostic(
                    "STRICTSPEC_INTRA_UNIQUE_BY",
                    _diag.new_path(_diag.Key("option")),
                    {
                        "value": _str_val(o.name),
                        "field": _diag.SlotString("name"),
                        "normalization": _diag.SlotString("none"),
                    },
                )
            )
        rk, rds = _parse_ranking(o.values, _diag.append_key(at, "values"))
        ds.extend(rds)
        if rk is not None and not rk.declares(o.default):
            ds.append(
                _diag.Diagnostic(
                    "STRICTSPEC_OPTIONS_DEFAULT_UNDECLARED",
                    _diag.append_key(at, "default"),
                    {"value": _str_val(o.default), "ranking": _str_val(o.values)},
                )
            )
        if not _valid_subject(o.subject):
            ds.append(
                _diag.Diagnostic(
                    "STRICTSPEC_OPTIONS_SUBJECT_INVALID",
                    _diag.append_key(at, "subject"),
                    {"value": _str_val(o.subject)},
                )
            )
        req_at = _diag.append_key(at, "requires")
        seen: set[str] = set()
        for r in o.requires:
            if r in seen:
                ds.append(
                    _diag.Diagnostic(
                        "STRICTSPEC_INTRA_PAIRWISE_DISTINCT",
                        req_at,
                        {"value": _str_val(r), "normalization": _diag.SlotString("none")},
                    )
                )
                break
            seen.add(r)
        candidates = tuple(n.name for n in reg.options if n.name != o.name)
        edges: list[str] = []
        for j, r in enumerate(o.requires):
            r_at = _diag.append_index(req_at, j)
            if r == o.name:
                ds.append(_diag.Diagnostic("STRICTSPEC_OPTIONS_REQUIRES_SELF", r_at, {"name": _str_val(o.name)}))
            elif r not in declared:
                ds.append(
                    _diag.Diagnostic(
                        "STRICTSPEC_OPTIONS_REQUIRES_UNDECLARED",
                        r_at,
                        {
                            "name": _str_val(o.name),
                            "value": _str_val(r),
                            "suggestion": _diag.SlotSuggestion(r, candidates),
                        },
                    )
                )
            else:
                edges.append(r)
        if o.name not in out:
            out[o.name] = CheckedOption(declaration=o, ranking=rk)
            requires[o.name] = edges
    # A cycle is a set of two or more options each depending on all the
    # others, reported once, at the requires of its first-declared member.
    names = list(out)
    reach = {n: _reach(n, requires) for n in names}
    in_cycle: set[str] = set()
    for n in names:
        if n in in_cycle or n not in reach[n]:
            continue
        members = [m for m in names if m == n or (m in reach[n] and n in reach[m])]
        in_cycle.update(members)
        ds.append(
            _diag.Diagnostic(
                "STRICTSPEC_OPTIONS_REQUIRES_CYCLE",
                _diag.new_path(_diag.Key("option"), _diag.Index(declared[n]), _diag.Key("requires")),
                {"options": _diag.SlotString(_rendered_list(members))},
            )
        )
    if ds:
        return None, _public(ds)
    dependents = {n: tuple(m for m in names if n in reach[m]) for n in names}
    return CheckedOptionsRegistry(out, dependents), ()


@dataclass(frozen=True)
class ClassifiedOptionsEntry:
    """An accepted entry of the validated namespace with its classification:
    OPTIONS_SETTLED, OPTIONS_DEBT, or OPTIONS_WAITING_ON_TOOL.
    """

    entry: OptionsEntry
    class_: str


def _options_file(name: str) -> str:
    """The repository-relative path of a subject document."""
    return OPTIONS_DIR + "/" + name


def _entry_path(e: OptionsEntry) -> _diag.Path:
    return _diag.new_path(_diag.Key("entry"), _diag.Index(e.index))


def validate_options_namespace(
    tool: str,
    reg: CheckedOptionsRegistry,
    entries: tuple[OptionsEntry, ...] | list[OptionsEntry],
) -> tuple[tuple[ClassifiedOptionsEntry, ...], tuple[Diagnostic, ...]]:
    """Judge the entries of one tool's namespace (`<tool>:*`) against that
    tool's checked registry, in the order given; tool is the tool's name.
    Entries of other namespaces are not judged. Returns the accepted entries,
    classified, and a diagnostic for every refusal; an entry with any refusal is
    not classified. Each diagnostic's path locates the entry within its subject
    document, and its message names that document.
    """
    prefix = tool + ":"
    candidates = tuple(prefix + n for n in reg.names())
    first: dict[tuple[str, str | None], OptionsEntry] = {}
    by_id: dict[str, list[OptionsEntry]] = {}
    for e in entries:
        by_id.setdefault(e.id, []).append(e)

    def missing(e: OptionsEntry, decl: OptionDeclaration, field: str) -> list[str]:
        """The dependents of the option of entry e (declaration decl) that are
        not off in field (current or ideal).
        """
        out: list[str] = []
        for dep in reg._dependents[decl.name]:
            dep_decl = reg._options[dep].declaration
            off = other = False
            for f in by_id.get(prefix + dep, ()):
                if getattr(f, field) != _OFF:
                    other = True
                elif f.scope is None or (e.scope is not None and f.scope == e.scope and dep_decl.scope == decl.scope):
                    off = True
            if not off and (dep_decl.default != _OFF or other):
                out.append(prefix + dep)
        return out

    accepted: list[ClassifiedOptionsEntry] = []
    ds: list[_diag.Diagnostic] = []
    for e in entries:
        if not e.id.startswith(prefix):
            continue
        before = len(ds)
        at = _entry_path(e)

        def refuse(code: str, path: _diag.Path, slots: dict[str, _diag.Slot]) -> None:
            slots["file"] = _diag.SlotString(_options_file(e.file))
            if code != "STRICTSPEC_OPTIONS_UNKNOWN_OPTION":
                slots["id"] = _str_val(e.id)
            ds.append(_diag.Diagnostic(code, path, slots))

        k = (e.id, e.scope)
        if k in first:
            f = first[k]
            refuse(
                "STRICTSPEC_OPTIONS_DUPLICATE_ENTRY",
                at,
                {"first": _diag.SlotPath(_entry_path(f)), "first_file": _diag.SlotString(_options_file(f.file))},
            )
        else:
            first[k] = e
        opt = reg.option(e.id[len(prefix) :])
        if opt is None:
            refuse(
                "STRICTSPEC_OPTIONS_UNKNOWN_OPTION",
                _diag.append_key(at, "id"),
                {
                    "id": _str_val(e.id),
                    "tool": _diag.SlotString(tool),
                    "suggestion": _diag.SlotSuggestion(e.id, candidates),
                },
            )
        else:
            decl, rk = opt.declaration, opt.ranking
            want = decl.subject + ".toml"
            if e.file != want:
                refuse("STRICTSPEC_OPTIONS_WRONG_SUBJECT", at, {"subject": _diag.SlotString(_options_file(want))})
            if e.scope is not None and decl.scope == _SCOPE_NONE:
                refuse("STRICTSPEC_OPTIONS_SCOPE_NOT_ACCEPTED", _diag.append_key(at, "scope"), {"value": _str_val(e.scope)})
            current_ok = rk.declares(e.current)
            if not current_ok:
                refuse(
                    "STRICTSPEC_OPTIONS_UNDECLARED_CURRENT",
                    _diag.append_key(at, "current"),
                    {"value": _str_val(e.current), "ranking": _str_val(decl.values)},
                )
            waiting = e.ideal == OPTIONS_NON_EXISTENT
            ideal_ok = waiting or rk.declares(e.ideal)
            if not ideal_ok:
                refuse(
                    "STRICTSPEC_OPTIONS_UNDECLARED_IDEAL",
                    _diag.append_key(at, "ideal"),
                    {"value": _str_val(e.ideal), "ranking": _str_val(decl.values)},
                )
            if current_ok and ideal_ok:
                if e.current == decl.default and e.ideal == decl.default:
                    refuse("STRICTSPEC_OPTIONS_REDUNDANT", at, {"value": _str_val(e.current)})
                elif not waiting and rk.level[e.current] < rk.level[e.ideal]:
                    refuse(
                        "STRICTSPEC_OPTIONS_CURRENT_ABOVE_IDEAL",
                        at,
                        {"current": _str_val(e.current), "ideal": _str_val(e.ideal), "ranking": _str_val(decl.values)},
                    )
            if e.current == _OFF and current_ok:
                deps = missing(e, decl, "current")
                if deps:
                    refuse(
                        "STRICTSPEC_OPTIONS_DEPENDENTS_NOT_OFF",
                        _diag.append_key(at, "current"),
                        {"dependents": _diag.SlotString(_rendered_list(deps))},
                    )
            if e.ideal == _OFF and ideal_ok:
                deps = missing(e, decl, "ideal")
                if deps:
                    refuse(
                        "STRICTSPEC_OPTIONS_DEPENDENTS_IDEAL_NOT_OFF",
                        _diag.append_key(at, "ideal"),
                        {"dependents": _diag.SlotString(_rendered_list(deps))},
                    )
        if e.reason == "":
            refuse("STRICTSPEC_OPTIONS_EMPTY_REASON", _diag.append_key(at, "reason"), {})
        if len(ds) > before:
            continue
        level = opt.ranking.level
        if e.ideal == OPTIONS_NON_EXISTENT:
            cls = OPTIONS_WAITING_ON_TOOL
        elif level[e.current] == level[e.ideal]:
            cls = OPTIONS_SETTLED
        else:
            cls = OPTIONS_DEBT
        accepted.append(ClassifiedOptionsEntry(entry=e, class_=cls))
    return tuple(accepted), _public(ds)
