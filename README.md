# Home Library

A local Go + SQLite backend with static Astro pages, Lumos styling and Svelte islands.

## Run the first slice

Requirements for rebuilding: Go 1.27+, Node 22.12+ and npm. Dependencies are pinned in go.mod/go.sum and web/package-lock.json.

```sh
cd web
npm ci
npm run build
cd ..
go build -o bin/library ./cmd/library
./bin/library
```

Open http://127.0.0.1:8080. The executable serves generated Astro pages and invokes Node/Astro when metadata changes. Run from the repository root or pass `-web` and `-data` with absolute paths. Stop with Ctrl+C.

Build outputs and household data are excluded from the repository. Install Go normally before building.

## Try it

1. Open Shelves and add a bookcase and shelf. Rename locations as needed; removal is blocked while current or archived copies reference them.
2. Open Add a book. Focus the barcode field and scan with an Enter suffix, or type an ISBN and press Enter.
3. Review the fetched details, choose a shelf and save. If lookup fails, fill in the details manually.
4. Scan the same ISBN again: the existing book opens and its copy count stays unchanged.
5. On Book details, use Add another copy deliberately, or move a copy to another shelf.
6. Search by title or author from Library. Scanning or entering an ISBN there opens the existing record directly, or automatically loads the add form for a new book.

Books without ISBNs can be entered manually. Do not concatenate the price supplement onto the ISBN: configure the scanner to send only the ISBN/EAN barcode and an Enter suffix. Scanner hardware has not yet been tested.

## Architecture

- `cmd/library`: local Go server startup/shutdown and static regeneration.
- `internal/httpapi`: same-origin API and static serving.
- `internal/store`: SQLite catalogue, inventory and discovery metadata.
- `internal/bookpages`: metadata snapshot, serialized Astro build, atomic publication.
- `internal/covers`: locally cached cover downloads, selection and validated uploads.
- `internal/catalogue`: shared explained discovery service.
- `internal/mcpserver` and `cmd/library-mcp`: read-only local stdio MCP tools.
- `web/src/pages/books/[id].astro`: one static metadata/cover page per stable book ID.
- `web/src/components/library`: live Svelte search, inventory, lending, cover picker and editor controls.

Go serves the site and database API; Node/Astro now run on the device only to regenerate pages. Start `./bin/library` from the repository root with `web/node_modules` installed. `-web` points to Astro source (default `web`), not `web/dist`. Startup generates existing book pages; add/edit/cover changes regenerate automatically. A build uses a SQLite metadata snapshot and publishes only complete output. Pages live under `data/pages/current`, with the current generation, two predecessors and a one-hour grace period retained for in-flight reads. Fingerprinted island assets are kept in a shared derived cache so older pages can still hydrate after code updates. They are derived data and can be rebuilt. Build failure preserves the committed record and old pages, shows a warning, and retries when the book is opened.

Shelf/copy/loan changes are fetched by Svelte and require no build. Inspect View Source at `/books/1/`: the title/authors/publisher/date/cover are already HTML; inventory controls hydrate and fetch current status. Existing `/book/?id=1` links redirect.

Catalogue filtering and pagination run in SQLite; only up to 24 records are loaded per page. An SQLite FTS5 trigram index narrows title/author/ISBN searches of three or more characters. Literal Unicode-normalized substring verification preserves exact search behavior; short and unsupported queries use the same SQL scan fallback.

Book details can look up fresh Open Library metadata for the saved ISBN. Review it, use it in the form, then save explicitly. Refresh never changes inventory or cover selection. Successful lookup drafts persist in `data/metadata` for seven days; older cached drafts remain available if lookup fails, with a dated warning. This cache is derived provider data.

Catalogue cards and cover choices use locally generated thumbnails capped at 320 pixels. They preserve original cover files and regenerate when the derived cache is missing or damaged.

Theme search uses subjects, descriptions and your tags/notes, with literal-match reasons. It does not yet infer semantic similarity. See MCP_README.md for the separate read-only tool interface; it is not installed in a client automatically.

Current database relationships: [ER_DIAGRAM.md](ER_DIAGRAM.md), with an editable tldraw board and Mermaid source.

## Verification

For a temporary phone preview, `cmd/library-preview` can proxy the running app on a specific private Wi-Fi IP with a generated private link and a two-hour expiry. It permits browsing only; writes are rejected. The link holder can view the catalogue, borrower names and notes, so enable this only with the household owner's approval. The main service remains loopback-only.

