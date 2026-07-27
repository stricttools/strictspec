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

import json
import subprocess
import threading
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

from . import REPO_ROOT
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


# --- The reference interpreter (Go) invocation contract -----------------------
#
# The Go adapter binary (go/cmd/conformance-adapter) reads a JSON request on
# stdin (schema path + input document + evidence) and writes the observed outcome
# on stdout. We build it ONCE and invoke it per fixture (the strictcli conformance
# pattern: compile the runtime once, feed many cases).

_GO_DIR = REPO_ROOT / "go"
_ADAPTER_BIN = _GO_DIR / ".conformance-adapter"  # gitignored build artifact
_build_lock = threading.Lock()
_built = False


def _ensure_adapter() -> Path:
    """Build the Go adapter binary once; return its path. A build failure raises."""
    global _built
    with _build_lock:
        if not _built:
            subprocess.run(
                ["go", "build", "-o", str(_ADAPTER_BIN), "./cmd/conformance-adapter"],
                cwd=str(_GO_DIR),
                check=True,
                capture_output=True,
                text=True,
            )
            _built = True
    return _ADAPTER_BIN


def _interpreter_invoke(fixture: Fixture) -> Outcome:
    """Run the reference interpreter over one fixture and return its outcome."""
    binary = _ensure_adapter()
    req: dict = {
        "schema": str(fixture.schema_path),
        "input_syntax": fixture.input_syntax,
        "evidence": fixture.evidence,
    }
    if fixture.input_path is not None:
        req["input_path"] = str(fixture.input_path)
    else:
        req["input_inline"] = fixture.input_inline or ""
    proc = subprocess.run(
        [str(binary)],
        input=json.dumps(req),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"interpreter adapter failed (exit {proc.returncode}): {proc.stderr.strip()}"
        )
    resp = json.loads(proc.stdout)
    diagnostics = tuple(
        ObservedDiagnostic(code=d["code"], path=d["path"], message=d["message"])
        for d in (resp.get("diagnostics") or [])
    )
    return Outcome(valid=bool(resp["valid"]), diagnostics=diagnostics)


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
# The reference interpreter (Phase 5.4) is LIVE. The generated python/go/ts
# targets land in later phases; they remain declared stubs (implemented=false).
_register(Target(name="interpreter", implemented=True, invoke=_interpreter_invoke))
_register(Target(name="python", implemented=False))
_register(Target(name="go", implemented=False))
_register(Target(name="ts", implemented=False))


def all_targets() -> list[Target]:
    """Every registered target, in registration (reporting) order."""
    return list(_REGISTRY.values())


def implemented_targets() -> list[Target]:
    return [t for t in _REGISTRY.values() if t.implemented]
