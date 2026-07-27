"""The acceptance test — strictspec vs a private project's legacy validators.

conformance/DESIGN.md, "The acceptance test": from the hand-written strictspec
translation of a private project's character-preview schema, GENERATE Python and TS
validators via ``strictspec gen``, run them over the real a private project corpus, and
assert VERDICT PARITY against a private project's existing hand-written validators
(pydantic + legacy TS) — STRICT except a committed, FROZEN waiver list where
divergence is the pass condition (strictspec's stricter-but-correct behaviour).

This module is the reusable machinery: it drives ``strictspec gen`` on the
conformance-owned schema copy, compiles and runs the generated Python and TS
validators over the committed corpus, and provides the path-normalization and
adjudication helpers the pytest entry point asserts on. The corpus and the legacy
verdicts are committed fixture data (``acceptance/``), so the test never reaches
into the a private project working tree at run time.

Strictspec verdicts come from the GENERATED validators (two of the four
conformance targets); because every target drives the shared emitter IR, the
generated Python and generated TS verdicts are byte-identical — the runner asserts
that too, so a single "strictspec verdict" is well-defined for the comparison.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from . import CONFORMANCE_DIR, REPO_ROOT
from .toolchain import ensure_cli

ACCEPTANCE_DIR = CONFORMANCE_DIR / "acceptance"
SCHEMA_FILE = ACCEPTANCE_DIR / "schema" / "character-preview.schema.toml"
CORPUS_DIR = ACCEPTANCE_DIR / "corpus"
LEGACY_VERDICTS_FILE = ACCEPTANCE_DIR / "legacy-verdicts.json"
WAIVERS_FILE = ACCEPTANCE_DIR / "waivers.toml"
EXAMPLES_SCHEMA = REPO_ROOT / "examples" / "private-project" / "character-preview.toml"

_PYTHON_DIR = REPO_ROOT / "python"
_TS_DIR = REPO_ROOT / "ts"
_GEN_ROOT = _TS_DIR / ".acceptance-gen"  # gitignored; TS gen must live under ts/ for self-import

# The schema's discriminated-union arm literals. strictspec annotates a matched
# arm in the path (`functions[1](blink)`); pydantic inserts the same literal as a
# loc element (`[..., 1, "blink", ...]`). Both are dropped during normalization so
# the two sides compare on a common canonical path (conformance/DESIGN.md:
# "index-then-key switching applied to both sides").
_ARM_TAGS = frozenset({"sine", "blink"})


# --- corpus / verdicts / waivers ---------------------------------------------


def corpus_documents() -> list[str]:
    """Sorted corpus document names (stems), excluding the provenance manifest."""
    return sorted(p.stem for p in CORPUS_DIR.glob("*.json"))


def load_legacy_verdicts() -> dict[str, Any]:
    data = json.loads(LEGACY_VERDICTS_FILE.read_text())
    return data["verdicts"]


@dataclass(frozen=True)
class Waiver:
    document: str
    legacy_targets: tuple[str, ...]
    defect: str
    strictspec_codes: tuple[str, ...]
    strictspec_paths: tuple[str, ...]


def load_waivers() -> list[Waiver]:
    data = tomllib.loads(WAIVERS_FILE.read_text())
    out: list[Waiver] = []
    for w in data.get("waivers", []):
        out.append(
            Waiver(
                document=w["document"],
                legacy_targets=tuple(w["legacy_targets"]),
                defect=w["defect"],
                strictspec_codes=tuple(w["strictspec_codes"]),
                strictspec_paths=tuple(w["strictspec_paths"]),
            )
        )
    return out


# --- path normalization -------------------------------------------------------


def normalize_strictspec_path(path: str) -> str:
    """Canonicalize a strictspec path: drop the `(arm)` match annotations so it
    compares against legacy paths, which have no arm concept."""
    return re.sub(r"\([^)]*\)", "", path)


def normalize_pydantic_loc(loc: list[Any]) -> str:
    """Canonicalize a pydantic error `loc` tuple to a strictspec-style path.

    Ints are array indices (`[i]`); strings are keys (`.key`). A discriminated-
    union arm literal that pydantic inserts immediately after an array index (e.g.
    `[..., 1, "blink"]`) is dropped — it is the arm annotation, not a field."""
    parts = ["$"]
    prev_int = False
    for elem in loc:
        if isinstance(elem, bool):  # guard: bool is an int subclass
            parts.append("." + str(elem))
            prev_int = False
        elif isinstance(elem, int):
            parts.append(f"[{elem}]")
            prev_int = True
        else:
            s = str(elem)
            if prev_int and s in _ARM_TAGS:
                # arm annotation, not a field — skip
                prev_int = False
                continue
            parts.append("." + s)
            prev_int = False
    return "".join(parts)


def normalize_ts_error(message: str) -> str:
    """Canonicalize a legacy-TS error string to a strictspec-style path.

    The legacy validator emits `${path}: ${message}`; the path is everything
    before the first ": ". The root is spelled `characterPreview`; rewrite it to
    `$`. Array indices already render `[i]`."""
    path = message.split(": ", 1)[0]
    if path == "characterPreview":
        return "$"
    if path.startswith("characterPreview."):
        return "$." + path[len("characterPreview."):]
    # Fallback: a path not rooted at characterPreview (should not occur for this
    # validator) — root it so comparison stays well-defined.
    return "$." + path


def _chain_compatible(legacy: str, strict_paths: set[str]) -> bool:
    """True if a normalized legacy path lies on the same root-to-node chain as
    some strictspec path — legacy is a prefix of, equal to, or has-as-prefix a
    strictspec path. This is the concrete realization of DESIGN's "normalized
    legacy path SET must be a subset of strictspec's": the legacy validators
    report at container- or leaf-granularity that strictspec does not always
    mirror exactly (pydantic stops at a model_validator's containing record;
    legacy TS drills to `functions[i].type`), so exact set-subset is unachievable.
    Chain compatibility asserts strictspec located the SAME structural region."""
    def segs(p: str) -> list[str]:
        # split "$.a.b[0].c" into ["$", ".a", ".b", "[0]", ".c"] token chain
        return re.findall(r"\$|\.[^.\[]+|\[[^\]]*\]", p)

    lsegs = segs(legacy)
    for s in strict_paths:
        ssegs = segs(s)
        n = min(len(lsegs), len(ssegs))
        if lsegs[:n] == ssegs[:n]:
            return True
    return False


def legacy_reject_paths(verdict: dict[str, Any], target: str) -> set[str]:
    """Normalized path set from one legacy target's raw rejection verdict."""
    out: set[str] = set()
    if target == "pydantic":
        for e in verdict.get("errors", []):
            out.add(normalize_pydantic_loc(e.get("loc", [])))
    elif target == "ts":
        for e in verdict.get("errors", []):
            out.add(normalize_ts_error(e))
    else:  # pragma: no cover - closed target set
        raise ValueError(f"unknown legacy target {target!r}")
    return out


