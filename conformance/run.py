#!/usr/bin/env python3
"""Conformance runner entry point (``python run.py`` / ``uv run python run.py``).

Thin wrapper over ``harness.cli`` so the suite runs the same way strictcli's
conformance ``run.py`` does. The console-script ``strictspec-conformance``
(pyproject) points at the same ``main``.
"""

from __future__ import annotations

import sys

from harness.cli import main

if __name__ == "__main__":
    sys.exit(main())
