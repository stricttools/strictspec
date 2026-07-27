"""The N-way conformance target registry.

Targets are registered DECLARATIVELY, one ``_register`` call each (the strictcli
conformance pattern: adding a target is one registration and zero changes
elsewhere). Every target carries a name, an ``implemented`` flag, and an
invocation contract -- a callable that, once the target exists, runs it over a
fixture (compile the schema once, feed the input document) and returns the
OBSERVED outcome (valid, or an ordered diagnostics list).

All FOUR targets are registered here with ``implemented=False``: they are
explicit declared STUBS. The implementations (generated Python, generated Go,
generated TypeScript, and the internal interpreter) land in later phases; when
one arrives, its ``_register`` call flips to ``implemented=True`` and supplies a
real ``invoke``. Nothing else in the harness changes.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Callable

from .fixtures import Fixture


@dataclass(frozen=True)
class ObservedDiagnostic:
    """One diagnostic a target actually emitted."""

    code: str
    path: str
    message: str


@dataclass(frozen=True)
class Outcome:
    """A target's observed outcome for one fixture."""

    valid: bool
    diagnostics: tuple[ObservedDiagnostic, ...] = ()


def _unimplemented_invoke(fixture: Fixture) -> Outcome:
    """Stub invocation for a target that does not exist yet."""
    raise NotImplementedError(
        "this target is a declared stub (implemented=false); its implementation "
        "lands in a later phase"
    )


@dataclass(frozen=True)
class Target:
    """A conformance target descriptor.

    invoke(fixture) -> Outcome
        Runs the target over the fixture and returns the observed outcome. For a
        stub target (implemented=false) this is never called by the runner.
    """

    name: str
    implemented: bool
    invoke: Callable[[Fixture], Outcome] = field(default=_unimplemented_invoke)


# Insertion order is the reporting order.
_REGISTRY: dict[str, Target] = {}


def _register(target: Target) -> None:
    if target.name in _REGISTRY:
        raise ValueError(f"duplicate target registration: {target.name!r}")
    _REGISTRY[target.name] = target


# --- The four declared targets ------------------------------------------------
# All implemented=false for now. Flip a flag and supply an `invoke` when the
# corresponding implementation phase lands.
_register(Target(name="interpreter", implemented=False))
_register(Target(name="python", implemented=False))
_register(Target(name="go", implemented=False))
_register(Target(name="ts", implemented=False))


def all_targets() -> list[Target]:
    """Every registered target, in registration (reporting) order."""
    return list(_REGISTRY.values())


def implemented_targets() -> list[Target]:
    return [t for t in _REGISTRY.values() if t.implemented]
