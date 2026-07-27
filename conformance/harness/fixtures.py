"""Fixture loading and structural validation.

A fixture is PURE DATA: a self-contained TOML file that names a strictspec
schema, an input document, and an expected outcome (VALID, or an ordered list of
diagnostics carrying code + path + slot values -- never message prose). The
runner itself loads and STRUCTURALLY VALIDATES every fixture; a malformed
fixture is a HARD ERROR (conformance/DESIGN.md, "Harness" and the honest-
reporting semantics), never a silent skip.

Fixture TOML shape::

    name = "..."               # unique, ident-ish label
    category = "..."           # one of CATEGORIES
    provenance = "..."         # hand-authored | derived-from(<repo path>[, mutated])
    description = "..."         # optional prose (author-facing, not asserted)
    schema = "_schemas/x.toml" # required, path relative to the fixtures root
    input = "_inputs/x.json"   # OR input_inline + input_syntax below
    input_inline = "..."       # raw document text
    input_syntax = "json"      # json | toml | jsonl (required with input_inline)

    [expected]
    valid = true               # XOR the diagnostics array below

    [[expected.diagnostics]]
    code = "STRICTSPEC_..."    # must be in the pinned catalogue
    path = "$..."              # must conform to the path grammar
    slots = { key = "q", condition = "..." }   # rendered slot values
"""

from __future__ import annotations

import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from . import templates
from .paths import is_valid_path

# Closed category set (conformance/DESIGN.md, "Fixture categories"). A fixture
# declaring a category outside this set is malformed.
CATEGORIES: frozenset[str] = frozenset(
    {
        "construct-coverage",
        "unions",
        "numbers",
        "datetimes",
        "duplicate-keys",
        "tagged-entry-equivalence",
        "unknown-keys",
        "opaque-leaves",
        "shared-types",
        "enum-sourcing",
        "aggregates",
        "cross-document-constraints",
        "versioning",
        "boundary-invariant",
        "toolchain",
        "diff-engine",
        "doc-diff",
        "error-reporting",
        "write-side",
        "aliases",
        "edge-inputs",
        "unicode",
        "meta-schema",
    }
)

_INPUT_SYNTAXES = frozenset({"json", "toml", "jsonl"})


class MalformedFixture(Exception):
    """A fixture failed structural validation. This is a hard error."""

    def __init__(self, path: Path, message: str) -> None:
        super().__init__(f"{path}: {message}")
        self.fixture_path = path
        self.reason = message


@dataclass(frozen=True)
class Diagnostic:
    code: str
    path: str
    slots: dict[str, str]

    def rendered_message(self) -> str:
        """The pinned message text for this diagnostic (template + slots)."""
        return templates.render(self.code, self.slots)


@dataclass(frozen=True)
class Fixture:
    source_path: Path
    name: str
    category: str
    provenance: str
    description: str
    schema_path: Path
    input_syntax: str
    # Exactly one of input_path / input_inline is set.
    input_path: Path | None
    input_inline: str | None
    valid: bool
    diagnostics: tuple[Diagnostic, ...]
    # Pinned evidence for cross-document constraint fixtures: resolver name ->
    # returned data, so a constraint verdict is a pure function of (document,
    # evidence) and identical on every target (conformance/DESIGN.md). Empty for
    # structural fixtures.
    evidence: dict[str, Any]


def _require(data: dict[str, Any], key: str, path: Path, kind: type) -> Any:
    if key not in data:
        raise MalformedFixture(path, f"missing required key {key!r}")
    value = data[key]
    if not isinstance(value, kind):
        raise MalformedFixture(
            path, f"key {key!r} must be {kind.__name__}, got {type(value).__name__}"
        )
    return value


def load_fixture(path: Path, fixtures_root: Path) -> Fixture:
    """Load and structurally validate one fixture. Raises MalformedFixture."""
    try:
        with path.open("rb") as f:
            data = tomllib.load(f)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise MalformedFixture(path, f"unreadable/unparseable TOML: {exc}") from exc

    name = _require(data, "name", path, str)
    category = _require(data, "category", path, str)
    if category not in CATEGORIES:
        raise MalformedFixture(path, f"unknown category {category!r}")
    provenance = _require(data, "provenance", path, str)
    description = data.get("description", "")
    if not isinstance(description, str):
        raise MalformedFixture(path, "description must be a string")

    schema_rel = _require(data, "schema", path, str)
    schema_path = (fixtures_root / schema_rel).resolve()
    if not schema_path.is_file():
        raise MalformedFixture(path, f"schema file not found: {schema_rel}")

    # Input: file reference XOR inline.
    has_file = "input" in data
    has_inline = "input_inline" in data
    if has_file == has_inline:
        raise MalformedFixture(
            path, "exactly one of `input` or `input_inline` is required"
        )
    input_path: Path | None = None
    input_inline: str | None = None
    if has_file:
        input_rel = _require(data, "input", path, str)
        input_path = (fixtures_root / input_rel).resolve()
        if not input_path.is_file():
            raise MalformedFixture(path, f"input file not found: {input_rel}")
        input_syntax = data.get("input_syntax") or _syntax_from_suffix(input_path)
        if input_syntax is None:
            raise MalformedFixture(
                path, f"cannot infer input_syntax from {input_rel}; set it explicitly"
            )
    else:
        input_inline = _require(data, "input_inline", path, str)
        input_syntax = _require(data, "input_syntax", path, str)
    if input_syntax not in _INPUT_SYNTAXES:
        raise MalformedFixture(path, f"unknown input_syntax {input_syntax!r}")

    expected = _require(data, "expected", path, dict)
    valid, diagnostics = _parse_expected(expected, path)

    evidence = data.get("evidence", {})
    if not isinstance(evidence, dict):
        raise MalformedFixture(path, "evidence must be a table")

    return Fixture(
        source_path=path,
        name=name,
        category=category,
        provenance=provenance,
        description=description,
        schema_path=schema_path,
        input_syntax=input_syntax,
        input_path=input_path,
        input_inline=input_inline,
        valid=valid,
        diagnostics=diagnostics,
        evidence=evidence,
    )


