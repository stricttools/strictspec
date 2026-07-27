"""Cross-target parity checker (skeleton).

Parity is the identity guarantee: for a fixture, every IMPLEMENTED target must
produce a byte-identical outcome -- same verdict, and the same ordered
(code, path, message) diagnostics (conformance/DESIGN.md, "Parity checkers").

This checker ACTIVATES only when at least two targets are implemented. With
fewer than two, parity is undefined and the checker reports nothing (it is a
declared skeleton until the second target lands, at which point cross-target
identity becomes a first-class, fatal check).
"""

from __future__ import annotations

from dataclasses import dataclass, field

from .targets import Outcome, Target


@dataclass
class ParityFinding:
    fixture_name: str
    detail: str


@dataclass
class ParityReport:
    active: bool = False
    findings: list[ParityFinding] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.findings


def _outcome_key(outcome: Outcome) -> tuple:
    return (
        outcome.valid,
        tuple((d.code, d.path, d.message) for d in outcome.diagnostics),
    )


def check_parity(
    fixture_name: str,
    outcomes: dict[str, Outcome],
) -> list[ParityFinding]:
    """Compare implemented targets' outcomes for one fixture.

    ``outcomes`` maps target name -> Outcome, restricted by the caller to
    targets that ran successfully. Fewer than two entries: nothing to compare.
    """
    if len(outcomes) < 2:
        return []
    keys = {name: _outcome_key(o) for name, o in outcomes.items()}
    distinct = set(keys.values())
    if len(distinct) == 1:
        return []
    detail = "; ".join(
        f"{name}=" + _describe(outcomes[name]) for name in sorted(outcomes)
    )
    return [ParityFinding(fixture_name=fixture_name, detail=detail)]


def _describe(outcome: Outcome) -> str:
    if outcome.valid:
        return "VALID"
    return "[" + ", ".join(d.code for d in outcome.diagnostics) + "]"


def is_active(implemented: list[Target]) -> bool:
    return len(implemented) >= 2
