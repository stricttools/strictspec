"""Command-line entry point for the conformance harness.

Mirrors the shape of strictcli's conformance runner: a single ``run`` over the
fixture tree that loads + structurally validates every fixture, executes each
registered target, and reports honestly (UNIMPLEMENTED is distinct from PASS).
Exit code is non-zero only on a malformed fixture, an implemented target's
mismatch, a non-invocable target, or a parity break.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from . import FIXTURES_ROOT
from .runner import format_report, run


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="strictspec-conformance",
        description="Run the strictspec cross-target conformance suite.",
    )
    parser.add_argument(
        "--fixtures",
        default=str(FIXTURES_ROOT),
        help="fixtures root directory (default: the bundled fixtures/)",
    )
    args = parser.parse_args(argv)

    report = run(Path(args.fixtures))
    print(format_report(report))
    return report.exit_code


if __name__ == "__main__":
    sys.exit(main())
