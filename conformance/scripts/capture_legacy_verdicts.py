#!/usr/bin/env python3
"""Capture a private project's LEGACY validator verdicts over the acceptance corpus.

The acceptance test (conformance/DESIGN.md, "The acceptance test") compares
strictspec's verdicts against a private project's EXISTING hand-written validators —
the pydantic model (``server/src/private-project/character_preview_generated.py``)
and the legacy TypeScript validator
(``src/lib/character/character-preview.generated.ts``). Those verdicts are
CAPTURED ONCE, read-only, and committed as fixture data
(``acceptance/legacy-verdicts.json``) so the test is reproducible and never
touches the a private project working tree at run time.

This script is the reproducible capture. Run it once (and again only when the
corpus or a legacy validator changes):

    cd conformance
    uv run python scripts/capture_legacy_verdicts.py \
        --private-project-root /home/m/Projects/private-project

It shells out to (a) a private project's uv environment for the pydantic model and
(b) ``node --experimental-strip-types`` for the legacy TS validator, running
each over every corpus document, and writes the RAW verdicts (pydantic loc
tuples + error types; TS error strings) to ``acceptance/legacy-verdicts.json``.

BOOTSTRAP: the legacy validators predate strictspec's ``format_version`` gate,
so ``format_version`` is stripped from each document before it is handed to a
legacy validator (decision 13). Path normalization and adjudication happen in
the test, not here — this file is the pure capture.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path

ACCEPTANCE = Path(__file__).resolve().parent.parent / "acceptance"
CORPUS = ACCEPTANCE / "corpus"
DEFAULT_OUTPUT = ACCEPTANCE / "legacy-verdicts.json"

# Driver run inside a private project's uv env: validate every corpus doc with the
# pydantic CharacterPreviewState model, exactly as a private project's state layer does
# (server/src/private-project/state.py::set_character_preview -> model_validate on a
# json.loads'd dict). format_version is stripped first (legacy predates it).
_PYDANTIC_DRIVER = r'''
import json, sys
sys.path.insert(0, sys.argv[3])  # <pw_root>/server/src
from a private project.character_preview_generated import CharacterPreviewState

corpus_dir, out_path = sys.argv[1], sys.argv[2]
import os
result = {}
for name in sorted(os.listdir(corpus_dir)):
    if not name.endswith(".json"):
        continue
    doc = json.loads(open(os.path.join(corpus_dir, name)).read())
    doc.pop("format_version", None)  # bootstrap: legacy predates the gate
    try:
        CharacterPreviewState.model_validate(doc)
        result[name[:-5]] = {"valid": True, "errors": []}
    except Exception as exc:
        errs = []
        try:
            for e in exc.errors():
                errs.append({"loc": list(e["loc"]), "type": e["type"]})
        except Exception:
            errs.append({"loc": [], "type": "unknown", "msg": str(exc)[:200]})
        result[name[:-5]] = {"valid": False, "errors": errs}
json.dump(result, open(out_path, "w"), indent=2)
'''

# Driver run under node --experimental-strip-types: validate every corpus doc
# with the legacy TS validator, imported by absolute path. Legacy parses with
# JSON.parse (like a private project's frontend); format_version is stripped first.
_TS_DRIVER = r'''
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
const [corpusDir, outPath, validatorPath] = process.argv.slice(2);
const { validateCharacterPreviewState } = await import(validatorPath);
const result: Record<string, unknown> = {};
for (const name of readdirSync(corpusDir).sort()) {
  if (!name.endsWith(".json")) continue;
  const doc = JSON.parse(readFileSync(join(corpusDir, name), "utf-8"));
  delete (doc as Record<string, unknown>).format_version;
  const errs = validateCharacterPreviewState(doc);
  result[name.slice(0, -5)] = { valid: errs.length === 0, errors: errs };
}
writeFileSync(outPath, JSON.stringify(result, null, 2));
'''


def _capture_pydantic(pw_root: Path, tmp: Path) -> dict:
    driver = tmp / "pydantic_driver.py"
    driver.write_text(_PYDANTIC_DRIVER)
    out = tmp / "pydantic.json"
    server_src = pw_root / "server" / "src"
    if not server_src.is_dir():
        raise SystemExit(f"a private project server/src not found under {pw_root}")
    subprocess.run(
        ["uv", "run", "python", str(driver), str(CORPUS), str(out), str(server_src)],
        cwd=str(pw_root),
        check=True,
    )
    return json.loads(out.read_text())


def _capture_ts(pw_root: Path, tmp: Path) -> dict:
    driver = tmp / "ts_driver.mts"
    driver.write_text(_TS_DRIVER)
    out = tmp / "ts.json"
    validator = pw_root / "src" / "lib" / "character" / "character-preview.generated.ts"
    if not validator.is_file():
        raise SystemExit(f"a private project legacy TS validator not found: {validator}")
    subprocess.run(
        ["node", "--experimental-strip-types", str(driver),
         str(CORPUS), str(out), str(validator)],
        check=True,
    )
    return json.loads(out.read_text())


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--private-project-root",
        default="/home/m/Projects/private-project",
        help="path to the a private project working tree (read-only)",
    )
    parser.add_argument("--output", default=str(DEFAULT_OUTPUT))
    args = parser.parse_args(argv)

    pw_root = Path(args.private-project_root).resolve()
    with tempfile.TemporaryDirectory() as td:
        tmp = Path(td)
        pydantic = _capture_pydantic(pw_root, tmp)
        ts = _capture_ts(pw_root, tmp)

    names = sorted(p.stem for p in CORPUS.glob("*.json"))
    verdicts: dict[str, dict] = {}
    for name in names:
        if name not in pydantic or name not in ts:
            raise SystemExit(f"legacy capture missing verdict for corpus doc {name!r}")
        verdicts[name] = {"pydantic": pydantic[name], "ts": ts[name]}

    payload = {
        "_meta": {
            "captured_by": "conformance/scripts/capture_legacy_verdicts.py",
            "capture_command": (
                "cd conformance && uv run python scripts/capture_legacy_verdicts.py "
                "--private-project-root <a private project>"
            ),
            "sources": {
                "pydantic": (
                    "a private project server/src/private-project/character_preview_generated.py "
                    "(CharacterPreviewState.model_validate, as state.py::set_character_preview does)"
                ),
                "ts": (
                    "a private project src/lib/character/character-preview.generated.ts "
                    "(validateCharacterPreviewState, run under node --experimental-strip-types)"
                ),
            },
            "bootstrap_note": (
                "format_version is stripped from every document before legacy validation: "
                "the legacy validators predate strictspec's format_version gate (decision 13)."
            ),
            "raw_shape": (
                "pydantic errors carry loc (list, ints are array indices) + type; "
                "ts errors are the validator's raw message strings. Path normalization and "
                "waiver adjudication happen in the test, not in this committed capture."
            ),
        },
        "verdicts": verdicts,
    }
    Path(args.output).write_text(json.dumps(payload, indent=2) + "\n")
    print(f"wrote {args.output} ({len(verdicts)} documents x 2 legacy validators)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
