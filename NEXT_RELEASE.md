# v0.6.0 — approved metadata release

Release, Pi installation and fill-only subject/first-publication-year backfill requested by Nealle on 15 October 2026.

## Included changes


- Update options: offer newer releases and only the previous successfully installed version for rollback; hide older release install buttons. Uses the installer's recorded previous version rather than guessing from GitHub release order. After rolling back, no further downgrade is offered.
- Validation: Astro check/build passed; version-policy checks covered numeric ordering, unknown/failed status and chained downgrades. A local browser preview offered v0.6.0, installed v0.5.0 and rollback to v0.4.1, with no controls for v0.4.0 or v0.3.0.

- Metadata browsing: searchable subject filters with book counts; title, author, edition-date and first-publication-year sorting. Subject labels can overlap and do not change physical shelves.
- First publication year: store and display the earliest year Open Library indexes for the linked work separately from the owned edition date, with a source link and manual correction/clearing. Explicit enrichment fills missing data and preserves existing records; revision checks reject concurrent overwrites.
- Validation: full Go race suite and vet passed; Astro check/build passed. Backend checks cover additive migration, backup/restore, v0.5.0 compatibility, exact linked-work lookup, bounded/rate-limited requests, manual correction, provenance, missing values, subject facets, sorting and concurrent edits. Integrated tablet browser checks confirmed subject counts/filtering, year ordering, separate edition/first-year display, and a saved manual correction that removed imported provenance while retaining the edition date and copy count. A browser-found input-pattern issue was corrected and reverified. Disposable records only.

- Controlled metadata migration: private plans use cached exact-edition and explicitly linked-work data, fill only empty subjects and unset first years, protect manual corrections/clears, skip stale/deleted records, and preserve core details, inventory, notes, tags, descriptions, shelves, borrowers and loans. The script verifies these boundaries after application and makes no provider requests.
- Release checks: full Go race suite, vet, Astro check/build, complete-history secret scan and five migration-script tests passed. The signed release workflow repeats source tests and scans before publication.

## Previous release

# v0.5.0 — approved release bundle

Release and Pi deployment requested by Nealle on 15 October 2026.

## Included changes

- Copy ISBN: a button beside the ISBN on book details copies the number without changing the record. It confirms success and offers manual copying if clipboard access is unavailable. Books without an ISBN show no copy button.

Validation: Astro check passed; local browser checks confirmed mouse and keyboard copying, exact pasted ISBN, success feedback, and no copy action for books without an ISBN. Tested with disposable records only.

- Cover picker: finding edition covers keeps the picker open and preserves the saved cover. Covers are saved only when the user chooses “Use this cover”; automatic covers for newly added books keep their existing behavior.

Validation: cover regression tests cover multiple alternatives, an existing selection, no selection, ISBN fallback, repeated discovery without book changes or page rebuilding, and explicit selection. Cover, API, store and page-generation tests and Astro check passed. Local browser verification used a disposable book and simulated cover images.

- Faster book saves: acknowledge the durable database save immediately, then fetch missing covers and rebuild derived pages in a coalescing background worker. Live book and kiosk views read current records directly, so new/edited records open without waiting for a build. Background work yields to updater maintenance and stops on shutdown; startup reconstructs interrupted work.
- Covers throughout book records: cover thumbnails or accessible missing-artwork placeholders in Loans & returns, deleted books, discovery, similar-book matches and deleted scan matches. Loan rows follow the currently selected cover without extra record lookups.
- Clicked-book details: full title, subtitle, authors, publisher, publication date and ISBN (including Copy) now match the full details view. Lending controls remain available.

Validation: affected Go race tests and Astro check passed. Tests cover blocked/failed builds, delayed cover lookup, safe retries without extra copies, rapid-save coalescing, updater maintenance and shutdown. Isolated offline save/cover/lending/backup/restore acceptance passed. Local Mac measurements for three disposable new books: save 1–17 ms, page shell 0–2 ms; these are not Pi performance results. Browser checks confirmed edited metadata immediately, current catalogue data, tablet book-detail parity and Loans cover/fallback rendering. Pi timing and physical iPad/Bluetooth scanner acceptance remain for the requested release.

## Release checklist

- Review the bundled changes and confirm the release scope.
- Complete relevant checks and update these notes with the results.
- Publish the signed release and deploy through Settings; verify catalogue preservation and Pi readiness.

## Pi deployment verified — 15 October 2026

- Signed v0.5.0 installed successfully through Settings; commit `2595ccc4de963a1bbe6aa417b69b75fcb9791728`.
- The installer created and verified its backup. A before/after API fingerprint matched for persisted catalogue, copy, shelf, borrower, lending, preference, deletion and discovery fields (excluding newly added derived loan cover URLs).
- An isolated Pi instance of the installed release saved three disposable books in 1.068, 0.503 and 0.383 seconds; their live page shells opened in 0.035, 0.010 and 0.007 seconds. Background page generation also completed successfully. No test records were added to the household catalogue.
