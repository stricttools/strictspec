#!/usr/bin/env python3
"""Reword the bare `spec/` mentions that mean the specification, not a path.

The specification pages moved to `.stricttools/docs/`, so a sentence spelling
`spec/` now spells a directory that does not exist. Where the mention means the
specification itself, the cure is the word rather than a new directory name.
Every replacement below is an exact string with an asserted occurrence count, so
a pattern that stops matching is a hard stop rather than a silent no-op.

    python3 scripts/reword-spec-concept.py --dry-run
    python3 scripts/reword-spec-concept.py --apply
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

AMENDMENT = (
    " AMENDED 2026-09-20: the specification pages moved out of `spec/` into "
    "`.stricttools/docs/` when the repository adopted selfdoc's `.stricttools/` "
    "layout, so the directory that holds them has a new name; the constitution, "
    "its home inside this repository, and the published site are unchanged."
)

# (file, old, new, expected occurrences)
EDITS: list[tuple[str, str, str, int]] = [
    # The decisions table.
    ("DESIGN.md",
     "are still WRITTEN NOW as normative, versioned spec/ sections",
     "are still WRITTEN NOW as normative, versioned specification sections", 1),
    ("DESIGN.md",
     "the strictspec LANGUAGE REFERENCE has the same home — the spec/ constitution",
     "the strictspec LANGUAGE REFERENCE has the same home — the specification constitution", 1),
    ("DESIGN.md",
     "versioned with the spec; the published site is the canonical citable manual |",
     "versioned with the spec; the published site is the canonical citable manual."
     + AMENDMENT + " |", 1),
    # The directory map.
    ("DESIGN.md",
     "- `spec/` — the constitution: schema language",
     "- `.stricttools/docs/` — the specification pages, the constitution: schema language", 1),
    # The donor inventory.
    ("DESIGN.md", "| go/ runtime; spec/ |", "| go/ runtime; the specification |", 1),
    ("DESIGN.md", "| spec/, examples/, conformance/ |",
     "| the specification, examples/, conformance/ |", 1),
    ("DESIGN.md", "donor only, not a consumer | spec/ |",
     "donor only, not a consumer | the specification |", 1),
    ("DESIGN.md", "| spec/, examples/ |", "| the specification, examples/ |", 1),
    # The retained pre-release record.
    ("DESIGN.md", "(2026-07-27): spec/ is\nredrafted in full",
     "(2026-07-27): the specification is\nredrafted in full", 1),
    ("DESIGN.md", "absorbed every finding into spec/ —",
     "absorbed every finding into the specification —", 1),
    ("DESIGN.md", "1. ~~Redraft spec/ in full.~~",
     "1. ~~Redraft the specification in full.~~", 1),

    ("conformance/DESIGN.md", "HAND-AUTHORED from spec/, never",
     "HAND-AUTHORED from the specification, never", 1),
    ("examples/DESIGN.md", "gap-note items resolved in spec/;",
     "gap-note items resolved in the specification;", 1),
    ("examples/migrations/gap-note.md", "Just as spec/ pins the language",
     "Just as the specification pins the language", 1),
    ("examples/pixelweaver/gap-note.md", "is not pinned anywhere in spec/",
     "is not pinned anywhere in the specification", 1),
    ("examples/predraw-scene/gap-note.md", "spec/ pins the language but not the authoring",
     "the specification pins the language but not the authoring", 1),

    ("go/DESIGN.md", "(see spec/, the version-boundary invariant)",
     "(see the specification, the version-boundary invariant)", 1),
    ("go/DESIGN.md", '(spec/, "Accepted-set semantics"', '(the specification, "Accepted-set semantics"', 1),
    ("go/DESIGN.md", '(spec/, "Domain checks"', '(the specification, "Domain checks"', 1),
    ("go/DESIGN.md", "taxonomy per spec/.", "taxonomy per the specification.", 1),

    ("python/DESIGN.md", "Diagnostics: the spec/ error model",
     "Diagnostics: the specification's error model", 1),
    ("python/DESIGN.md", "Scalar guards per spec/ (", "Scalar guards per the specification (", 1),

    ("ts/DESIGN.md", "invariant (spec/): browser runtimes",
     "invariant (the specification): browser runtimes", 1),

    # The moved pages, where the move script's mechanical rewrite put a
    # directory name where the specification itself was meant.
    (".stricttools/docs/DESIGN.md",
     "# .stricttools/docs/ — The strictspec Schema Language",
     "# The strictspec Schema Language", 1),
    (".stricttools/docs/DESIGN.md",
     "findings absorbed into .stricttools/docs/)",
     "findings absorbed into the specification)", 1),
    (".stricttools/docs/DESIGN.md",
     'description = "The strictspec constitution: the language-neutral definition',
     'description = "The strictspec constitution, the pages under .stricttools/docs/: '
     'the language-neutral definition', 1),
    (".stricttools/docs/_README.md",
     "The `.stricttools/docs/` constitution is the language-neutral definition",
     "The constitution under `.stricttools/docs/` is the language-neutral definition", 1),
]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--apply", action="store_true")
    parser.add_argument("--project", default=".")
    args = parser.parse_args()

    project = Path(args.project).resolve()
    pending: dict[str, str] = {}
    failures: list[str] = []

    for name, old, new, expected in EDITS:
        text = pending.get(name)
        if text is None:
            text = (project / name).read_text(encoding="utf-8")
        found = text.count(old)
        status = "ok" if found == expected else "MISMATCH"
        print(f"{status}: {name}: {found} occurrence(s), expected {expected}\n"
              f"    - {old[:110]}\n    + {new[:110]}")
        if found != expected:
            failures.append(f"{name}: {old[:60]!r} found {found}, expected {expected}")
            continue
        pending[name] = text.replace(old, new)

    if failures:
        print("refused:", file=sys.stderr)
        for line in failures:
            print(f"  {line}", file=sys.stderr)
        return 1
    if not args.apply:
        print(f"dry run: {len(EDITS)} edit(s) across {len(pending)} file(s); nothing written.")
        return 0
    for name, text in pending.items():
        (project / name).write_text(text, encoding="utf-8")
    print(f"applied {len(EDITS)} edit(s) across {len(pending)} file(s).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
