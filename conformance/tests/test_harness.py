"""Tests for the conformance harness itself.

These exercise the machinery (fixture loading, structural validation, honest
reporting, exit-code semantics, parity activation) AND run the real fixture tree
so `uv run pytest` in conformance/ validates the whole suite.
"""

from __future__ import annotations

import textwrap
from pathlib import Path

import pytest

from harness import FIXTURES_ROOT, templates
from harness.fixtures import MalformedFixture, load_fixture
from harness.paths import is_valid_path
from harness.runner import Status, run
from harness.targets import Outcome, Target, all_targets, implemented_targets
from harness import runner as runner_mod
from harness import parity as parity_mod


# --- The real fixture tree ----------------------------------------------------


def test_real_fixture_tree_is_green():
    report = run(FIXTURES_ROOT)
    assert report.malformed == [], f"malformed fixtures: {report.malformed}"
    assert report.exit_code == 0, runner_mod.format_report(report)
    assert report.fixture_count > 0


def test_interpreter_live_others_stub():
    # The reference interpreter (Phase 5.4) is implemented: every fixture PASSes
    # on it and none MISMATCHes / is NOT_INVOCABLE. The generated python/go/ts
    # targets remain declared stubs, so each contributes UNIMPLEMENTED per fixture.
    report = run(FIXTURES_ROOT)
    assert report.count(Status.PASS) == report.fixture_count
    assert report.count(Status.MISMATCH) == 0
    assert report.count(Status.NOT_INVOCABLE) == 0
    stub_count = len(all_targets()) - len(implemented_targets())
    assert report.count(Status.UNIMPLEMENTED) == report.fixture_count * stub_count


def test_four_targets_interpreter_implemented():
    names = [t.name for t in all_targets()]
    assert names == ["interpreter", "python", "go", "ts"]
    impl = [t.name for t in implemented_targets()]
    assert impl == ["interpreter"]


# --- Template catalogue -------------------------------------------------------


def test_every_used_code_is_in_catalogue():
    report_dir = FIXTURES_ROOT
    for toml in report_dir.rglob("*.toml"):
        if any(p.startswith("_") for p in toml.relative_to(report_dir).parts[:-1]):
            continue
        fx = load_fixture(toml, report_dir)
        for d in fx.diagnostics:
            assert d.code in templates.CATALOGUE


def test_render_fills_all_slots():
    msg = templates.render(
        "STRICTSPEC_INTRA_FORBIDDEN_WHEN",
        {"key": "q", "path": "$.a.q", "condition": "x in [1]"},
    )
    assert msg == "Field q at $.a.q is forbidden when x in [1]."
    assert "{" not in msg and "}" not in msg


def test_suggestion_slot_is_optional():
    msg = templates.render(
        "STRICTSPEC_KEY_UNKNOWN", {"key": "wobble", "path": "$.a"}
    )
    assert msg == "Unknown key wobble at $.a."


# --- Path grammar -------------------------------------------------------------


@pytest.mark.parametrize(
    "path",
    [
        "$",
        "$.a",
        "$.a.b",
        "$.items[0]",
        '$.headers["Content-Type"]',
        "$.shape(gradient).stops[0]",
        '$.embedded_bank["bad_filter"](subtractive).params.q',
        "$.budget@L42:17",
        "$@L42:0",
        "$.a-b.c_d",
    ],
)
def test_valid_paths(path):
    assert is_valid_path(path), path


@pytest.mark.parametrize(
    "path",
    [
        "",
        "a.b",              # missing root
        "$.1bad",           # key starts with digit
        "$[0",              # unbalanced
        "$.a@L1",           # jsonl suffix missing byte offset
        "$.a.",             # trailing dot
    ],
)
def test_invalid_paths(path):
    assert not is_valid_path(path), path


# --- Structural validation (malformed fixtures are hard errors) ---------------


def _write(tmp_path: Path, body: str, name: str = "fx.toml") -> Path:
    root = tmp_path / "fixtures"
    (root / "_schemas").mkdir(parents=True, exist_ok=True)
    (root / "_schemas" / "s.toml").write_text("name='s'\n")
    (root / "cat").mkdir(parents=True, exist_ok=True)
    p = root / "cat" / name
    p.write_text(textwrap.dedent(body))
    return p


def _base(tmp_path: Path, extra: str) -> Path:
    return _write(
        tmp_path,
        """
        name = "t"
        category = "numbers"
        provenance = "hand-authored"
        schema = "_schemas/s.toml"
        input_inline = "{}"
        input_syntax = "json"
        """
        + extra,
    )


def test_unknown_code_is_malformed(tmp_path):
    p = _base(
        tmp_path,
        """
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_NOT_A_REAL_CODE"
        path = "$.a"
        """,
    )
    with pytest.raises(MalformedFixture, match="not in the pinned catalogue"):
        load_fixture(p, p.parents[1])


def test_bad_path_is_malformed(tmp_path):
    p = _base(
        tmp_path,
        """
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_TYPE_NOT_INTEGER"
        path = "not-a-path"
        slots = { got = "float" }
        """,
    )
    with pytest.raises(MalformedFixture, match="path grammar"):
        load_fixture(p, p.parents[1])


def test_unknown_slot_is_malformed(tmp_path):
    p = _base(
        tmp_path,
        """
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_TYPE_NOT_INTEGER"
        path = "$.a"
        slots = { got = "float", bogus = "x" }
        """,
    )
    with pytest.raises(MalformedFixture, match="unknown slot"):
        load_fixture(p, p.parents[1])


