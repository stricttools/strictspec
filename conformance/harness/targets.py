"""The N-way conformance target registry.

Targets are registered DECLARATIVELY, one ``_register`` call each (the strictcli
conformance pattern: adding a target is one registration and zero changes
elsewhere). Every target carries a name, an ``implemented`` flag, and an
invocation contract -- a callable that, once the target exists, runs it over a
fixture (compile the schema once, feed the input document) and returns the
OBSERVED outcome (valid, or an ordered diagnostics list).

All FOUR targets are LIVE here with ``implemented=True``: the internal
interpreter, generated Go, generated Python, and generated TypeScript. Each
carries a real ``invoke`` that drives its runtime over a fixture and returns the
observed outcome. Adding or removing a target is a single ``_register`` change;
nothing else in the harness moves.
"""

from __future__ import annotations

import hashlib
import json
import subprocess
import threading
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

from . import REPO_ROOT
from .fixtures import Fixture


def _hash_tree(root: Path, suffixes: tuple[str, ...]) -> str:
    """A stable content hash of every file under ``root`` whose name ends in one
    of ``suffixes``. Used to key build caches by TOOLCHAIN/RUNTIME state so a
    stale artifact can never mask a source regression (conformance/DESIGN.md,
    harness cache hygiene). Files are visited in sorted relative-path order; both
    the path and the bytes feed the digest.
    """
    h = hashlib.sha256()
    for p in sorted(root.rglob("*")):
        if not p.is_file() or not p.name.endswith(suffixes):
            continue
        # Skip build outputs / caches so the key reflects SOURCE, not artifacts.
        parts = set(p.relative_to(root).parts)
        if parts & {"node_modules", "dist", "dist-test", ".gen-cache", "__pycache__"}:
            continue
        h.update(str(p.relative_to(root)).encode())
        h.update(b"\0")
        h.update(p.read_bytes())
        h.update(b"\0")
    return h.hexdigest()[:16]


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
_PYTHON_DIR = REPO_ROOT / "python"
_TS_DIR = REPO_ROOT / "ts"
_ADAPTER_BIN = _GO_DIR / ".conformance-adapter"  # gitignored build artifact
_GEN_ADAPTER_BIN = _GO_DIR / ".gen-adapter"  # gitignored build artifact
_GEN_CACHE_ROOT = _GO_DIR / ".gen-cache"  # gitignored per-schema generated+built cache
_TS_BUILD_STAMP = _TS_DIR / ".conformance-build-cache.json"  # gitignored build stamp
_build_lock = threading.Lock()
_built = False
_gen_built = False
_go_src_hash: str | None = None
_python_ready = False
_ts_ready = False


def _gen_cache_dir() -> Path:
    """The per-schema generated-code cache directory, NAMESPACED by a hash of the
    Go source tree.

    The gen-cache inside emit.Build is keyed by the schema's file-set hash ALONE,
    so a runtime/emitter change with an unchanged schema would reuse a stale
    built validator and MASK the regression (this was observed once). Namespacing
    the cache root by the Go source hash means any runtime/emitter/adapter change
    invalidates every entry, so stale artifacts cannot mask a regression. Old
    namespaces simply become unreferenced (harmless, gitignored).
    """
    global _go_src_hash
    if _go_src_hash is None:
        _go_src_hash = _hash_tree(_GO_DIR, (".go", ".mod", ".sum"))
    return _GEN_CACHE_ROOT / _go_src_hash


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


def _ensure_gen_adapter() -> Path:
    """Build the Go generated-code adapter once; return its path.

    The gen-adapter generates a validator for a fixture's schema, compiles it
    against the runtime, and runs it -- proving the emitter's output compiles
    and validates. A build failure raises.
    """
    global _gen_built
    with _build_lock:
        if not _gen_built:
            subprocess.run(
                ["go", "build", "-o", str(_GEN_ADAPTER_BIN), "./cmd/gen-adapter"],
                cwd=str(_GO_DIR),
                check=True,
                capture_output=True,
                text=True,
            )
            _gen_built = True
    return _GEN_ADAPTER_BIN


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


# --- The generated-Go target invocation contract ------------------------------
#
# The gen-adapter GENERATES a Go validator for each fixture's schema (cached per
# schema under go/.gen-cache), COMPILES it against the runtime, and RUNS it on
# the fixture input. Because the generated validator drives the SAME shared
# emitter IR as the interpreter, its verdict+code+path+message are byte-identical
# -- cross-target parity is structural, not coincidental.


def _go_invoke(fixture: Fixture) -> Outcome:
    """Generate, compile, and run the generated Go validator over one fixture."""
    binary = _ensure_gen_adapter()
    req: dict = {
        "schema": str(fixture.schema_path),
        "input_syntax": fixture.input_syntax,
        "evidence": fixture.evidence,
        "runtime_dir": str(_GO_DIR),
        "cache_dir": str(_gen_cache_dir()),
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
            f"gen adapter failed (exit {proc.returncode}): {proc.stderr.strip()}"
        )
    resp = json.loads(proc.stdout)
    diagnostics = tuple(
        ObservedDiagnostic(code=d["code"], path=d["path"], message=d["message"])
        for d in (resp.get("diagnostics") or [])
    )
    return Outcome(valid=bool(resp["valid"]), diagnostics=diagnostics)


