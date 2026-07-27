"""Toolchain-level (CLI-only) conformance fixtures.

The four-target validation harness (``runner.py``) asserts VERDICT + CODE + PATH
+ MESSAGE identity across Python, Go, TS, and the interpreter. The Phase-6
analyses -- migration, the write path, ``diff`` certificates, and ``doc-diff``
deltas -- are TOOLCHAIN-ONLY by the constitution (migration is CLI-only;
``diff``/``doc-diff`` are golden-output determinism, not multi-target execution).
They therefore run against the GO TOOLCHAIN ALONE, single-target, and are asserted
as CLI behaviour (exit code + byte-exact output + expected diagnostics/claims),
NOT as cross-target parity.

Toolchain fixtures live under ``fixtures/_toolchain/<name>/fixture.toml`` (the
leading ``_`` keeps them out of the validation harness's discovery). Each fixture
directory is self-contained: the schema(s), migration file(s), input document(s),
corpus, and expected output all sit beside ``fixture.toml``. Expected outcomes are
HAND-AUTHORED from spec, never regenerated from the tool.
"""

from __future__ import annotations

import shutil
import subprocess
import threading
import tomllib
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from . import REPO_ROOT

_GO_DIR = REPO_ROOT / "go"
_CLI_BIN = _GO_DIR / ".strictspec-cli"  # gitignored build artifact
_build_lock = threading.Lock()
_built = False

_KINDS = frozenset({"migration", "diff", "doc-diff"})


class MalformedToolchainFixture(Exception):
    """A toolchain fixture failed structural validation. This is a hard error."""


def ensure_cli() -> Path:
    """Build the strictspec CLI binary once; return its path. Build failure raises."""
    global _built
    with _build_lock:
        if not _built:
            subprocess.run(
                ["go", "build", "-o", str(_CLI_BIN), "./cmd/strictspec"],
                cwd=str(_GO_DIR),
                check=True,
                capture_output=True,
                text=True,
            )
            _built = True
    return _CLI_BIN


@dataclass(frozen=True)
class ToolchainFixture:
    source_dir: Path
    name: str
    kind: str
    description: str
    spec: dict[str, Any]
    expected: dict[str, Any]


def discover_toolchain_fixtures(root: Path) -> list[ToolchainFixture]:
    tc_root = root / "_toolchain"
    fixtures: list[ToolchainFixture] = []
    if not tc_root.is_dir():
        return fixtures
    for fixture_toml in sorted(tc_root.rglob("fixture.toml")):
        fixtures.append(load_toolchain_fixture(fixture_toml))
    return fixtures


def load_toolchain_fixture(path: Path) -> ToolchainFixture:
    try:
        with path.open("rb") as f:
            data = tomllib.load(f)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise MalformedToolchainFixture(f"{path}: unreadable/unparseable: {exc}") from exc
    name = data.get("name")
    kind = data.get("kind")
    if not isinstance(name, str) or not name:
        raise MalformedToolchainFixture(f"{path}: missing `name`")
    if kind not in _KINDS:
        raise MalformedToolchainFixture(f"{path}: unknown kind {kind!r} (one of {sorted(_KINDS)})")
    expected = data.get("expected")
    if not isinstance(expected, dict):
        raise MalformedToolchainFixture(f"{path}: missing [expected] table")
    return ToolchainFixture(
        source_dir=path.parent,
        name=name,
        kind=kind,
        description=data.get("description", ""),
        spec=data,
        expected=expected,
    )


@dataclass
class ToolchainResult:
    fixture_name: str
    ok: bool
    detail: str = ""


def run_toolchain_fixture(binary: Path, fixture: ToolchainFixture, workdir: Path) -> ToolchainResult:
    """Run one toolchain fixture in an ISOLATED copy of its directory (migration
    writes in place, so the source tree is never mutated)."""
    work = workdir / fixture.name
    shutil.copytree(fixture.source_dir, work)

    argv = _build_argv(binary, fixture, work)
    proc = subprocess.run(argv, cwd=str(work), capture_output=True, text=True)

    exp = fixture.expected
    if "exit_code" in exp and proc.returncode != exp["exit_code"]:
        return ToolchainResult(
            fixture.name, False,
            f"exit_code {proc.returncode} != {exp['exit_code']}\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}",
        )
    for needle in exp.get("stdout_contains", []):
        if needle not in proc.stdout:
            return ToolchainResult(fixture.name, False, f"stdout missing {needle!r}\nSTDOUT:\n{proc.stdout}")
    for needle in exp.get("stderr_contains", []):
        if needle not in proc.stderr:
            return ToolchainResult(fixture.name, False, f"stderr missing {needle!r}\nSTDERR:\n{proc.stderr}")
    for produced, expected_file in exp.get("output_equals", {}).items():
        got = (work / produced).read_bytes()
        want = (work / expected_file).read_bytes()
        if got != want:
            return ToolchainResult(
                fixture.name, False,
                f"byte mismatch for {produced}:\n--- got ---\n{got.decode()}\n--- want ---\n{want.decode()}",
            )
    return ToolchainResult(fixture.name, True)


def _build_argv(binary: Path, fixture: ToolchainFixture, work: Path) -> list[str]:
    s = fixture.spec
    if fixture.kind == "migration":
        argv = [str(binary), "migrate", s["schema"], *s["inputs"],
                "--to", str(s["to"]), "--migrations", s.get("migrations", ".")]
        if s.get("dry_run"):
            argv.append("--dry-run")
        return argv
    if fixture.kind == "diff":
        corpus_root = s.get("corpus_subdir", ".")
        argv = [str(binary), "diff", s["old_schema"], s["new_schema"],
                "--corpus", s["corpus"], "--corpus-root", corpus_root]
        if s.get("migration"):
            argv += ["--migration", s["migration"]]
        if s.get("adjudication"):
            argv += ["--adjudication", s["adjudication"]]
        if s.get("same_version"):
            argv.append("--same-version")
        return argv
    if fixture.kind == "doc-diff":
        return [str(binary), "doc-diff", s["schema"], s["old_document"], s["new_document"]]
    raise MalformedToolchainFixture(f"{fixture.name}: unhandled kind {fixture.kind!r}")
