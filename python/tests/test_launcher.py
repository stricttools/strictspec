"""Unit tests for the first-run CLI launcher (strictspec._launcher).

The launcher lazy-downloads the Go toolchain binary on first CLI invocation.
These tests exercise version/URL/asset-name construction, checksum-verification
failure (a hard error), and the cache-hit path (execs without any download) --
all WITHOUT touching the network: `_download` is monkeypatched to explode if it
is ever called on the cache-hit path.
"""

import hashlib

import pytest

from strictspec import _launcher


@pytest.fixture
def linux_amd64(monkeypatch):
    monkeypatch.setattr(_launcher.platform, "system", lambda: "Linux")
    monkeypatch.setattr(_launcher.platform, "machine", lambda: "x86_64")


@pytest.fixture
def windows_arm64(monkeypatch):
    monkeypatch.setattr(_launcher.platform, "system", lambda: "Windows")
    monkeypatch.setattr(_launcher.platform, "machine", lambda: "ARM64")


def test_target_triple_linux(linux_amd64):
    assert _launcher.target_triple() == ("linux", "amd64", "tar.gz")


def test_target_triple_windows_zip(windows_arm64):
    assert _launcher.target_triple() == ("windows", "arm64", "zip")


def test_target_triple_unsupported(monkeypatch):
    monkeypatch.setattr(_launcher.platform, "system", lambda: "Plan9")
    monkeypatch.setattr(_launcher.platform, "machine", lambda: "sparc")
    with pytest.raises(RuntimeError, match="Unsupported platform"):
        _launcher.target_triple()


def test_asset_name(linux_amd64):
    assert _launcher.asset_name("0.1.0") == "strictspec_0.1.0_linux_amd64.tar.gz"


def test_asset_name_windows(windows_arm64):
    assert _launcher.asset_name("2.3.4") == "strictspec_2.3.4_windows_arm64.zip"


def test_release_base_url_uses_releasable_group_tag():
    # The critical strictspec adaptation: the Go binary lives on the releasable
    # group's `strictspec@vX.Y.Z` tag, not a bare `vX.Y.Z` tag. The retired
    # per-package `go-strictspec@vX.Y.Z` spelling has no release behind it from
    # the releasable-group migration onward, so a launcher still asking for it
    # 404s on its very first run.
    url = _launcher.release_base_url("0.2.1")
    assert url == (
        "https://github.com/stricttools/strictspec/releases/download/strictspec@v0.2.1"
    )


def test_release_tag_prefix_is_not_the_retired_per_package_one():
    assert _launcher.RELEASABLE_TAG_PREFIX == "strictspec@v"
    assert "go-strictspec@" not in _launcher.release_base_url("9.9.9")


def test_download_failure_remediation_points_at_the_live_release(
    linux_amd64, tmp_path, monkeypatch
):
    # The manual-install instructions in the hard error must name the SAME
    # (live) release URL the download used -- a remediation pointing at a
    # retired tag is a second 404 handed to the user.
    version = "0.2.1"
    monkeypatch.setattr(_launcher, "_installed_version", lambda: version)
    monkeypatch.setattr(_launcher, "cache_dir", lambda: tmp_path)

    def _fail(url):
        raise OSError("HTTP Error 404: Not Found")

    monkeypatch.setattr(_launcher, "_download", _fail)

    with pytest.raises(RuntimeError) as excinfo:
        _launcher.ensure_binary()
    message = str(excinfo.value)
    assert "releases/download/strictspec@v0.2.1" in message
    assert "go-strictspec@" not in message


def test_download_failure_remediation_names_a_resolvable_go_module_path(
    linux_amd64, tmp_path, monkeypatch
):
    # The `go install` line the hard error prints is a command a reader runs, so
    # it must spell the module path the repository actually serves: the host,
    # then the owner and the repository the launcher already knows.
    version = "0.2.1"
    monkeypatch.setattr(_launcher, "_installed_version", lambda: version)
    monkeypatch.setattr(_launcher, "cache_dir", lambda: tmp_path)

    def _fail(url):
        raise OSError("HTTP Error 404: Not Found")

    monkeypatch.setattr(_launcher, "_download", _fail)

    with pytest.raises(RuntimeError) as excinfo:
        _launcher.ensure_binary()
    message = str(excinfo.value)
    assert (
        f"go install github.com/{_launcher.GITHUB_REPO}/go/cmd/strictspec@v{version}"
        in message
    )


def test_expected_digest_matches_filename():
    checksums = (
        "aaaa  strictspec_0.1.0_linux_amd64.tar.gz\n"
        "bbbb  strictspec_0.1.0_darwin_arm64.tar.gz\n"
    )
    assert (
        _launcher.expected_digest(checksums, "strictspec_0.1.0_darwin_arm64.tar.gz")
        == "bbbb"
    )
    assert _launcher.expected_digest(checksums, "nope.tar.gz") is None


def test_verify_checksum_ok():
    data = b"the-binary-bytes"
    digest = hashlib.sha256(data).hexdigest()
    checksums = f"{digest}  strictspec_0.1.0_linux_amd64.tar.gz\n"
    assert _launcher.verify_checksum(
        data, checksums, "strictspec_0.1.0_linux_amd64.tar.gz"
    )


def test_verify_checksum_mismatch_is_hard_error():
    data = b"the-binary-bytes"
    checksums = "0000  strictspec_0.1.0_linux_amd64.tar.gz\n"
    with pytest.raises(RuntimeError, match="Checksum mismatch"):
        _launcher.verify_checksum(
            data, checksums, "strictspec_0.1.0_linux_amd64.tar.gz"
        )


def test_verify_checksum_missing_entry_is_hard_error():
    with pytest.raises(RuntimeError, match="No checksum entry"):
        _launcher.verify_checksum(b"x", "", "strictspec_0.1.0_linux_amd64.tar.gz")


def test_cache_hit_execs_without_download(tmp_path, linux_amd64, monkeypatch):
    version = "0.1.0"
    monkeypatch.setattr(_launcher, "_installed_version", lambda: version)
    monkeypatch.setattr(_launcher, "cache_dir", lambda: tmp_path)

    # Any network access on the cache-hit path is a test failure.
    def _boom(url):
        raise AssertionError(f"network access attempted on cache-hit path: {url}")

    monkeypatch.setattr(_launcher, "_download", _boom)

    cached = tmp_path / version / "strictspec"
    cached.parent.mkdir(parents=True)
    cached.write_bytes(b"#!/bin/sh\n")
    cached.chmod(0o755)

    assert _launcher.ensure_binary() == cached
