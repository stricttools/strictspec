#!/usr/bin/env python3
"""Rewrite the emitted-shape pin (``conformance/pinned/generated-code-shape.json``).

Run this ONLY as the second half of a deliberate generated-code format bump:
the emitters write a new shape, every runtime's accepted range moves to the new
format, and then the pin records what the emitters now write. Running it to
silence a failing pin without bumping the format is the exact defect the pin
exists to catch -- generated code in the wild would keep importing against a
runtime that no longer matches it.

    uv run python scripts/pin_generated_code_shape.py [--check]

``--check`` rewrites nothing and exits non-zero when the pin is stale.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from harness import pairing  # noqa: E402


def build_pin() -> dict:
    observed = pairing.observe_all()
    formats = {p.max_format for p in observed.values()}
    if len(formats) != 1:
        raise SystemExit(
            "the runtimes disagree on the generated-code format they write: "
            f"{ {p.port: p.max_format for p in observed.values()} }"
        )
    sources = pairing.emit_sources()
    return {
        "generated_code_format": formats.pop(),
        "schema": pairing.PIN_SCHEMA,
        "shapes": {lang: pairing.shape_hash(src) for lang, src in sources.items()},
    }


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--check",
        action="store_true",
        help="exit non-zero if the pin on disk differs; write nothing",
    )
    args = ap.parse_args()

    pin = build_pin()
    rendered = json.dumps(pin, indent=2, sort_keys=True) + "\n"
    if args.check:
        current = pairing.PIN_PATH.read_text(encoding="utf-8")
        if current != rendered:
            print("pin is stale:\n" + rendered, file=sys.stderr)
            return 1
        print("pin is current")
        return 0
    pairing.PIN_PATH.parent.mkdir(parents=True, exist_ok=True)
    pairing.PIN_PATH.write_text(rendered, encoding="utf-8")
    print(f"wrote {pairing.PIN_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
