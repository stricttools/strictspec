"""First-run CLI launcher for the ``strictspec`` console script.

Decision 31 (root DESIGN.md): the CLI ships INSIDE the runtime package -- there
is no separate wrapper package. Importing ``strictspec`` as a library performs
ZERO network I/O; only invoking the ``strictspec`` console script triggers this
module. pip has no post-install hook, so on the FIRST CLI invocation this module
downloads the exact-version Go toolchain binary from its GitHub Release, verifies
its SHA-256 against the release's ``checksums.txt``, caches it under a
platform-specific cache directory, then ``os.exec``s it -- passing argv through.
Subsequent invocations reuse the cached binary (no network). Python stdlib only
-- zero runtime dependencies.

The binary is the goreleaser-built ``strictspec`` archive on the
``go-strictspec@vX.Y.Z`` GitHub Release (go/.goreleaser.yml). The launcher
resolves ITS OWN installed version and downloads the matching go-strictspec
Release asset: runtime package version == strictspec release version, so the
lazy download and the exact version-pairing rule (decision 19) agree by
construction. A failed download is a hard error with manual-install remediation,
never a silent fallback (decision 31).

This is adapted from rlsbl's ``pypi/shim-launcher.py.tpl``; the sole strictspec
difference is that the binary lives on the prefixed monorepo-releasable tag
``go-strictspec@vX.Y.Z`` rather than a bare ``vX.Y.Z`` tag.
"""

import hashlib
import os
import platform
import sys
import tarfile
import tempfile
import urllib.request
import zipfile
from pathlib import Path

# The monorepo repo that hosts the GitHub Releases.
GITHUB_REPO = "smm-h/strictspec"
# goreleaser project_name -> the archive asset name AND the binary name inside
# the archive (strictspec_<ver>_<os>_<arch>.<ext> containing `strictspec`).
ASSET_PROJECT = "strictspec"
# Cache-directory / diagnostic name.
BINARY_NAME = "strictspec"
# The installed distribution whose version keys the download.
DIST_NAME = "strictspec"
# The go releasable's tag prefix on the shared monorepo GitHub Releases. The Go
# binary is published under `go-strictspec@vX.Y.Z`, not a bare `vX.Y.Z` tag.
GO_RELEASABLE_TAG_PREFIX = "go-strictspec@v"


def _installed_version():
    """Resolve this package's version to derive the release tag."""
    from importlib.metadata import PackageNotFoundError, version

    try:
        return version(DIST_NAME)
    except PackageNotFoundError:
        raise RuntimeError(
            f"Cannot determine installed version of {DIST_NAME!r}; "
            "is the package installed?"
        )


def target_triple():
    """Map the current interpreter's platform to goreleaser (os, arch, ext)."""
    sysname = platform.system().lower()
    os_map = {"linux": "linux", "darwin": "darwin", "windows": "windows"}
    goos = os_map.get(sysname)

    machine = platform.machine().lower()
    arch_map = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }
    goarch = arch_map.get(machine)

    if goos is None or goarch is None:
        raise RuntimeError(
            f"Unsupported platform: {sysname}/{machine}. "
            "Supported: linux|darwin|windows x amd64|arm64."
        )
    ext = "zip" if goos == "windows" else "tar.gz"
    return goos, goarch, ext


def asset_name(version):
    goos, goarch, ext = target_triple()
    return f"{ASSET_PROJECT}_{version}_{goos}_{goarch}.{ext}"


def release_base_url(version):
    # The Go binary is on the go-strictspec releasable's GitHub Release, whose
    # tag is `go-strictspec@vX.Y.Z` (NOT a bare `vX.Y.Z` tag). `@` is a valid
    # path character and GitHub serves release assets under it literally.
    return f"https://github.com/{GITHUB_REPO}/releases/download/{GO_RELEASABLE_TAG_PREFIX}{version}"