def strictspec_reject_paths(diagnostics: list[dict[str, str]]) -> set[str]:
    return {normalize_strictspec_path(d["path"]) for d in diagnostics}


def subset_holds(legacy_paths: set[str], strict_paths: set[str]) -> bool:
    """Every normalized legacy path is chain-compatible with strictspec's set."""
    return all(_chain_compatible(lp, strict_paths) for lp in legacy_paths)


# --- generate + run the strictspec validators ---------------------------------


def _force_rmtree(path: Path) -> None:
    def _onerror(func, p, exc):  # generated files land chmod 444
        os.chmod(p, stat.S_IWRITE)
        func(p)

    shutil.rmtree(path, onerror=_onerror)


_PY_DRIVER = r'''
import importlib.util, json, sys
mod_path, cases_json = sys.argv[1], sys.argv[2]
spec = importlib.util.spec_from_file_location("gen_module", mod_path)
m = importlib.util.module_from_spec(spec); sys.modules["gen_module"] = m
spec.loader.exec_module(m)  # runs the pairing guard + compile-at-import
out = {}
for c in json.loads(cases_json):
    data = open(c["path"], "rb").read()
    _root, diags = m.validate_bytes_with_evidence(data, "json", None)
    out[c["name"]] = {"valid": len(diags) == 0,
                      "diagnostics": [{"code": d.code, "path": d.path} for d in diags]}
json.dump(out, sys.stdout)
'''

_TS_DRIVER = r'''
import { readFileSync } from "node:fs";
import { validateBytesWithEvidence } from "./character_preview_gen.js";
const cases = JSON.parse(process.argv[2]);
const out: Record<string, unknown> = {};
for (const c of cases) {
  const raw = readFileSync(c.path, "utf-8");
  const [, diags] = validateBytesWithEvidence(raw, "json", null);
  out[c.name] = { valid: diags.length === 0,
                  diagnostics: diags.map((d: any) => ({ code: d.code, path: d.path })) };
}
process.stdout.write(JSON.stringify(out));
'''