def _syntax_from_suffix(path: Path) -> str | None:
    return {".json": "json", ".toml": "toml", ".jsonl": "jsonl"}.get(path.suffix)


def _parse_expected(
    expected: dict[str, Any], path: Path
) -> tuple[bool, tuple[Diagnostic, ...]]:
    has_valid = "valid" in expected
    has_diags = "diagnostics" in expected
    if has_valid and expected["valid"] is True:
        if has_diags:
            raise MalformedFixture(
                path, "`expected.valid = true` must not also list diagnostics"
            )
        return True, ()
    if not has_diags:
        raise MalformedFixture(
            path,
            "expected outcome must be `valid = true` or a non-empty diagnostics list",
        )
    raw = expected["diagnostics"]
    if not isinstance(raw, list) or not raw:
        raise MalformedFixture(path, "expected.diagnostics must be a non-empty array")
    diagnostics = tuple(_parse_diagnostic(d, path) for d in raw)
    return False, diagnostics


def _parse_diagnostic(raw: Any, path: Path) -> Diagnostic:
    if not isinstance(raw, dict):
        raise MalformedFixture(path, "each diagnostic must be a table")
    code = raw.get("code")
    if not isinstance(code, str):
        raise MalformedFixture(path, "diagnostic `code` must be a string")
    if code not in templates.CATALOGUE:
        raise MalformedFixture(
            path,
            f"code {code!r} is not in the pinned catalogue "
            f"(appendix-error-codes.md). Do not invent codes.",
        )
    diag_path = raw.get("path")
    if not isinstance(diag_path, str):
        raise MalformedFixture(path, f"diagnostic {code}: `path` must be a string")
    if not is_valid_path(diag_path):
        raise MalformedFixture(
            path, f"diagnostic {code}: path {diag_path!r} violates the path grammar"
        )
    slots = raw.get("slots", {})
    if not isinstance(slots, dict):
        raise MalformedFixture(path, f"diagnostic {code}: `slots` must be a table")
    slot_values = {k: _slot_str(k, v, code, path) for k, v in slots.items()}
    # Convention: the template's `{path}` slot defaults to the diagnostic's own
    # path, so a fixture writes the location once. Author may override by setting
    # `path` in slots explicitly (rare: when the message's {path} names the
    # containing record rather than the offending leaf).
    if "path" in templates.slots_for(code) and "path" not in slot_values:
        slot_values["path"] = diag_path
    _validate_slots(code, slot_values, path)
    return Diagnostic(code=code, path=diag_path, slots=slot_values)


def _slot_str(key: str, value: Any, code: str, path: Path) -> str:
    if not isinstance(value, str):
        raise MalformedFixture(
            path,
            f"diagnostic {code}: slot {key!r} must be a pre-rendered string "
            f"(fixtures carry rendered slot values, not raw values)",
        )
    return value


def _validate_slots(code: str, slots: dict[str, str], path: Path) -> None:
    expected_slots = templates.slots_for(code)
    provided = set(slots)
    unknown = provided - expected_slots
    if unknown:
        raise MalformedFixture(
            path,
            f"diagnostic {code}: unknown slot(s) {sorted(unknown)}; "
            f"template slots are {sorted(expected_slots)}",
        )
    required = expected_slots - templates.OPTIONAL_SLOTS
    missing = required - provided
    if missing:
        raise MalformedFixture(
            path, f"diagnostic {code}: missing required slot(s) {sorted(missing)}"
        )
    # Rendering must succeed (every template placeholder resolves). Slot-set
    # completeness above guarantees this; render to surface any KeyError. Literal
    # braces inside slot values (e.g. a regex `{0,31}`) are fine -- they are
    # inserted verbatim and never re-scanned as placeholders.
    templates.render(code, slots)


def discover_fixtures(fixtures_root: Path) -> list[Fixture]:
    """Load every ``*.toml`` fixture under ``fixtures_root`` (sorted, recursive).

    Files under a directory whose name starts with ``_`` (e.g. ``_schemas``,
    ``_inputs``) are treated as shared assets, not fixtures.
    """
    fixtures: list[Fixture] = []
    for toml_path in sorted(fixtures_root.rglob("*.toml")):
        if any(part.startswith("_") for part in toml_path.relative_to(fixtures_root).parts[:-1]):
            continue
        fixtures.append(load_fixture(toml_path, fixtures_root))
    return fixtures
