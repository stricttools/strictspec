"""The options built-ins and rules (stricttools/docs/appendix-options.md).

The shape and rules cases live in go/strictspec/testdata/options/ and are
shared with the Go and TypeScript runtimes' tests, so every runtime asserts the
identical bound values, diagnostics, refusals, and classifications.
"""

import json
from pathlib import Path

import pytest

import strictspec as ss
from strictspec import _options

_CASES = Path(__file__).resolve().parents[2] / "go" / "strictspec" / "testdata" / "options"
_SHAPE = json.loads((_CASES / "shape-cases.json").read_text(encoding="utf-8"))
_RULES = json.loads((_CASES / "rules-cases.json").read_text(encoding="utf-8"))


def _diags(ds):
    return [{"code": d.code, "path": d.path, "message": d.message} for d in ds]


def _run_shape(c):
    got = {}
    data = c["input"].encode("utf-8")
    if c["schema"] == "entries":
        es, ds = ss.read_options_entries(c["file"], data)
        if es:
            got["entries"] = [
                {
                    "file": e.file,
                    "index": e.index,
                    "id": e.id,
                    "scope": e.scope,
                    "current": e.current,
                    "ideal": e.ideal,
                    "reason": e.reason,
                }
                for e in es
            ]
    elif c["schema"] == "registry":
        reg, ds = ss.read_options_registry(data)
        if reg is not None and reg.options:
            got["options"] = [
                {
                    "name": o.name,
                    "subject": o.subject,
                    "values": o.values,
                    "default": o.default,
                    "scope": o.scope,
                    "description": o.description,
                }
                for o in reg.options
            ]
    else:
        u, ds = ss.read_upstream(data)
        if u is not None:
            got["upstream"] = {"host": u.host, "owner": u.owner, "repo": u.repo, "branch": u.branch}
    if ds:
        got["diagnostics"] = _diags(ds)
    return got


@pytest.mark.parametrize("case", _SHAPE, ids=[c["name"] for c in _SHAPE])
def test_shape_case(case):
    got = _run_shape(case)
    # An expected diagnostic without a message compares code and path only
    # (parse-error detail comes from each runtime's own parser).
    for want, g in zip(case["expect"].get("diagnostics", []), got.get("diagnostics", [])):
        if "message" not in want:
            del g["message"]
    assert got == case["expect"]


@pytest.mark.parametrize(
    ("name", "program"),
    [
        ("options-entries", ss.options_entries_program),
        ("options-registry", ss.options_registry_program),
        ("upstream", ss.upstream_program),
    ],
)
def test_builtin_schema_accepts_only_format_version_1(name, program):
    res = program().validate(b"format_version = 7\n", "toml")
    assert [d.code for d in res.diagnostics] == ["STRICTSPEC_GATE_UNSUPPORTED"]
    assert f"schema {name} " in res.diagnostics[0].message
    assert program()._prog.format_version() == 1


def _write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


_ONE_ENTRY = 'format_version = 1\n[[entry]]\nid = "{}"\ncurrent = "off"\nideal = "on"\nreason = "r"\n'


def test_load_options_entries_missing_directory(tmp_path):
    assert ss.load_options_entries(tmp_path) == ss.OptionsEntriesLoad(entries=(), invalid=())


def test_load_options_entries_directory(tmp_path):
    d = tmp_path / ".strictmetadata" / "options"
    _write(d / "manifest.toml", 'owner = "strictspec"\n')
    _write(d / "release.toml", _ONE_ENTRY.format("rlsbl:b"))
    _write(d / "changelog.toml", _ONE_ENTRY.format("rlsbl:a"))
    _write(d / "broken.toml", "format_version = 1\n")
    _write(d / "notes.txt", "not a subject document")
    load = ss.load_options_entries(tmp_path)
    assert [(e.file, e.id) for e in load.entries] == [
        ("changelog.toml", "rlsbl:a"),
        ("release.toml", "rlsbl:b"),
    ]
    assert [f.file for f in load.invalid] == ["broken.toml"]
    assert load.invalid[0].diagnostics[0].code == "STRICTSPEC_TYPE_MISSING_REQUIRED"


def test_load_options_registry_file(tmp_path):
    path = tmp_path / "options.toml"
    _write(
        path,
        'format_version = 1\n[[option]]\nname = "a"\nsubject = "s"\nvalues = "on > off"\n'
        'default = "on"\nscope = "none"\ndescription = "d"\n',
    )
    reg, diags = ss.load_options_registry(path)
    assert diags == ()
    assert reg.options[0].values == "on > off"
    with pytest.raises(OSError):
        ss.load_options_registry(tmp_path / "absent.toml")


def test_load_upstream(tmp_path):
    assert ss.load_upstream(tmp_path) == (None, False, ())
    _write(
        tmp_path / ".strictmetadata" / "upstream" / "upstream.toml",
        'format_version = 1\nhost = "github.com"\nowner = "ncruces"\nrepo = "wasm2go"\nbranch = "main"\n',
    )
    assert ss.load_upstream(tmp_path) == (
        ss.Upstream(host="github.com", owner="ncruces", repo="wasm2go", branch="main"),
        True,
        (),
    )


# --- the rules beyond shape ---------------------------------------------------


def _refusals(rs):
    return [{"rule": r.rule, "file": r.file, "index": r.index, "value": r.value, "detail": r.detail} for r in rs]


def _levels(rk):
    if rk is None:
        return None
    out: list[list[str]] = []
    for v in rk.values:
        lv = rk.level[v]
        while len(out) <= lv:
            out.append([])
        out[lv].append(v)
    return out


@pytest.mark.parametrize("case", _RULES["ranking"], ids=[c["name"] for c in _RULES["ranking"]])
def test_ranking(case):
    rk, refusals = _options._parse_ranking(case["input"])
    assert _refusals(refusals) == case.get("refusals", [])
    assert _levels(rk) == case.get("levels")


def _registry(src: str) -> ss.OptionsRegistry:
    reg, diags = ss.read_options_registry(src.encode("utf-8"))
    assert diags == (), diags
    return reg


@pytest.mark.parametrize("case", _RULES["registry"], ids=[c["name"] for c in _RULES["registry"]])
def test_registry_rules(case):
    checked, refusals = _options._check_registry(_registry(case["registry"]))
    assert _refusals(refusals) == case["refusals"]
    assert (checked is None) == bool(case["refusals"])


_NS = _RULES["namespace"]


@pytest.mark.parametrize("case", _NS["cases"], ids=[c["name"] for c in _NS["cases"]])
def test_namespace_rules(case):
    reg, refusals = _options._check_registry(_registry(_NS["registry"]))
    assert refusals == []
    entries = []
    for name in sorted(case["files"]):
        es, diags = ss.read_options_entries(name, case["files"][name].encode("utf-8"))
        assert diags == (), diags
        entries.extend(es)
    accepted, refusals = _options._validate_namespace(_NS["tool"], reg, entries)
    assert _refusals(refusals) == case["refusals"]
    assert [{"file": e.file, "index": e.index, "class": c} for e, c in accepted] == case["classified"]
