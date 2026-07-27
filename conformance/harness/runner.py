"""The conformance runner: load fixtures, run every target, report honestly.

Honest-reporting semantics (conformance/DESIGN.md, "Harness"):

- A target with ``implemented=false`` is reported per fixture as UNIMPLEMENTED --
  a DISTINCT status, never counted as a pass.
- The run FAILS (non-zero exit) only on:
  (a) a malformed fixture,
  (b) an implemented target's MISMATCH against the hand-authored expectation,
  (c) a target claiming ``implemented=true`` but not invocable (NOT_INVOCABLE).
- With all four targets declared unimplemented and every fixture well-formed, the
  run exits GREEN. Flipping a target to implemented=true makes any mismatch fail.

When >= 2 targets are implemented, the parity checker also runs; a parity break
is fatal.
"""

from __future__ import annotations

import enum
from dataclasses import dataclass, field
from pathlib import Path

from . import parity
from .fixtures import Fixture, MalformedFixture, load_fixture
from .targets import Outcome, Target, all_targets, implemented_targets


class Status(enum.Enum):
    PASS = "PASS"
    MISMATCH = "MISMATCH"
    UNIMPLEMENTED = "UNIMPLEMENTED"
    NOT_INVOCABLE = "NOT_INVOCABLE"


@dataclass
class CaseResult:
    fixture_name: str
    target_name: str
    status: Status
    detail: str = ""


@dataclass
class Report:
    malformed: list[str] = field(default_factory=list)
    results: list[CaseResult] = field(default_factory=list)
    parity: parity.ParityReport = field(default_factory=parity.ParityReport)
    fixture_count: int = 0

    def count(self, status: Status) -> int:
        return sum(1 for r in self.results if r.status is status)

    @property
    def mismatches(self) -> list[CaseResult]:
        return [r for r in self.results if r.status is Status.MISMATCH]

    @property
    def not_invocable(self) -> list[CaseResult]:
        return [r for r in self.results if r.status is Status.NOT_INVOCABLE]

    @property
    def exit_code(self) -> int:
        if self.malformed or self.mismatches or self.not_invocable:
            return 1
        if not self.parity.ok:
            return 1
        return 0


def _diff(expected: Fixture, observed: Outcome) -> str | None:
    """Return a human diff string if observed != expected, else None."""
    if expected.valid:
        if observed.valid and not observed.diagnostics:
            return None
        return f"expected VALID, observed {_describe(observed)}"
    # expected diagnostics, ordered
    exp = [(d.code, d.path, d.rendered_message()) for d in expected.diagnostics]
    obs = [(d.code, d.path, d.message) for d in observed.diagnostics]
    if observed.valid:
        return f"expected {len(exp)} diagnostic(s), observed VALID"
    if exp != obs:
        return f"expected {exp!r}, observed {obs!r}"
    return None


def _describe(outcome: Outcome) -> str:
    if outcome.valid:
        return "VALID"
    return "[" + ", ".join(d.code for d in outcome.diagnostics) + "]"


def _run_target(
    target: Target, fixture: Fixture
) -> tuple[CaseResult, Outcome | None]:
    """Run one target over one fixture. Returns (result, observed-outcome).

    The observed outcome is returned (for parity comparison) only when the
    target actually ran; it is None for stubs and non-invocable targets.
    """
    if not target.implemented:
        return CaseResult(fixture.name, target.name, Status.UNIMPLEMENTED), None
    try:
        observed = target.invoke(fixture)
    except NotImplementedError as exc:
        return (
            CaseResult(
                fixture.name,
                target.name,
                Status.NOT_INVOCABLE,
                f"declares implemented=true but is not invocable: {exc}",
            ),
            None,
        )
    except Exception as exc:  # noqa: BLE001 -- any invocation failure is NOT_INVOCABLE
        return (
            CaseResult(
                fixture.name, target.name, Status.NOT_INVOCABLE, f"invocation raised: {exc}"
            ),
            None,
        )
    diff = _diff(fixture, observed)
    if diff is None:
        return CaseResult(fixture.name, target.name, Status.PASS), observed
    return CaseResult(fixture.name, target.name, Status.MISMATCH, diff), observed


def run(fixtures_root: Path) -> Report:
    """Load and run the full fixture tree. Never raises on a malformed fixture;
    collects it and fails the exit code instead."""
    report = Report()

    # Load every fixture; a malformed fixture is a hard error we record.
    fixtures: list[Fixture] = []
    for toml_path in sorted(fixtures_root.rglob("*.toml")):
        rel_parts = toml_path.relative_to(fixtures_root).parts[:-1]
        if any(p.startswith("_") for p in rel_parts):
            continue
        try:
            fixtures.append(load_fixture(toml_path, fixtures_root))
        except MalformedFixture as exc:
            report.malformed.append(str(exc))
    report.fixture_count = len(fixtures)

    targets = all_targets()
    impl = implemented_targets()
    report.parity.active = parity.is_active(impl)

    for fixture in fixtures:
        outcomes: dict[str, Outcome] = {}
        for target in targets:
            result, observed = _run_target(target, fixture)
            report.results.append(result)
            if observed is not None:
                outcomes[target.name] = observed
        if report.parity.active:
            report.parity.findings.extend(
                parity.check_parity(fixture.name, outcomes)
            )

    return report


def format_report(report: Report) -> str:
    """A concise textual summary of a run."""
    lines: list[str] = []
    if report.malformed:
        lines.append("MALFORMED FIXTURES (hard error):")
        for m in report.malformed:
            lines.append(f"  {m}")
        lines.append("")
    if report.mismatches:
        lines.append("MISMATCHES:")
        for r in report.mismatches:
            lines.append(f"  {r.fixture_name} [{r.target_name}]: {r.detail}")
        lines.append("")
    if report.not_invocable:
        lines.append("NOT INVOCABLE:")
        for r in report.not_invocable:
            lines.append(f"  {r.fixture_name} [{r.target_name}]: {r.detail}")
        lines.append("")
    if report.parity.active and not report.parity.ok:
        lines.append("PARITY BREAKS:")
        for f in report.parity.findings:
            lines.append(f"  {f.fixture_name}: {f.detail}")
        lines.append("")

    lines.append(
        f"fixtures: {report.fixture_count}  "
        f"pass: {report.count(Status.PASS)}  "
        f"mismatch: {report.count(Status.MISMATCH)}  "
        f"unimplemented: {report.count(Status.UNIMPLEMENTED)}  "
        f"not-invocable: {report.count(Status.NOT_INVOCABLE)}  "
        f"malformed: {len(report.malformed)}"
    )
    parity_state = (
        "active" if report.parity.active else "inactive (<2 implemented targets)"
    )
    lines.append(f"parity: {parity_state}")
    lines.append("GREEN" if report.exit_code == 0 else "RED")
    return "\n".join(lines)
