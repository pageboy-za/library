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

Keep Astro source and the Pi's installed `web/node_modules/` beside the Go application. **Node is needed at runtime** to regenerate static book pages after metadata/cover changes; the binary alone is insufficient. See [PI_README.md](PI_README.md) for startup, service and kiosk instructions, and [BACKUP_README.md](BACKUP_README.md) to transfer household data through verified backup/restore.

## Verification scope

After building the binaries and installing frontend dependencies, run `./deploy/offline-acceptance.sh` for a disposable offline/recovery check. It blocks provider traffic in its own child process, creates fresh test data, verifies backup/restore and removes its fixtures. See [OFFLINE_ACCEPTANCE.md](OFFLINE_ACCEPTANCE.md) for evidence and limits, and [RELEASE_ACCEPTANCE.md](RELEASE_ACCEPTANCE.md) for remaining device checks. Both documents and the runner are included in the source package.

`package-test.sh` uses disposable folders. It checks required source, payload checksums, archive paths/types, exclusion of fixture data/secrets/build outputs, filenames with spaces, reproducibility, existing-output protection, source-directory output rejection, symbolic-link rejection and missing required files. It never opens a household database or installs a service. These checks validate packaging, not Raspberry Pi performance, desktop startup or physical scanner behavior.

Development checks passed: shell syntax and disposable packaging acceptance; an extracted archive also compiled all Go commands for Linux ARM64 with CGO disabled and Go module downloads disabled. This used the existing verified local module cache. No source package was installed, uploaded or run on a Pi, and no household database was opened. Frontend dependency installation and the complete `build-pi.sh` run still need checking on the target Pi.

The extracted frontend also passed `npm run check` (zero errors/warnings) and `npm run build` using a temporary link to the development Mac's already installed dependencies. A second build generated `/books/4242/` from a synthetic book snapshot and verified its title/author in the static HTML. All extracted files and generated pages were disposable. This checks that the package includes the frontend source/assets; it does not verify ARM64 dependency installation or Pi runtime.
