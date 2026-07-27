#!/usr/bin/env node
// First-run CLI launcher for the `strictspec` npm bin (root DESIGN.md
// decision 31: the CLI ships INSIDE the runtime package -- no separate wrapper
// package, and NO postinstall script, so `npm install` performs ZERO network
// I/O and library-only installs never touch the network).
//
// On the FIRST CLI invocation this stub resolves the package's exact version,
// downloads the matching go-strictspec GitHub Release asset for the current
// platform, verifies its SHA-256 against the release's checksums.txt, caches it
// under a platform-appropriate cache directory, then execs it -- passing argv
// through. Subsequent invocations exec the cached binary directly (no network).
// Node stdlib only -- zero runtime dependencies.
//
// The binary is the goreleaser-built `strictspec` archive on the
// `go-strictspec@vX.Y.Z` GitHub Release (go/.goreleaser.yml). Runtime package
// version == strictspec release version, so lazy download and the exact
// version-pairing rule (decision 19) agree by construction.
//
// Adapted from rlsbl's npm/shim-firstrun.cjs.tpl; the sole strictspec change is
// that the binary lives on the prefixed monorepo-releasable tag
// `go-strictspec@vX.Y.Z` rather than a bare `vX.Y.Z` tag.
"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const crypto = require("crypto");
const { execFileSync, spawnSync } = require("child_process");

const GITHUB_REPO = "smm-h/strictspec";
const ASSET_PROJECT = "strictspec";
const BINARY_NAME = "strictspec";
// The go releasable's tag prefix on the shared monorepo GitHub Releases: the Go
// binary is published under `go-strictspec@vX.Y.Z`, not a bare `vX.Y.Z` tag.
const GO_RELEASABLE_TAG_PREFIX = "go-strictspec@v";

// process.platform/arch -> goreleaser (Os, Arch) naming.
const OS_MAP = { linux: "linux", darwin: "darwin", win32: "windows" };
const ARCH_MAP = { x64: "amd64", arm64: "arm64" };

function targetTriple() {
  const goos = OS_MAP[process.platform];
  const goarch = ARCH_MAP[process.arch];
  if (!goos || !goarch) {
    throw new Error(
      `Unsupported platform: ${process.platform}/${process.arch}. ` +
        `Supported: linux|darwin|windows x amd64|arm64.`
    );
  }
  const ext = goos === "windows" ? "zip" : "tar.gz";
  return { goos, goarch, ext };
}

function assetName(version) {
  const { goos, goarch, ext } = targetTriple();
  return `${ASSET_PROJECT}_${version}_${goos}_${goarch}.${ext}`;
}

function releaseBaseUrl(version) {
  // `@` is a valid path character; GitHub serves release assets under the
  // literal `go-strictspec@vX.Y.Z` tag.
  return `https://github.com/${GITHUB_REPO}/releases/download/${GO_RELEASABLE_TAG_PREFIX}${version}`;
}

// Platform-specific cache directory for the downloaded binary. Linux:
// $XDG_CACHE_HOME or ~/.cache; macOS: ~/Library/Caches; Windows: %LOCALAPPDATA%.
function cacheDir() {
  if (process.platform === "win32") {
    const base =
      process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local");
    return path.join(base, BINARY_NAME);
  }
  if (process.platform === "darwin") {
    return path.join(os.homedir(), "Library", "Caches", BINARY_NAME);
  }
  const base = process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache");
  return path.join(base, BINARY_NAME);
}

function cachedBinaryPath(version) {
  const exe = process.platform === "win32" ? ".exe" : "";
  return path.join(cacheDir(), version, ASSET_PROJECT + exe);
}

