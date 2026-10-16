# Source release package

This produces a source-only archive for building on a 64-bit Raspberry Pi. It does not deploy, install dependencies, upload anything, or include household data. Raspberry Pi runtime and hardware acceptance remain outstanding.

## Create and check a package

Packaging needs Python 3 and a POSIX shell on the development machine. Choose a fresh output filename **outside** the project directory:

```sh
./deploy/package-test.sh
./deploy/package-source.sh /private/tmp/home-library-source.tar.gz
```

The builder prints the archive's SHA-256 checksum and refuses to overwrite an existing file. Filenames containing spaces are supported. An optional second argument selects another source checkout:

```sh
./deploy/package-source.sh "/private/tmp/library release.tar.gz" "/path/to/library checkout"
```

The archive contains a single `home-library-source/` folder. It includes Go source/tests and manifests, Astro/Svelte source and assets, npm package/lock files, selected deployment scripts, documentation and the Lumos license. `SHA256SUMS` describes the archived source bytes; `release-manifest.json` lists the included files. File order, timestamps and archive ownership are normalized, so unchanged inputs produce the same archive bytes.

The explicit allowlist excludes household SQLite/covers/pages, installed dependencies, generated HTML, Mac binaries, caches, hidden files, `.git`, environment files, keys and unknown deployment files. Symbolic links in selected source are rejected. This controls packaged paths; review source and documentation before distributing them, since embedded credentials or private text inside an allowed source file are not detected automatically. Add future required file types or deployment files to the allowlist deliberately.

## Build on the Pi

Transfer the archive separately from household backups. Verify its printed SHA-256 against the transferred file, then extract into a fresh directory:

```sh
sha256sum home-library-source.tar.gz
mkdir library-release
tar -xzf home-library-source.tar.gz -C library-release
cd library-release/home-library-source
sha256sum -c SHA256SUMS
./deploy/build-pi.sh
```

Use Go 1.27 or newer, Node 22.12 or newer and npm 9.6.5 or newer on Raspberry Pi OS 64-bit. The build script installs exact frontend versions with `npm ci`; it may download npm packages and Go modules. The archive itself includes no installed dependencies and makes no network requests. Build online or arrange a separate verified dependency cache.

The build produces `bin/` and prebuilt `web/dist/`. Install those HTML/assets beside the Go application as `web/`. Go serves the prebuilt interface and live catalogue API; **Node and frontend source are build-time dependencies only**. See [PI_README.md](PI_README.md) for startup, service and kiosk instructions, and [BACKUP_README.md](BACKUP_README.md) to transfer household data through verified backup/restore.

## Verification scope

After building the binaries and installing frontend dependencies, run `./deploy/offline-acceptance.sh` for a disposable offline/recovery check. It blocks provider traffic in its own child process, creates fresh test data, verifies backup/restore and removes its fixtures. See [OFFLINE_ACCEPTANCE.md](OFFLINE_ACCEPTANCE.md) for evidence and limits, and [RELEASE_ACCEPTANCE.md](RELEASE_ACCEPTANCE.md) for remaining device checks. Both documents and the runner are included in the source package.

`package-test.sh` uses disposable folders. It checks required source, payload checksums, archive paths/types, exclusion of fixture data/secrets/build outputs, filenames with spaces, reproducibility, existing-output protection, source-directory output rejection, symbolic-link rejection and missing required files. It never opens a household database or installs a service. These checks validate packaging, not Raspberry Pi performance, desktop startup or physical scanner behavior.

Development checks passed: shell syntax and disposable packaging acceptance; an extracted archive also compiled all Go commands for Linux ARM64 with CGO disabled and Go module downloads disabled. This used the existing verified local module cache. No source package was installed, uploaded or run on a Pi, and no household database was opened. Frontend dependency installation and the complete `build-pi.sh` run still need checking on the target Pi.

## Signed prebuilt Raspberry Pi runtime

The Approved Pi release workflow builds on Linux ARM64 and publishes `bundle.tar.gz`, `manifest.json` and `manifest.sig`. New manifests declare `runtime: static-v1`. Bundles contain Go binaries, prebuilt `web/` files and licences, without Node, npm dependencies or frontend source. The four required HTML entry points are checked before packaging and by the updater. A fresh output directory is required:

```sh
sh deploy/prepare-release.sh v0.2.0 /absolute/fresh/release-output
```

Run this on Linux ARM64 (or supply the appropriate Go cross-compilation environment). Building requires the Go/Node/npm development dependencies; running the resulting bundle requires none of them.

### Verify before a fresh installation

Obtain the three assets from the intended release. Obtain the publisher public key through a separately trusted channel; a key downloaded beside an untrusted bundle does not establish publisher identity. From a reviewed checkout, build the verifier and use the signed-file extractor into a new directory:

```sh
go build -o bin/library-release ./cmd/library-release
python3 - /absolute/path/to/downloads /absolute/path/to/trusted-public.pem /absolute/path/to/new-runtime <<'PYTHON'
import pathlib, sys
sys.path.insert(0, 'deploy')
from updates_common import verify, extract
assets, key, destination = map(pathlib.Path, sys.argv[1:])
manifest = verify(assets/'manifest.json', assets/'manifest.sig', key, pathlib.Path('bin/library-release').resolve())
assert manifest.get('runtime') == 'static-v1', 'Expected a prebuilt runtime'
extract(assets/'bundle.tar.gz', manifest, destination)
print('Verified', manifest['version'], manifest['commit'])
PYTHON
```

The extractor checks the signature, architecture, file declarations, bundle checksum and every file checksum; it rejects unexpected archive entries. Check the printed version/commit against the release you intended to install. Follow [PI_README.md](PI_README.md) to install the verified `bin/` and `web/` directories and service configuration.

### Existing managed Pi: refresh updater helpers once

The old installed updater expects Node inside every bundle and will reject `static-v1`. Before installing the first prebuilt release, transfer/review this checkout on the Pi and run:

```sh
sudo python3 deploy/upgrade-runtime-updater.py
```

This is a separate administrator operation, not an application update. It validates helper syntax, temporarily stops the updater request API/path, takes the activation lock, refuses a queued update and replaces the fixed root-owned updater helpers. It restores previously running updater services and leaves the current application, signing key, TLS configuration and catalogue untouched. Finish any in-progress/queued update before running it. Use trusted reviewed source: this command intentionally installs privileged code.

Helper files are staged before replacement and restored if a normal operation fails. Replacement of the complete set is not atomic across power loss: if interrupted, rerun the command from the same reviewed checkout before requesting another application update.

Then use Settings → Application updates normally. Legacy manifests without a `runtime` field remain supported, including their Node/cache setup, so recovery to an existing legacy release remains possible. Do not remove that previous release or its caches during this transition. No helper refresh or runtime deployment has been performed on the household Pi as part of this code change.
