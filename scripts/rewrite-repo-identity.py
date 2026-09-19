#!/usr/bin/env python3
"""Rewrite the retired GitHub identity `smm-h/strictspec` to `stricttools/strictspec`.

The repository was transferred to the `stricttools` organisation, so every
spelling of the old owner names a repository GitHub only redirects: install
lines, release-asset URLs, package metadata, the docs config, and the Go module
path inside string literals the import rewriter does not reach.

`rlsbl rewrite go-module-path` already moved every `go.mod` token and every real
import site. What is left is text, and this sweep is that text.

Generated files are never rewritten: the root README is rendered from its
template by `selfdoc gen`.

    python3 scripts/rewrite-repo-identity.py --dry-run
    python3 scripts/rewrite-repo-identity.py --apply --expect 36
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

OLD = "smm-h/strictspec"
NEW = "stricttools/strictspec"

SKIP_PREFIXES = ("todo/",)
# Generated from `.stricttools/docs/_README.md` by `selfdoc gen`.
SKIP_FILES = {"README.md"}


def tracked(project: Path) -> list[str]:
    out = subprocess.run(
        ["git", "ls-files", "-z"], cwd=project, capture_output=True, text=True, check=True
    )
    return [name for name in out.stdout.split("\0") if name]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--apply", action="store_true")
    parser.add_argument("--project", default=".")
    parser.add_argument("--expect", type=int, default=None)
    args = parser.parse_args()

    project = Path(args.project).resolve()
    planned: list[tuple[str, str, int]] = []
    total = 0

    for name in tracked(project):
        if name.startswith(SKIP_PREFIXES) or name in SKIP_FILES:
            continue
        path = project / name
        try:
            before = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        count = before.count(OLD)
        if not count:
            continue
        planned.append((name, before.replace(OLD, NEW), count))
        total += count

    print(f"mentions to rewrite: {total} in {len(planned)} file(s)")
    for name, _, count in planned:
        print(f"  {name}: {count}")

    if args.expect is not None and total != args.expect:
        print(f"refused: planned {total} rewrites, expected {args.expect}", file=sys.stderr)
        return 1
    if total == 0:
        print("refused: no mention matched", file=sys.stderr)
        return 1
    if not args.apply:
        print("dry run: nothing was written.")
        return 0

    for name, after, _ in planned:
        (project / name).write_text(after, encoding="utf-8")
    print(f"rewrote {total} mention(s) in {len(planned)} file(s).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
