"""Conformance-adapter for the strictspec Python runtime.

The Python-side invocation contract for the cross-target conformance harness's
``python`` target. It reads a JSON request on stdin describing one fixture
(schema path + input document + optional cross-document evidence), drives the
PUBLIC Python runtime (``compile_embedded`` + ``Program.validate`` / the
meta-schema reader for meta fixtures), and writes the observed outcome as JSON on
stdout in the runner's expected shape::

    {"valid": bool, "diagnostics": [{"code","path","message"}, ...]}

This is the exact request/response contract the Go ``conformance-adapter`` speaks
(go/cmd/conformance-adapter/main.go); byte-identical outcomes across targets are
what the parity checker asserts. The harness prepares the Python environment once
(uv) and invokes this script per fixture -- the strictcli conformance pattern:
compile the schema once, feed the input document.
"""

from __future__ import annotations

import json
import os
import sys

import strictspec
from strictspec import _doc, _render, _schema


def _build_fileset(schema_path: str) -> tuple[dict[str, str], str]:
    """Read every ``.toml`` in the schema's directory into an in-memory FileSet,
    keyed by basename. This mirrors the Go adapter: imports reference sibling
    files by bare filename, and the scalar manifest is a sibling ``.toml`` that
    ``load_manifest_scalars_from`` merges across every file in the set.
    """
    directory = os.path.dirname(schema_path)
    files: dict[str, str] = {}
    for name in os.listdir(directory):
        full = os.path.join(directory, name)
        if os.path.isdir(full) or not name.endswith(".toml"):
            continue
        with open(full, encoding="utf-8") as f:
            files[name] = f.read()
    return files, os.path.basename(schema_path)


def _read_input(req: dict) -> bytes:
    inline = req.get("input_inline")
    if inline:
        return inline.encode("utf-8")
    return open(req["input_path"], "rb").read()


def _render_diags(diags: list) -> list[dict[str, str]]:
    return [
        {"code": d.code, "path": d.path.render(), "message": _render.render(d)}
        for d in diags
    ]


def _meta_validate(req: dict) -> list[dict[str, str]]:
    """Read the input file AS a schema/type-definition file and return the
    meta-schema reader's authoring diagnostics (mirrors Go's metaValidate)."""
    src = _read_input(req)
    try:
        d = _tomldoc_parse(src)
    except _doc.ParseError as pe:
        return _render_diags([strictspec._parse_diag(pe)])
    _, diags = _schema.read_schema(d.root, "")
    return _render_diags(diags)


def _tomldoc_parse(src: bytes):
    from strictspec import _tomldoc

    return _tomldoc.parse(src)


def run(req: dict) -> dict:
    schema_path = req["schema"]
    files, main = _build_fileset(schema_path)

    # Meta-schema mode: the fixture's "document" is itself a schema file, read AS
    # a document of the built-in meta-schema -- the reader's authoring diagnostics
    # ARE the outcome.
    s, _ = _schema.parse_from(files, main)
    if s.name == "strictspec-meta-schema":
        diagnostics = _meta_validate(req)
        return {"valid": len(diagnostics) == 0, "diagnostics": diagnostics}

    program = strictspec.compile_embedded(files, main)
    src = _read_input(req)
    evidence = req.get("evidence") or None
    result = program.validate_with_evidence(src, req["input_syntax"], evidence)
    diagnostics = [
        {"code": d.code, "path": d.path, "message": d.message}
        for d in result.diagnostics
    ]
    return {"valid": result.valid, "diagnostics": diagnostics}


def main() -> int:
    try:
        req = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        json.dump({"error": f"bad request: {exc}"}, sys.stderr)
        return 2
    try:
        resp = run(req)
    except Exception as exc:  # noqa: BLE001 -- surface any failure to the harness
        json.dump({"error": str(exc)}, sys.stderr)
        return 2
    json.dump(resp, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
