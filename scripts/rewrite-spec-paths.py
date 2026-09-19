#!/usr/bin/env python3
"""Rewrite the retired `spec/` path across the repository after the docs move.

The specification pages moved from `spec/` to `.stricttools/docs/` when this
repository adopted selfdoc's `.stricttools/` layout. Two kinds of mention are
left behind, and only one of them is mechanical:

  * A PATH mention names a file -- `spec/appendix-error-codes.md`. It is
    rewritten to `.stricttools/docs/appendix-error-codes.md`. That is what this
    script does.
  * A CONCEPT mention is the bare `spec/`, meaning the specification itself.
    Rewriting it to a directory name reads as a path that no longer means what
    it says, so this script reports those mentions and rewrites none of them.

Generated files are never rewritten: their generator spells the path, and the
generated file is regenerated from it.

    python3 scripts/rewrite-spec-paths.py --dry-run
    python3 scripts/rewrite-spec-paths.py --apply --expect 101
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

OLD = "spec/"
NEW = ".stricttools/docs/"

# Neither a word character, a slash, nor a hyphen may precede the mention, so
# `strictspec/` and `github.com/smm-h/strictspec/go` are left alone.
PATH_MENTION = re.compile(r"(?<![\w/-])" + re.escape(OLD) + r"(?=[A-Za-z0-9_-]+\.md)")
CONCEPT_MENTION = re.compile(r"(?<![\w/-])" + re.escape(OLD) + r"(?![A-Za-z0-9_-])")

# Directories whose files this sweep never touches.
SKIP_PREFIXES = ("todo/",)

# Generated files: their generator carries the path, and they are regenerated.
SKIP_FILES = {
    "go/internal/codes/catalogue_gen.go",
    "ts/src/codes.generated.ts",
    "python/src/strictspec/_codes.py",
    ".stricttools/docs-state/manifest.json",
}


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
    parser.add_argument("--expect", type=int, default=None,
                        help="refuse unless exactly this many path mentions are rewritten")
    args = parser.parse_args()

    project = Path(args.project).resolve()
    planned: list[tuple[str, str, int]] = []
    concepts: list[tuple[str, int, str]] = []
    total = 0

    for name in tracked(project):
        if name.startswith(SKIP_PREFIXES) or name in SKIP_FILES:
            continue
        path = project / name
        try:
            before = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        if OLD not in before:
            continue
        after, count = PATH_MENTION.subn(NEW, before)
        if count:
            planned.append((name, after, count))
            total += count
        for number, line in enumerate(after.split("\n"), start=1):
            if CONCEPT_MENTION.search(line):
                concepts.append((name, number, line.strip()))

    print(f"path mentions to rewrite: {total} in {len(planned)} file(s)")
    for name, _, count in planned:
        print(f"  {name}: {count}")
    print(f"concept mentions left for a human: {len(concepts)}")
    for name, number, line in concepts:
        print(f"  {name}:{number}: {line[:160]}")

    if args.expect is not None and total != args.expect:
        print(f"refused: planned {total} rewrites, expected {args.expect}", file=sys.stderr)
        return 1
    if total == 0:
        print("refused: no path mention matched", file=sys.stderr)
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
