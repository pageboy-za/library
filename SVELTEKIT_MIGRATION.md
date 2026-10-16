# SvelteKit frontend migration

The frontend now uses SvelteKit 3, Svelte 5, TypeScript and the static adapter. Go remains the application server and SQLite remains the authoritative database. Node is needed only for development and release builds.

## Runtime and URLs

Build with `npm ci && npm run check && npm run build` inside `web/`. The static adapter writes `web/dist/`, including the home, settings and scanning pages and a `book-view/index.html` fallback. Go serves that fallback for existing `/books/ID/` records; SvelteKit resolves the original URL and loads current records from the same-origin Go API. Legacy `/book/?id=ID` links still work. Unknown HTTP routes remain 404s.

The four-entry `static-v1` release contract is unchanged. The updater already installed for v0.8.0 can validate the new bundle without another helper upgrade. Releases still need the normal signed build, backup and candidate validation before activation.

SvelteKit owns navigation, layouts, document titles and the error page. Existing library components and styling are reused. URL changes remount query-driven pages; changing book IDs resets book state. Kiosk event handlers and resize observation clean up on navigation. Pending book saves and scanner restoration block navigation until confirmed. Go caches fingerprinted `/_app/immutable/` assets, while HTML and version metadata revalidate.

## Verification on 16 October 2026

- Frontend type checking: zero errors. The stricter Svelte checker reports 36 existing unused-style, CSS compatibility and keyboard-handler accessibility warnings in four components; these are not hidden.
- Production build and all four version markers passed.
- Go race tests and vet passed; Linux ARM64 compilation passed.
- Source package acceptance and signed updater tests passed.
- Local browser checks: catalogue filtering and sorting, repeated navigation, direct book page reload, document titles, edit/save, checkout/return, and phone-width selection/return. At 375 pixels, the page had no horizontal overflow and did not autofocus search. This is a desktop Chromium width check, not iPhone/Safari validation.
- The full offline acceptance runner passed locally and on the actual Raspberry Pi: manual entry, local cover upload, repeated ISBN lookup, explicit copies, shelves, checkout/return, metadata, private-note exclusion, verified backup/restore and restart. The child application had an empty executable search path, proving normal runtime needs no Node or build tools.

Pi verification used separate application files, a separate process/port and disposable data under the ordinary user account. The installed service and household catalogue were not replaced. Publishing and deployment of this migration have not been performed.
