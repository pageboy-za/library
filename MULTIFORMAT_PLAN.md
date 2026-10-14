# Proposal: ebooks, audiobooks and related editions

Status: design proposal, 10 October 2026. No database migration, UI change or new ticket has been applied.

## Current boundary

The existing `books` records represent editions with optional unique ISBNs; `copies` represent physical inventory and loans attach to copy IDs. Creation currently adds the first physical copy automatically. There is no grouping for related editions, format metadata or digital ownership/access record. SQLite, Go and Astro/Svelte remain suitable.

## Recommended model

Work → Editions → Holdings.

A work groups related published versions. An edition carries publisher/date/language, its own cover and publication identifiers. Content format is print, ebook or audiobook; distinguish physical binding/carrier from digital file format. Audiobooks may need narrator, duration and abridged status. A holding records a physical copy or digital access entitlement, with optional provider, safe access link, local file reference, access type and expiry. Never store provider passwords in catalogue records.

Example: The Code Book has a paperback edition with two physical copies, an ebook edition with a digital purchase, and an unabridged audiobook edition with a different narrator from another recording. This is one grouped work, several editions, and separately tracked holdings. A CD audiobook is physical inventory; downloaded audio is digital access. Several file copies or devices do not automatically imply several purchases. Revised/translated/abridged versions should be reviewed before grouping.

## Migration and identity

Back up with the existing verified backup command. Add work grouping and edition metadata while preserving current book/copy/loan IDs and `/books/{id}/` links. Convert existing copies to physical holdings; retain shelves, cover selections and loan history. Initially give each existing edition its own work; linking related editions is explicit or a reviewed provider suggestion, not an automatic title match. Do not silently assume the bindings of existing books.

Keep canonical ISBN deduplication at edition level. Add namespaced identifiers for provider-specific product IDs and optional editions without ISBNs. Review duplicate holdings by provider/product and acquisition identity: opening or importing an existing product must not increment inventory. A changed file path or a second device is not a new holding by itself. Provider mismatches and duplicate-identifier edits require review.

## API and UI

Add a visible format selector during manual entry, plus format-specific fields. Digital saves create a digital holding, not a physical Copy1. Physical saves retain shelf selection. Offer Link to an existing work and a grouped search result listing the formats owned. Count works, editions, physical copies and digital holdings separately; availability and lending refer to individual holdings.

Retain edition detail pages and their covers; optionally introduce a static work overview linking to editions. Static pages contain bibliographic metadata and format; Svelte islands load current physical locations, digital access, ownership/expiry and loans. Provider account/access links and personal notes remain dynamic. Grouped discovery/MCP can return one work with its owned formats, with narration/edition distinctions available when requested.

Digital lending should be disabled unless the holding supports it; physical media keep copy-level checkout. Catalogue access tracking must not imply that provider purchases can be transferred or played directly by this app.

## Delivery sequence

1. Backed-up additive model/migration; preserve IDs and duplicate-scan behavior.
2. Physical/ebook/audio metadata and holdings entry, edition grouping, format filters and correct counts.
3. Adapt covers, static generation, lending, discovery, MCP and export/restore to the model.
4. Optional separate project: file upload/storage, EPUB metadata, audio files, reading/playback and progress. Evaluate Pi storage and browser behavior before promising an embedded reader/player; external-provider holdings can use safe links meanwhile.

## Verification

Test multiple copies of one edition; multiple formats of one work; distinct narrations; ebooks without ISBNs; physical audio CDs; expired subscription access; repeated product import; the same file on multiple devices; existing loans/URLs after migration; and static rebuild failure recovery. Grouping and ungrouping must preserve edition IDs and holdings.

## Sources

- [International ISBN Agency: ISBN assignment](https://www.isbn-international.org/node/88): separately published formats/language editions have distinct ISBN assignments.
- [International ISBN Agency: ebook assignment guidelines](https://www.isbn-international.org/node/26): identifiers distinguish separately available products and usage constraints.
- [Open Library Books API](https://openlibrary.org/dev/docs/api/books): bibliographic identifier lookup; imported identity remains subject to review.

Terminology is recorded in CONTEXT.md. The proposal is independent of the currently running temporary network preview.