def cache_dir():
    """Platform-specific cache directory for the downloaded binary.

    Linux: ``$XDG_CACHE_HOME`` or ``~/.cache``; macOS: ``~/Library/Caches``;
    Windows: ``%LOCALAPPDATA%``. Hand-rolled -- zero runtime dependencies.
    """
    sysname = platform.system().lower()
    if sysname == "windows":
        base = os.environ.get("LOCALAPPDATA") or str(Path.home() / "AppData" / "Local")
        return Path(base) / BINARY_NAME
    if sysname == "darwin":
        return Path.home() / "Library" / "Caches" / BINARY_NAME
    base = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(base) / BINARY_NAME


def _download(url):
    req = urllib.request.Request(url, headers={"User-Agent": "strictspec-launcher"})
    with urllib.request.urlopen(req) as resp:  # noqa: S310 (https GitHub URL)
        return resp.read()


def expected_digest(checksums_text, name):
    """Return the hex sha256 for ``name`` from a goreleaser checksums.txt."""
    for line in checksums_text.splitlines():
        trimmed = line.strip()
        if not trimmed:
            continue
        parts = trimmed.split()
        if len(parts) >= 2 and parts[-1] == name:
            return parts[0].lower()
    return None


def verify_checksum(data, checksums_text, name):
    """Verify ``data`` against checksums.txt. Hard-fail on mismatch/absence."""
    expected = expected_digest(checksums_text, name)
    if expected is None:
        raise RuntimeError(f"No checksum entry for {name} in checksums.txt")
    actual = hashlib.sha256(data).hexdigest()
    if actual != expected:
        raise RuntimeError(
            f"Checksum mismatch for {name}: expected {expected}, got {actual}"
        )
    return True


def _extract_binary(archive_path, dest_dir, ext):
    dest_dir.mkdir(parents=True, exist_ok=True)
    exe = ".exe" if platform.system().lower() == "windows" else ""
    # goreleaser names the binary inside the archive after the project name.
    binary_path = dest_dir / (ASSET_PROJECT + exe)
    if ext == "zip":
        with zipfile.ZipFile(archive_path) as zf:
            zf.extractall(dest_dir)
    else:
        with tarfile.open(archive_path, "r:gz") as tf:
            tf.extractall(dest_dir)
    if not binary_path.exists():
        raise RuntimeError(
            f"Extracted archive did not contain expected binary: {binary_path}"
        )
    if not exe:
        binary_path.chmod(0o755)
    return binary_path


def ensure_binary():
    """Return the cached binary path, downloading + verifying if missing."""
    version = _installed_version()
    exe = ".exe" if platform.system().lower() == "windows" else ""
    cached = cache_dir() / f"{version}" / (ASSET_PROJECT + exe)
    if cached.exists():
        return cached

    name = asset_name(version)
    base = release_base_url(version)
    sys.stderr.write(
        f"[{BINARY_NAME}] first run: downloading {name} from {GITHUB_REPO}...\n"
    )
    try:
        asset = _download(f"{base}/{name}")
        checksums = _download(f"{base}/checksums.txt").decode("utf-8")
    except Exception as e:  # noqa: BLE001 -- re-raised as a hard error, never a fallback
        raise RuntimeError(
            f"[{BINARY_NAME}] failed to download the toolchain binary for version "
            f"{version} ({name}): {e}. Manual install: download the matching asset "
            f"from {base} or `go install {GITHUB_REPO.replace('smm-h/', 'github.com/smm-h/')}/go/cmd/strictspec@v{version}`."
        )

    # Verify BEFORE writing the binary into the cache.
    verify_checksum(asset, checksums, name)
    sys.stderr.write(f"[{BINARY_NAME}] checksum verified.\n")

    _, _, ext = target_triple()
    with tempfile.TemporaryDirectory() as tmp:
        archive_path = Path(tmp) / name
        archive_path.write_bytes(asset)
        binary_path = _extract_binary(archive_path, cached.parent, ext)
    return binary_path


def main():
    binary = ensure_binary()
    args = [str(binary), *sys.argv[1:]]
    if platform.system().lower() == "windows":
        import subprocess

        sys.exit(subprocess.run(args).returncode)
    os.execv(str(binary), args)


if __name__ == "__main__":
    main()