```sh
go test -race ./...
go vet ./...
cd web
npm run check
npm run build
```

Tests cover canonical ISBNs, malformed codes, duplicate/replayed saves, concurrent Add copy retries, changed request payloads, stale edits, persistence, shelf joins, one-active-loan uniqueness, HTTP origin/JSON boundaries and metadata client caching/failure. Browser checks use an isolated data directory rather than adding sample books to the real catalogue.

## Data and migrations

`data/library.sqlite` is mutable household data and is ignored by Git. SQLite uses foreign keys, WAL, FULL synchronous mode and a single connection. The initial migration runs transactionally before readiness; newer schema versions are rejected.

Take a verified consistent backup before deploying schema changes; never copy only a live database file in WAL mode. The library-backup command creates a consistent live SQLite snapshot, portable JSON export and cover files with checksum and foreign-key verification. It restores only into a new directory. See BACKUP_README.md. Settings provides read-only backup health; daily timer templates are prepared but durable storage and activation remain NEA-40 acceptance work.

## Current scope and limitations

This is a development preview, not the Pi release. Real cover assets are cached locally; exact-edition alternatives and own-image uploads are available. Some editions lack artwork; upload a cover when lookup cannot find one. JPEG/PNG/WebP uploads are validated up to 8 MiB and 16 million pixels, corrected for camera orientation, stripped of metadata and reduced to a maximum dimension of 1600 pixels. Preview the upload before saving. Local 320-pixel thumbnails are implemented. The contribution panel reviews the exact edition and prepares a sanitized download for manual upload on Open Library. Upstream API contributions are not automatic.

Borrowers, per-copy checkout/return and history are implemented. Descriptions/subjects and personal tags/notes support explained theme discovery; optional Open Library enrichment fills empty fields while preserving local edits. Catalogue filtering/pagination and indexed search run in SQLite. Authors remain an ordered list on each edition. Shelf renaming/deletion use revision checks and block removal while copies refer to the shelf. Copy archival is explicit, blocks active loans and preserves history. Publication precision remains source text.

Network exposure: the listener is loopback-only with Host/Origin checks. Open Library lookup needs internet; manual entry, existing ISBN scans, local search and shelf/copy changes work without it. Successful lookup drafts persist in the local seven-day cache, with older drafts available on provider failure. Book details offers a reviewed metadata refresh; applying it requires an explicit save.

Lumos source: v0.0.5, commit d8591421cb2c6596fd80c9814cc31eb02e1f95ff. Source files were copied into web and retain the MIT license in web/LUMOS_LICENSE. Local theme/control adaptations use Lumos tokens and the four CSS layers.

The frontend is generated into data/pages rather than embedded. Source packaging and Go ARM64 cross-build checks pass; the Pi needs Go binaries, Astro source and installed frontend dependencies. Actual-device installation and runtime checks remain outstanding. Do not claim hardware support based on this macOS preview.

See [the plan](LIBRARY_PLAN.md) and [release acceptance checks](RELEASE_ACCEPTANCE.md).

Raspberry Pi installation templates and build instructions are in [PI_README.md](PI_README.md). They cross-compile for ARM64 but still require actual-device validation.

## Foundation acceptance — 10 October

Startup validates the supported core schema, keys and required constraints before applying additive migrations in one transaction. Future or incompatible databases are refused; failed upgrades roll back. Take a verified backup before deploying schema changes.

Manual entry preserves its draft when returning to the scanner and suggests local title matches without blocking save. A committed save with failed static generation shows a saved receipt and a retry link; the new-book form cannot accidentally create another copy. Reviewed source metadata persists on edits; changing an ISBN clears stale provider provenance. Physical-keyboard and scanner checks remain outstanding.

Source release packaging and its checks are documented in [RELEASE_README.md](RELEASE_README.md). The package excludes household data and installed dependencies; install frontend dependencies on the target Pi and transfer library data separately through verified backup/restore.

Use [RELEASE_ACCEPTANCE.md](RELEASE_ACCEPTANCE.md) for the scanner, display and Pi checklist. The disposable [offline acceptance runner](OFFLINE_ACCEPTANCE.md) checks local operation and backup recovery without using household data.

The Settings page checks saved backup folders when an optional `-backup-dir /absolute/durable/backups` is configured at startup. It reports verification and48hour freshness without scheduling or writing backups. See [BACKUP_README.md](BACKUP_README.md) for limits and setup. After changing application/ frontend source, restart the service to regenerate static pages; in-process regeneration is driven by metadata changes.
