"""Cross-port observation of the generated-code format pairing contract.

Generated code declares the generated-code FORMAT it was written to
(``GENERATED_CODE_FORMAT``); each runtime declares the inclusive range of formats
it reads. Pairing succeeds when the declared format is in the runtime's range,
whatever release produced the file -- the release string (``GENERATED_BY``) is
information, never the contract.

Three runtimes implement that contract independently, so it is exactly the kind
of cross-port fact this suite exists to hold in lockstep. This module OBSERVES
each port by executing it -- the accepted range it declares and the refusal text
it renders -- and generates the emitted sources for one pinned schema, so the
tests can assert byte identity across ports and pin the emitted shape against
silent change.

Nothing here asserts; ``tests/test_generated_code_format.py`` does.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path

from . import CONFORMANCE_DIR, FIXTURES_ROOT, REPO_ROOT
from .targets import _ensure_python, _ensure_ts  # prepared runtimes (uv env / tsc build)
from .toolchain import ensure_cli

GO_DIR = REPO_ROOT / "go"
PYTHON_DIR = REPO_ROOT / "python"
TS_DIR = REPO_ROOT / "ts"

#: The pinned emitted-shape record: the generated-code format plus one hash per
#: target language. Any change to what the emitters write moves a hash, and the
#: format must move with it (the bump discipline).
PIN_PATH = CONFORMANCE_DIR / "pinned" / "generated-code-shape.json"

#: The schema the emitted-shape pin is taken over: it imports a type-definition
#: file, registers custom scalars, and declares all three targets, so it
#: exercises the whole emitted surface.
PIN_SCHEMA = "shared-canvas.toml"

#: The release string the probes claim as the generating release. It is
#: deliberately NOT the runtime's own version: a supported format must pair
#: regardless of which release produced the file.
PROBE_GENERATED_BY = "0.0.1-probe"

PORTS = ("go", "python", "ts")


@dataclass(frozen=True)
class PortPairing:
    """What one runtime declares and renders, observed by running it."""

    port: str
    version: str
    min_format: int
    max_format: int
    #: Rendered refusal for a format one past the accepted range.
    unread_format_message: str
    #: Rendered refusal for generated code that predates the format declaration.
    absent_format_message: str


_PY_PROBE = """
import json
import strictspec as s

generated_by = {gb!r}
print(json.dumps({{
    "version": s.Version,
    "min_format": s.MIN_GENERATED_CODE_FORMAT,
    "max_format": s.MAX_GENERATED_CODE_FORMAT,
    "unread_format_message": s.check_generated_code_format(
        s.MAX_GENERATED_CODE_FORMAT + 1, generated_by
    ),
    "absent_format_message": s.check_runtime_version(generated_by),
}}))
"""

_TS_PROBE = """
import(process.argv[1]).then((s) => {
  const generatedBy = process.argv[2];
  process.stdout.write(JSON.stringify({
    version: s.VERSION,
    min_format: s.MIN_GENERATED_CODE_FORMAT,
    max_format: s.MAX_GENERATED_CODE_FORMAT,
    unread_format_message: s.checkGeneratedCodeFormat(
      s.MAX_GENERATED_CODE_FORMAT + 1, generatedBy),
    absent_format_message: s.checkRuntimeVersion(generatedBy),
  }));
});
"""

_GO_PROBE = """package main

import (
	"encoding/json"
	"os"

	"github.com/stricttools/strictspec/go/strictspec"
)

func main() {
	generatedBy := os.Args[1]
	json.NewEncoder(os.Stdout).Encode(map[string]any{
		"version":    strictspec.Version,
		"min_format": strictspec.MinGeneratedCodeFormat,
		"max_format": strictspec.MaxGeneratedCodeFormat,
		"unread_format_message": strictspec.CheckGeneratedCodeFormat(
			strictspec.MaxGeneratedCodeFormat+1, generatedBy).Error(),
		"absent_format_message": strictspec.CheckRuntimeVersion(generatedBy).Error(),
	})
}
"""

_GO_PROBE_MOD = """module ssprobe

go 1.26

require github.com/stricttools/strictspec/go v0.0.0