# --- The generated-Python and generated-TS target invocation contracts --------
#
# Both speak the SAME stdin/stdout JSON contract as the Go adapters (schema +
# input + evidence in; {valid, diagnostics:[{code,path,message}]} out). Each
# drives its language's PUBLIC runtime (compile_embedded / compileFromSource +
# Program.validate, or the meta-schema reader for meta fixtures). The harness
# prepares each runtime ONCE per run (uv env / tsc build) and invokes the adapter
# per fixture -- the strictcli conformance pattern.


def _adapter_request(fixture: Fixture) -> dict:
    req: dict = {
        "schema": str(fixture.schema_path),
        "input_syntax": fixture.input_syntax,
        "evidence": fixture.evidence,
    }
    if fixture.input_path is not None:
        req["input_path"] = str(fixture.input_path)
    else:
        req["input_inline"] = fixture.input_inline or ""
    return req


def _adapter_response(label: str, proc: subprocess.CompletedProcess) -> Outcome:
    if proc.returncode != 0:
        raise RuntimeError(
            f"{label} adapter failed (exit {proc.returncode}): {proc.stderr.strip()}"
        )
    resp = json.loads(proc.stdout)
    diagnostics = tuple(
        ObservedDiagnostic(code=d["code"], path=d["path"], message=d["message"])
        for d in (resp.get("diagnostics") or [])
    )
    return Outcome(valid=bool(resp["valid"]), diagnostics=diagnostics)


def _ensure_python() -> None:
    """Sync the Python project's uv environment ONCE per run (editable install),
    so per-fixture invocations can use ``uv run --no-sync`` with no re-resolution.
    A sync failure raises."""
    global _python_ready
    with _build_lock:
        if not _python_ready:
            subprocess.run(
                ["uv", "sync", "--quiet"],
                cwd=str(_PYTHON_DIR),
                check=True,
                capture_output=True,
                text=True,
            )
            _python_ready = True


def _python_invoke(fixture: Fixture) -> Outcome:
    """Run the generated-Python runtime over one fixture and return its outcome."""
    _ensure_python()
    proc = subprocess.run(
        ["uv", "run", "--no-sync", "python", "scripts/conformance_adapter.py"],
        cwd=str(_PYTHON_DIR),
        input=json.dumps(_adapter_request(fixture)),
        capture_output=True,
        text=True,
    )
    return _adapter_response("python", proc)


def _ts_src_hash() -> str:
    """Hash of the TS build INPUTS (src tree + tsconfig/package). Any change here
    means dist/ is stale and must be rebuilt -- so a stale build can never mask a
    src regression."""
    h = hashlib.sha256()
    h.update(_hash_tree(_TS_DIR / "src", (".ts",)).encode())
    for cfg in ("tsconfig.json", "package.json"):
        p = _TS_DIR / cfg
        if p.is_file():
            h.update(p.read_bytes())
    return h.hexdigest()[:16]


def _ensure_ts() -> None:
    """Build ts/dist ONCE per run, CACHED across runs by the src-input hash. If
    the recorded stamp matches the current src hash and dist exists, the build is
    skipped; otherwise ``npm run build`` runs and the stamp is refreshed. This is
    the ts analogue of the go gen-cache hygiene: the cache key is the toolchain/
    source state, so stale dist cannot mask a regression. A build failure raises.
    """
    global _ts_ready
    with _build_lock:
        if _ts_ready:
            return
        want = _ts_src_hash()
        have = None
        if _TS_BUILD_STAMP.is_file():
            try:
                have = json.loads(_TS_BUILD_STAMP.read_text()).get("src_hash")
            except (OSError, json.JSONDecodeError):
                have = None
        dist_ok = (_TS_DIR / "dist" / "index.js").is_file()
        if have != want or not dist_ok:
            subprocess.run(
                ["npm", "run", "build"],
                cwd=str(_TS_DIR),
                check=True,
                capture_output=True,
                text=True,
            )
            _TS_BUILD_STAMP.write_text(json.dumps({"src_hash": want}))
        _ts_ready = True


def _ts_invoke(fixture: Fixture) -> Outcome:
    """Run the generated-TS runtime over one fixture and return its outcome."""
    _ensure_ts()
    proc = subprocess.run(
        ["node", "scripts/conformance-adapter.mjs"],
        cwd=str(_TS_DIR),
        input=json.dumps(_adapter_request(fixture)),
        capture_output=True,
        text=True,
    )
    return _adapter_response("ts", proc)


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
# All FOUR targets are LIVE: the reference interpreter, the generated Go target,
# the generated Python target, and the generated TypeScript target. Cross-target
# parity is active across all four -- every fixture must produce a byte-identical
# outcome (verdict + code + path + message) on each.
_register(Target(name="interpreter", implemented=True, invoke=_interpreter_invoke))
_register(Target(name="python", implemented=True, invoke=_python_invoke))
_register(Target(name="go", implemented=True, invoke=_go_invoke))
_register(Target(name="ts", implemented=True, invoke=_ts_invoke))


def all_targets() -> list[Target]:
    """Every registered target, in registration (reporting) order."""
    return list(_REGISTRY.values())


def implemented_targets() -> list[Target]:
    return [t for t in _REGISTRY.values() if t.implemented]