_TS_TSCONFIG = json.dumps(
    {
        "compilerOptions": {
            "module": "nodenext",
            "moduleResolution": "nodenext",
            "target": "es2023",
            "skipLibCheck": True,
            "strict": False,
            "types": ["node"],
            "outDir": "out",
        },
        "include": ["character_preview_gen.ts", "tsdriver.ts"],
    }
)


def _ensure_python_env() -> None:
    subprocess.run(
        ["uv", "sync", "--quiet"], cwd=str(_PYTHON_DIR), check=True,
        capture_output=True, text=True,
    )


def _ensure_ts_build() -> None:
    if (_TS_DIR / "dist" / "index.js").is_file():
        return
    subprocess.run(
        ["npm", "run", "build"], cwd=str(_TS_DIR), check=True,
        capture_output=True, text=True,
    )


@dataclass(frozen=True)
class StrictspecVerdict:
    valid: bool
    diagnostics: tuple[dict[str, str], ...]


def generate_and_run() -> dict[str, StrictspecVerdict]:
    """Run `strictspec gen` on the acceptance schema, then compile and run the
    generated Python AND TypeScript validators over the whole corpus. Assert the
    two targets agree (four-target identity), and return the unified verdicts.

    Raises on any tool failure — a broken pipeline is a hard error, never a skip.
    """
    cli = ensure_cli()
    _ensure_python_env()
    _ensure_ts_build()

    _GEN_ROOT.mkdir(parents=True, exist_ok=True)
    workdir = Path(tempfile.mkdtemp(prefix="case.", dir=str(_GEN_ROOT)))
    try:
        shutil.copyfile(SCHEMA_FILE, workdir / "character-preview.schema.toml")
        (workdir / "strictspec.toml").write_text(
            "format_version = 1\n\n"
            "[[schemas]]\n"
            'path = "character-preview.schema.toml"\n\n'
            "[[schemas.targets]]\n"
            'lang = "python"\n'
            'output = "character_preview_gen.py"\n\n'
            "[[schemas.targets]]\n"
            'lang = "ts"\n'
            'output = "character_preview_gen.ts"\n'
        )
        subprocess.run(
            [str(cli), "gen", "--manifest", str(workdir / "strictspec.toml")],
            check=True, capture_output=True, text=True,
        )

        cases = [
            {"name": n, "path": str(CORPUS_DIR / f"{n}.json")}
            for n in corpus_documents()
        ]
        cases_json = json.dumps(cases)

        py = _run_python(workdir, cases_json)
        ts = _run_ts(workdir, cases_json)
    finally:
        _force_rmtree(workdir)

    # Four-target identity: generated Python and generated TS must agree exactly.
    if set(py) != set(ts):
        raise AssertionError(f"python/ts verdict key mismatch: {set(py) ^ set(ts)}")
    unified: dict[str, StrictspecVerdict] = {}
    for name in py:
        if py[name] != ts[name]:
            raise AssertionError(
                f"generated python vs ts DIVERGED on {name!r}:\n"
                f"  python: {py[name]}\n  ts:     {ts[name]}"
            )
        v = py[name]
        unified[name] = StrictspecVerdict(
            valid=v["valid"],
            diagnostics=tuple(v["diagnostics"]),
        )
    return unified


def _run_python(workdir: Path, cases_json: str) -> dict[str, Any]:
    driver = workdir / "pydriver.py"
    driver.write_text(_PY_DRIVER)
    proc = subprocess.run(
        ["uv", "run", "--no-sync", "python", str(driver),
         str(workdir / "character_preview_gen.py"), cases_json],
        cwd=str(_PYTHON_DIR), capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"generated python validator failed:\n{proc.stderr}")
    return json.loads(proc.stdout)


def _run_ts(workdir: Path, cases_json: str) -> dict[str, Any]:
    (workdir / "tsdriver.ts").write_text(_TS_DRIVER)
    (workdir / "tsconfig.json").write_text(_TS_TSCONFIG)
    tsc = _TS_DIR / "node_modules" / ".bin" / "tsc"
    compile_proc = subprocess.run(
        [str(tsc), "-p", "tsconfig.json"], cwd=str(workdir),
        capture_output=True, text=True,
    )
    if compile_proc.returncode != 0:
        raise RuntimeError(
            f"generated ts failed to compile:\n{compile_proc.stdout}\n{compile_proc.stderr}"
        )
    run_proc = subprocess.run(
        ["node", str(workdir / "out" / "tsdriver.js"), cases_json],
        cwd=str(workdir), capture_output=True, text=True,
    )
    if run_proc.returncode != 0:
        raise RuntimeError(f"generated ts validator failed:\n{run_proc.stderr}")
    return json.loads(run_proc.stdout)