replace github.com/stricttools/strictspec/go => {go_dir}
"""


def _run(argv: list[str], *, cwd: Path, label: str) -> str:
    proc = subprocess.run(argv, cwd=str(cwd), capture_output=True, text=True)
    if proc.returncode != 0:
        raise RuntimeError(
            f"{label} probe failed (exit {proc.returncode}):\n{proc.stdout}\n{proc.stderr}"
        )
    return proc.stdout


def _python_pairing() -> PortPairing:
    _ensure_python()
    out = _run(
        ["uv", "run", "--no-sync", "python", "-c", _PY_PROBE.format(gb=PROBE_GENERATED_BY)],
        cwd=PYTHON_DIR,
        label="python",
    )
    return PortPairing(port="python", **json.loads(out))


def _ts_pairing() -> PortPairing:
    _ensure_ts()
    dist = (TS_DIR / "dist" / "index.js").as_uri()
    out = _run(
        ["node", "-e", _TS_PROBE, dist, PROBE_GENERATED_BY],
        cwd=TS_DIR,
        label="ts",
    )
    return PortPairing(port="ts", **json.loads(out))


def _go_pairing() -> PortPairing:
    # A throwaway module with a replace directive onto the runtime, the same
    # pattern emit.Build uses for generated validators: copying the runtime's
    # go.sum satisfies verification with no network access.
    with tempfile.TemporaryDirectory(prefix="strictspec-pairing-probe-") as tmp:
        mod = Path(tmp)
        (mod / "go.mod").write_text(_GO_PROBE_MOD.format(go_dir=GO_DIR))
        (mod / "main.go").write_text(_GO_PROBE)
        go_sum = GO_DIR / "go.sum"
        if go_sum.is_file():
            shutil.copyfile(go_sum, mod / "go.sum")
        env_run = ["go", "run", "."]
        proc = subprocess.run(
            env_run + [PROBE_GENERATED_BY],
            cwd=str(mod),
            capture_output=True,
            text=True,
            env={**_go_env()},
        )
        if proc.returncode != 0:
            raise RuntimeError(
                f"go probe failed (exit {proc.returncode}):\n{proc.stdout}\n{proc.stderr}"
            )
        return PortPairing(port="go", **json.loads(proc.stdout))


def _go_env() -> dict[str, str]:
    return {**os.environ, "GOFLAGS": "-mod=mod", "GOWORK": "off"}


def observe_all() -> dict[str, PortPairing]:
    """Run every runtime and return what each one declares and renders."""
    return {
        "go": _go_pairing(),
        "python": _python_pairing(),
        "ts": _ts_pairing(),
    }


# --- the emitted shape --------------------------------------------------------

_MANIFEST = """format_version = 1
[[schemas]]
path = "{schema}"
[[schemas.targets]]
lang = "go"
output = "gen/validator_gen.go"
package = "gen"
[[schemas.targets]]
lang = "python"
output = "gen/validator_generated.py"
[[schemas.targets]]
lang = "ts"
output = "gen/validator.gen.ts"
"""

_EMITTED_FILES = {
    "go": "gen/validator_gen.go",
    "python": "gen/validator_generated.py",
    "ts": "gen/validator.gen.ts",
}

#: ``GENERATED_BY = "0.2.5"`` / ``export const GENERATED_BY = "0.2.5";`` -- the
#: declaration whose VALUE moves on every release. The shape pin normalizes it
#: away, so the pin tracks the emitted SHAPE and nothing else.
_RELEASE_IN_SOURCE = re.compile(r'GENERATED_BY = "[^"]*"')
_RELEASE_IN_HEADER = re.compile(r"strictspec generator: .*")


def emit_sources() -> dict[str, str]:
    """Generate the pinned schema for all three targets with the real CLI and
    return the emitted source per target language.
    """
    cli = ensure_cli()
    with tempfile.TemporaryDirectory(prefix="strictspec-emitted-shape-") as tmp:
        work = Path(tmp)
        for path in FIXTURES_ROOT.joinpath("_schemas").iterdir():
            if path.is_file():
                shutil.copyfile(path, work / path.name)
        (work / "strictspec.toml").write_text(_MANIFEST.format(schema=PIN_SCHEMA))
        _run([str(cli), "gen"], cwd=work, label="gen")
        return {
            lang: (work / rel).read_text(encoding="utf-8")
            for lang, rel in _EMITTED_FILES.items()
        }


def shape_hash(source: str) -> str:
    """The emitted shape of one source, with the generating release normalized
    away: a release bump must NOT move this hash, and any change to what the
    emitter writes must.
    """
    normalized = _RELEASE_IN_SOURCE.sub('GENERATED_BY = "<release>"', source)
    normalized = _RELEASE_IN_HEADER.sub("strictspec generator: <release>", normalized)
    return hashlib.sha256(normalized.encode("utf-8")).hexdigest()


def read_pin() -> dict:
    return json.loads(PIN_PATH.read_text(encoding="utf-8"))


def declared_constant(source: str, name: str) -> str:
    """The value of one emitted declaration, whatever the target language's
    syntax around it (``NAME = value``, ``const NAME = value``, trailing ``;``).
    """
    m = re.search(rf"^(?:export )?(?:const )?{re.escape(name)} = (.+?);?$", source, re.M)
    if m is None:
        raise AssertionError(f"emitted source declares no {name}")
    return m.group(1).strip()
