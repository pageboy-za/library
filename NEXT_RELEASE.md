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
