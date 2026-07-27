// Unit tests for the first-run CLI launcher (bin/strictspec.cjs).
//
// The launcher lazy-downloads the Go toolchain binary on first CLI invocation.
// These tests exercise version/URL/asset-name construction, checksum
// verification failure (a hard error), the cache-hit path (returns the cached
// binary with no download), and the decision-31 contract that package.json
// carries NO postinstall script -- all WITHOUT touching the network (the
// cache-hit path structurally returns before any download call).

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
// dist-test/launcher.test.js -> ../bin/strictspec.cjs at runtime.
// biome-ignore lint/suspicious/noExplicitAny: the .cjs stub is untyped by design.
const launcher = require("../bin/strictspec.cjs") as any;

const pkg = JSON.parse(
	readFileSync(join(here, "..", "package.json"), "utf8"),
) as { version: string; bin?: Record<string, string>; scripts?: Record<string, string> };

test("assetName has the goreleaser default shape", () => {
	const name: string = launcher.assetName("0.1.0");
	assert.match(
		name,
		/^strictspec_0\.1\.0_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$/,
	);
});

test("releaseBaseUrl targets the go-strictspec@ releasable tag", () => {
	// The critical strictspec adaptation: the Go binary lives on the
	// `go-strictspec@vX.Y.Z` releasable tag, not a bare `vX.Y.Z` tag.
	assert.equal(
		launcher.releaseBaseUrl("0.1.0"),
		"https://github.com/smm-h/strictspec/releases/download/go-strictspec@v0.1.0",
	);
});

test("expectedDigest matches by filename", () => {
	const checksums =
		"aaaa  strictspec_0.1.0_linux_amd64.tar.gz\n" +
		"bbbb  strictspec_0.1.0_darwin_arm64.tar.gz\n";
	assert.equal(
		launcher.expectedDigest(checksums, "strictspec_0.1.0_darwin_arm64.tar.gz"),
		"bbbb",
	);
	assert.equal(launcher.expectedDigest(checksums, "nope.tar.gz"), null);
});

test("verifyChecksum passes on a matching digest", () => {
	const data = Buffer.from("the-binary-bytes");
	const digest: string = launcher.sha256(data);
	const checksums = `${digest}  strictspec_0.1.0_linux_amd64.tar.gz\n`;
	assert.equal(
		launcher.verifyChecksum(data, checksums, "strictspec_0.1.0_linux_amd64.tar.gz"),
		true,
	);
});

test("verifyChecksum mismatch is a hard error", () => {
	const data = Buffer.from("the-binary-bytes");
	const checksums = "0000  strictspec_0.1.0_linux_amd64.tar.gz\n";
	assert.throws(
		() =>
			launcher.verifyChecksum(
				data,
				checksums,
				"strictspec_0.1.0_linux_amd64.tar.gz",
			),
		/Checksum mismatch/,
	);
});

test("verifyChecksum missing entry is a hard error", () => {
	assert.throws(
		() => launcher.verifyChecksum(Buffer.from("x"), "", "strictspec_0.1.0_linux_amd64.tar.gz"),
		/No checksum entry/,
	);
});

test("cache hit returns the cached binary without downloading", async () => {
	// Steer cacheDir() at a temp dir via XDG_CACHE_HOME (Linux CI). A pre-seeded
	// cached binary means ensureBinary returns before reaching the download.
	if (process.platform !== "linux") {
		return; // XDG steering only applies on Linux; skip elsewhere.
	}
	const prev = process.env.XDG_CACHE_HOME;
	const tmp = mkdtempSync(join(tmpdir(), "strictspec-cache-"));
	process.env.XDG_CACHE_HOME = tmp;
	try {
		const version: string = launcher.installedVersion();
		const cached: string = launcher.cachedBinaryPath(version);
		mkdirSync(dirname(cached), { recursive: true });
		writeFileSync(cached, "#!/bin/sh\n");
		const got: string = await launcher.ensureBinary();
		assert.equal(got, cached);
	} finally {
		if (prev === undefined) {
			delete process.env.XDG_CACHE_HOME;
		} else {
			process.env.XDG_CACHE_HOME = prev;
		}
	}
});

test("package.json has NO postinstall (decision 31: zero network at install)", () => {
	assert.equal(pkg.scripts?.postinstall, undefined);
	assert.equal(pkg.bin?.strictspec, "bin/strictspec.cjs");
});