// Download a URL to a Buffer, following GitHub's redirect to the CDN.
function download(url, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "strictspec-launcher" } }, (res) => {
        const { statusCode, headers } = res;
        if (statusCode >= 300 && statusCode < 400 && headers.location) {
          res.resume();
          if (redirectsLeft <= 0) {
            reject(new Error(`Too many redirects for ${url}`));
            return;
          }
          resolve(download(headers.location, redirectsLeft - 1));
          return;
        }
        if (statusCode === 404) {
          res.resume();
          reject(new Error(`Asset not found (HTTP 404): ${url}`));
          return;
        }
        if (statusCode !== 200) {
          res.resume();
          reject(new Error(`Download failed (HTTP ${statusCode}): ${url}`));
          return;
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

// Parse a goreleaser checksums.txt ("<sha256>  <filename>" per line) and return
// the expected hex digest for `name`, or null if absent.
function expectedDigest(checksumsText, name) {
  for (const line of checksumsText.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const parts = trimmed.split(/\s+/);
    if (parts.length >= 2 && parts[parts.length - 1] === name) {
      return parts[0].toLowerCase();
    }
  }
  return null;
}

function sha256(buffer) {
  return crypto.createHash("sha256").update(buffer).digest("hex");
}

// Verify `buffer` against the checksums file. Throws on mismatch or a missing
// checksum line -- a hard failure, never a silent pass.
function verifyChecksum(buffer, checksumsText, name) {
  const expected = expectedDigest(checksumsText, name);
  if (!expected) {
    throw new Error(`No checksum entry for ${name} in checksums.txt`);
  }
  const actual = sha256(buffer);
  if (actual !== expected) {
    throw new Error(
      `Checksum mismatch for ${name}: expected ${expected}, got ${actual}`
    );
  }
  return true;
}

function extractBinary(archivePath, destDir) {
  fs.mkdirSync(destDir, { recursive: true });
  // System tar handles both .tar.gz and .zip. child_process is Node stdlib.
  execFileSync("tar", ["-xf", archivePath, "-C", destDir], {
    stdio: "inherit",
  });
}

function installedVersion() {
  const pkg = require(path.join(__dirname, "..", "package.json"));
  return pkg.version;
}

// Return the cached binary path, downloading + verifying if missing. The
// download+verify+extract happens ONLY on the first invocation for a given
// version; afterwards the cached binary is reused with no network I/O.
async function ensureBinary() {
  const version = installedVersion();
  const cached = cachedBinaryPath(version);
  if (fs.existsSync(cached)) {
    return cached;
  }

  const name = assetName(version);
  const base = releaseBaseUrl(version);
  process.stderr.write(
    `[${BINARY_NAME}] first run: downloading ${name} from ${GITHUB_REPO}...\n`
  );
  let assetBuf;
  let checksumsBuf;
  try {
    assetBuf = await download(`${base}/${name}`);
    checksumsBuf = await download(`${base}/checksums.txt`);
  } catch (err) {
    // Hard error with manual-install remediation -- never a silent fallback.
    throw new Error(
      `failed to download the toolchain binary for version ${version} (${name}): ` +
        `${err.message}. Manual install: download the matching asset from ${base} ` +
        `or \`go install github.com/${GITHUB_REPO}/go/cmd/strictspec@v${version}\`.`
    );
  }

  // Verify BEFORE writing anything into the cache.
  verifyChecksum(assetBuf, checksumsBuf.toString("utf8"), name);
  process.stderr.write(`[${BINARY_NAME}] checksum verified.\n`);

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "strictspec-launcher-"));
  const archivePath = path.join(tmpDir, name);
  fs.writeFileSync(archivePath, assetBuf);

  const destDir = path.dirname(cached);
  extractBinary(archivePath, destDir);
  if (!fs.existsSync(cached)) {
    throw new Error(
      `Extracted archive did not contain expected binary: ${cached}`
    );
  }
  if (process.platform !== "win32") {
    fs.chmodSync(cached, 0o755);
  }
  fs.rmSync(tmpDir, { recursive: true, force: true });
  return cached;
}

async function main() {
  const bin = await ensureBinary();
  const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
  if (result.error) {
    process.stderr.write(
      `[${BINARY_NAME}] failed to exec: ${result.error.message}\n`
    );
    process.exit(1);
  }
  process.exit(result.status === null ? 1 : result.status);
}

module.exports = {
  targetTriple,
  assetName,
  releaseBaseUrl,
  cacheDir,
  cachedBinaryPath,
  expectedDigest,
  sha256,
  verifyChecksum,
  download,
  extractBinary,
  installedVersion,
  ensureBinary,
  main,
};

if (require.main === module) {
  main().catch((err) => {
    process.stderr.write(`[${BINARY_NAME}] launch failed: ${err.message}\n`);
    process.exit(1);
  });
}
