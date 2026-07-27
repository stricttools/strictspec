import re

import strictspec


def test_import_and_version() -> None:
    # The exact value is asserted against pyproject in test_public.py (the drift
    # guard); here we only smoke-check that a well-formed version is exposed, so
    # this test survives release bumps.
    assert isinstance(strictspec.__version__, str)
    assert re.match(r"^\d+\.\d+\.\d+", strictspec.__version__)
