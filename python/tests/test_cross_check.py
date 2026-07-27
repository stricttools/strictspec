"""Cross-target parity: the Python runtime must produce IDENTICAL ordered
(code, path, message) output to the Go reference interpreter on representative
example fixtures.

The Go conformance-adapter binary is built once (via `go build`) into a
temporary directory (read-only w.r.t. the repo) and invoked per fixture with the
same JSON request the conformance harness uses. Any divergence fails the test;
Go is the reference (a Python divergence is a Python bug to fix).
"""

import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

import pytest

import strictspec as ss
from strictspec import _doc as _doc
from strictspec import _ir as _ir
from strictspec import _jsondoc as _jsondoc
from strictspec import _render as _render
from strictspec import _schema as _schema
from strictspec import _tomldoc as _tomldoc

_REPO = Path(__file__).resolve().parents[2]
_GO = _REPO / "go"
_EXAMPLES = _REPO / "examples"

# 6 representative (schema, input, syntax) fixtures spanning: valid, JSON type/
# regex/union errors, datetime kind, enum, intra-document constraint, and JSONL
# per-line anchors.
FIXTURES = [
    ("shared-types-exercise/schema-canvas.toml", "shared-types-exercise/canvas.valid.json", "json"),
    ("shared-types-exercise/schema-canvas.toml", "shared-types-exercise/canvas.invalid.json", "json"),
    ("datetime-exercise/schema-datetime.toml", "datetime-exercise/invalid-01-kind-and-range.json", "json"),
    ("enum-baking-exercise/schema-drum-pattern.toml", "enum-baking-exercise/pattern.invalid.json", "json"),
    ("rlsbl-config/config.schema.toml", "rlsbl-config/invalid-01-launcher.json", "json"),
    (
        "rlsbl-changelog-entry/changelog-entry-commit-mode.schema.toml",
        "rlsbl-changelog-entry/invalid-commit-mode.jsonl",
        "jsonl",
    ),
]


def _py_validate(schema_path: str, input_path: str, syntax: str):
    """Python analogue of the Go conformance-adapter run(): load schema from
    disk, resolve imports, discover manifest scalars, then validate the input.
    Returns the {valid, diagnostics:[{code,path,message}]} shape.
    """
    s, sdiags = _schema.load_file(schema_path)
    sdiags = list(sdiags) + _schema.resolve_imports(s)
    if sdiags:
        return _shape(sdiags)
    scalars = _schema.load_manifest_scalars(s.dir)
    prog = _ir.compile_program(s, scalars)
    src = Path(input_path).read_bytes()
    if syntax == "jsonl":
        docs = _jsondoc.parse_lines(src)
        starts = _line_starts(src)
        out = []
        for i, d in enumerate(docs):
            ls = starts[i] if i < len(starts) else 0
            out.extend(
                _ir.execute(
                    prog,
                    d.root,
                    _ir.ExecOptions(format=_doc.FORMAT_JSONL, jsonl=True, line=i + 1, line_start=ls),
                )
            )
        return _shape(out)
    if syntax == "toml":
        d = _tomldoc.parse(src)
        diags = _ir.execute(prog, d.root, _ir.ExecOptions(format=_doc.FORMAT_TOML))
        return _shape(diags)
    d = _jsondoc.parse(src)
    diags = _ir.execute(prog, d.root, _ir.ExecOptions(format=_doc.FORMAT_JSON))
    return _shape(diags)


def _shape(diags):
    return {
        "valid": len(diags) == 0,
        "diagnostics": [
            {"code": d.code, "path": d.path.render(), "message": _render.render(d)} for d in diags
        ],
    }


def _line_starts(src: bytes):
    starts = [0]
    for i, b in enumerate(src):
        if b == 0x0A:
            starts.append(i + 1)
    return starts


@pytest.fixture(scope="module")
def go_adapter():
    if shutil.which("go") is None:
        pytest.skip("go toolchain not available")
    tmp = tempfile.mkdtemp(prefix="strictspec-adapter-")
    binary = os.path.join(tmp, "conformance-adapter")
    proc = subprocess.run(
        ["go", "build", "-o", binary, "./cmd/conformance-adapter"],
        cwd=_GO,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        shutil.rmtree(tmp, ignore_errors=True)
        pytest.skip(f"go build failed: {proc.stderr}")
    yield binary
    shutil.rmtree(tmp, ignore_errors=True)


def _run_go(binary: str, schema_path: str, input_path: str, syntax: str):
    req = json.dumps(
        {
            "schema": schema_path,
            "input_path": input_path,
            "input_syntax": syntax,
            "evidence": {},
        }
    )
    proc = subprocess.run(
        [binary], input=req, capture_output=True, text=True, cwd=str(_REPO)
    )
    assert proc.returncode == 0, f"go adapter failed: {proc.stderr}"
    return json.loads(proc.stdout)


@pytest.mark.skipif(not _EXAMPLES.exists(), reason="examples/ not found")
@pytest.mark.parametrize("schema_rel,input_rel,syntax", FIXTURES)
def test_go_python_parity(go_adapter, schema_rel, input_rel, syntax):
    schema_path = str(_EXAMPLES / schema_rel)
    input_path = str(_EXAMPLES / input_rel)
    go_out = _run_go(go_adapter, schema_path, input_path, syntax)
    py_out = _py_validate(schema_path, input_path, syntax)
    assert py_out == go_out, (
        f"parity mismatch for {input_rel}\n"
        f"GO:     {json.dumps(go_out, indent=2)}\n"
        f"PYTHON: {json.dumps(py_out, indent=2)}"
    )