def test_missing_required_slot_is_malformed(tmp_path):
    p = _base(
        tmp_path,
        """
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_INTRA_FORBIDDEN_WHEN"
        path = "$.a"
        slots = { key = "q" }
        """,
    )
    with pytest.raises(MalformedFixture, match="missing required slot"):
        load_fixture(p, p.parents[1])


def test_unknown_category_is_malformed(tmp_path):
    p = _write(
        tmp_path,
        """
        name = "t"
        category = "not-a-category"
        provenance = "hand-authored"
        schema = "_schemas/s.toml"
        input_inline = "{}"
        input_syntax = "json"
        [expected]
        valid = true
        """,
    )
    with pytest.raises(MalformedFixture, match="unknown category"):
        load_fixture(p, p.parents[1])


def test_valid_and_diagnostics_are_mutually_exclusive(tmp_path):
    p = _base(
        tmp_path,
        """
        [expected]
        valid = true
        [[expected.diagnostics]]
        code = "STRICTSPEC_TYPE_NOT_INTEGER"
        path = "$.a"
        slots = { got = "float" }
        """,
    )
    with pytest.raises(MalformedFixture):
        load_fixture(p, p.parents[1])


def test_malformed_fixture_fails_run(tmp_path):
    _base(
        tmp_path,
        """
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_NOT_A_REAL_CODE"
        path = "$.a"
        """,
    )
    report = run(tmp_path / "fixtures")
    assert report.malformed
    assert report.exit_code == 1


# --- Honest reporting + exit-code semantics (with simulated targets) ----------


def _one_fixture(tmp_path: Path):
    _write(
        tmp_path,
        """
        name = "sim"
        category = "numbers"
        provenance = "hand-authored"
        schema = "_schemas/s.toml"
        input_inline = "{}"
        input_syntax = "json"
        [expected]
        valid = false
        [[expected.diagnostics]]
        code = "STRICTSPEC_TYPE_NOT_INTEGER"
        path = "$.seed"
        slots = { got = "float" }
        """,
    )
    return tmp_path / "fixtures"


def test_implemented_target_pass(tmp_path, monkeypatch):
    root = _one_fixture(tmp_path)

    def good_invoke(fx):
        from harness.targets import ObservedDiagnostic

        return Outcome(
            valid=False,
            diagnostics=(
                ObservedDiagnostic(
                    "STRICTSPEC_TYPE_NOT_INTEGER",
                    "$.seed",
                    "Expected an integer at $.seed, got float.",
                ),
            ),
        )

    monkeypatch.setattr(
        runner_mod,
        "all_targets",
        lambda: [Target("python", implemented=True, invoke=good_invoke)],
    )
    monkeypatch.setattr(
        runner_mod,
        "implemented_targets",
        lambda: [Target("python", implemented=True, invoke=good_invoke)],
    )
    report = run(root)
    assert report.count(Status.PASS) == 1
    assert report.exit_code == 0


def test_implemented_target_mismatch_fails(tmp_path, monkeypatch):
    root = _one_fixture(tmp_path)

    def wrong_invoke(fx):
        return Outcome(valid=True)

    monkeypatch.setattr(
        runner_mod,
        "all_targets",
        lambda: [Target("python", implemented=True, invoke=wrong_invoke)],
    )
    monkeypatch.setattr(runner_mod, "implemented_targets", lambda: [])
    report = run(root)
    assert report.count(Status.MISMATCH) == 1
    assert report.exit_code == 1


def test_non_invocable_implemented_target_fails(tmp_path, monkeypatch):
    root = _one_fixture(tmp_path)
    # implemented=True but keeps the default stub invoke (raises NotImplementedError)
    monkeypatch.setattr(
        runner_mod, "all_targets", lambda: [Target("python", implemented=True)]
    )
    monkeypatch.setattr(runner_mod, "implemented_targets", lambda: [])
    report = run(root)
    assert report.count(Status.NOT_INVOCABLE) == 1
    assert report.exit_code == 1


def test_parity_activates_and_detects_break(tmp_path, monkeypatch):
    root = _one_fixture(tmp_path)
    from harness.targets import ObservedDiagnostic

    def a(fx):
        return Outcome(False, (ObservedDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", "$.seed", "m"),))

    def b(fx):
        return Outcome(True)  # diverges from a

    t_a = Target("python", implemented=True, invoke=a)
    t_b = Target("go", implemented=True, invoke=b)
    monkeypatch.setattr(runner_mod, "all_targets", lambda: [t_a, t_b])
    monkeypatch.setattr(runner_mod, "implemented_targets", lambda: [t_a, t_b])
    report = run(root)
    assert report.parity.active
    assert not report.parity.ok
    assert report.exit_code == 1


def test_parity_inactive_with_one_target(tmp_path, monkeypatch):
    root = _one_fixture(tmp_path)
    from harness.targets import ObservedDiagnostic

    def a(fx):
        return Outcome(False, (ObservedDiagnostic("STRICTSPEC_TYPE_NOT_INTEGER", "$.seed", "m"),))

    t_a = Target("python", implemented=True, invoke=a)
    monkeypatch.setattr(runner_mod, "all_targets", lambda: [t_a])
    monkeypatch.setattr(runner_mod, "implemented_targets", lambda: [t_a])
    report = run(root)
    assert not report.parity.active
