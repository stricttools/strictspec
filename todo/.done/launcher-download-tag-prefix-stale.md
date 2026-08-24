# Python launcher downloads from the retired per-package tag — first run 404s on 0.2.x

## Problem

The Python package's binary launcher (`python/src/strictspec/_launcher.py`)
builds its download URL from the OLD per-package release tag scheme: it
requests
`releases/download/go-strictspec@v<version>/strictspec_<version>_linux_amd64.tar.gz`.
Since the releasable-group migration, releases are tagged
`strictspec@v<version>` — the 0.2.x releases carry that tag and the assets
exist there under exactly the names the launcher computes (verified:
`strictspec@v0.2.1` holds `strictspec_0.2.1_linux_amd64.tar.gz` and
siblings plus `checksums.txt`). No `go-strictspec@v0.2.x` release exists,
so every first run of the 0.2.x wheel fails with HTTP 404 and the CLI is
unusable until the user manually seeds the cache or `go install`s the
binary.

Reproduced live: fresh install of the 0.2.1 wheel, first `strictspec
--help` → `HTTP Error 404: Not Found` for
`go-strictspec@v0.2.1/strictspec_0.2.1_linux_amd64.tar.gz`.

## Fix

Update the launcher's release-tag construction
(`GO_RELEASABLE_TAG_PREFIX` / `release_base_url`) to the releasable tag
scheme (`strictspec@v<version>`). Consider whether pre-migration versions
(0.1.0, tagged `go-strictspec@v0.1.0`) still need the old prefix — the
launcher always downloads the version matching the installed wheel, so a
plain prefix swap is correct for every release from the migration onward,
and old wheels keep their old launcher.

Add a test pinning the constructed URL against the tag scheme, and
consider a release-time check that the launcher's computed asset URL for
the version being released will actually exist (the release itself knows
the tag it is about to create).

## Workaround (for anyone hitting this before the fix ships)

`go install github.com/smm-h/strictspec/go/cmd/strictspec@v<version>` and
copy the binary to `~/.cache/strictspec/<version>/strictspec` — the
launcher uses the cache when present and never downloads.

## Affected files

- `python/src/strictspec/_launcher.py` (tag prefix / URL construction)
- its tests

## Effort

Small: one constant/function plus tests; ships with the next release.
