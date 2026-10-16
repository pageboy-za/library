# Home Library design

A warm, quiet library appliance. Covers provide the colour; controls stay calm.

## Shared system

- Genre: modern minimal with a literary voice. Kiosk home: light scanner, cover collection, then a focused lending/return screen after explicit selection. The scanner remains visible in both states.
- Keep the existing Lumos layers and tokens. App semantic tokens in `web/src/styles/app.css` map to that system: warm paper, white surfaces, forest accent, ink and muted text.
- Book-detail display and placeholder jackets: locally hosted Newsreader; catalogue titles, controls and body: locally hosted Inter with system fallback. No remote fonts or artwork.
- Four-point spacing, 44px minimum action targets, clear focus outlines, short hover feedback and reduced-motion support.
- Books are the main visual surface. Natural cover ratios on a common baseline; intentional typographic missing-cover treatment with accessible missing-cover text. No invented artwork, metrics or decorative hero content.
- Catalogue: scanning/search first, covers directly underneath, shelf/availability behind filters. Existing ISBN opens its record; copies are always explicit.
- Book detail: cover and static metadata, then immediately available shelf/lending actions. Less common editing tools stay behind labelled disclosures.
- Primary actions use forest fill; secondary controls use text/borders. Direct labels, no marketing footer. Kiosk navigation is Loans & returns and Manage. Manage contains Add book, Search & filters, Explore ideas, Shelves, Borrowers and Settings. Focused lending has All books and Loans & returns. Other pages retain their existing navigation.
- Keep single-copy availability available to assistive technology without repeating it under every cover; show visible wording for loans, multiple copies, partial availability and zero copies. Lending and return actions remain directly on each book; outstanding loans also offer direct Return book buttons.
- Narrow screens: compact labelled navigation, horizontally scrollable covers and stacked lending controls; the document itself must not overflow sideways.

## Review process

Each iteration has a screenshot and a fresh critic with no implementation or review history. On13October the user approved adding the kiosk brief; version2 in DESIGN_REVIEW.md is now fixed. Scores are recorded honestly. Screenshot review cannot certify interaction behavior; separately verify focus, filtering, routes, keyboard access and unchanged inventory.

The user requested scan → select → lend or return, then capped further design work at four more attempts. Those attempts scored 6, 6, 6 and 5/10, so design iteration is stopped. The implemented flow is available for assessment, not represented as an approved 9/10 design. Earlier `/design/` routes remain historical concepts. The filtered catalogue is retained at `/catalogue/`.

Scanning and selection stay read-only; copies remain explicit. Unique saved borrowers can be lent to with one clearly labelled action; a new name requires Lend book. Return book is an explicit action. Status, copy selection, circulation and book details use Svelte components within SvelteKit routes; records load from the Go API. The review record is in `design-review/opus-redesign/README.md`.
