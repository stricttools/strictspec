"""Meta-schema reader sweep over examples/** (port of the Go sweep_test.go):
every role-bearing schema/type-definition file reads with ZERO authoring
diagnostics, except the deliberately-invalid companions whose expected rejection
is asserted.
"""

import os
from pathlib import Path

import pytest

from strictspec import _doc as doc
from strictspec import _schema as schema
from strictspec import _tomldoc as tomldoc

_EXAMPLES = Path(__file__).resolve().parents[2] / "examples"

_KNOWN_INVALID = {
    "INVALID-types-transitive.toml": "STRICTSPEC_IMPORT_TRANSITIVE",
    "INVALID-types-with-constraint.toml": "STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
    "opaque-no-stance.reject.toml": "STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE",
    "unchecked-no-reason.reject.toml": "STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON",
}


def _schema_files():
    out = []
    for dp, _, files in os.walk(_EXAMPLES):
        for f in files:
            if not f.endswith(".toml"):
                continue
            p = os.path.join(dp, f)
            try:
                d = tomldoc.parse(Path(p).read_bytes())
            except doc.ParseError:
                continue
            role, _ok = schema._str_entry(d.root, "role")
            if role in ("schema", "type-definitions"):
                out.append((f, p, d))
    return out


@pytest.mark.skipif(not _EXAMPLES.exists(), reason="examples/ not found")
def test_examples_sweep():
    files = _schema_files()
    scanned = 0
    for base, path, d in files:
        scanned += 1
        s, diags = schema.read_schema(d.root, os.path.dirname(path))
        codes = [x.code for x in diags]
        if base in _KNOWN_INVALID:
            assert _KNOWN_INVALID[base] in codes, f"{base}: expected {_KNOWN_INVALID[base]}, got {codes}"
        else:
            assert codes == [], f"{base} ({s.name}): expected zero authoring diagnostics, got {codes}"
    assert scanned >= 30, f"sweep scanned only {scanned} schema files"
