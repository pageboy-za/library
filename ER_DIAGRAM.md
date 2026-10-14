# Home Library entity relationships

Current implemented SQLite model, checked against `internal/store/schema.sql`, `discovery.go` and `locations.go` on **10 October 2026**. Proposed ebook, audiobook and abstract-work models are deliberately excluded.

`ER_DIAGRAM.mmd` is the editable Mermaid source for the diagram below.

![Current SQLite entity relationships](ER_DIAGRAM.png)

## Read the model

A **book** is currently an edition record: one title/author/publisher/publication/ISBN combination. A **copy** is an individual physical item. For example, two copies of the same edition share one book ID, but can live on different shelves and have different loan histories. A book without an ISBN still receives its own stable ID.

One book can have many copies, cover assets and loan histories through its copies. A shelf belongs to exactly one bookcase; a copy has zero or one home shelf. The home shelf remains recorded while a copy is borrowed. Each loan records one copy and one borrower; returning it fills `returned_at` instead of deleting the row.

## Important constraints

- **Duplicate scans:** canonical ISBN uniqueness identifies the existing edition. Rescanning opens it; adding another physical copy is an explicit operation.
- **Copies and lending:** a partial unique index on `loans(copy_id)` where `returned_at IS NULL` permits only one active loan for each copy. Availability is derived from active loans and non-archived copies, rather than a separate stored status.
- **Locations:** `(bookcase_id, name)` is unique for shelves. Shelf/bookcase removal is blocked while any copy references the location, including archived copies. Location revision checks prevent stale updates.
- **Archives:** copy and borrower archive timestamps retain history. Historical loans remain linked to their original IDs.
- **Selected cover:** the nullable `books.selected_cover_asset_id` FK references an existing asset. Application code additionally verifies that the asset belongs to that same book. This ownership rule is **not** a composite SQL FK. The Mermaid `selected_FK` relationship shows the unconstrained SQL reference; application behavior selects at most one owned asset per book.
- **Discovery:** each book has at most one `book_discovery` row; its book ID is both primary key and foreign key, with cascade deletion. Subjects/tags and authors are JSON arrays stored in text columns; separate author/subject entities do not exist yet. Personal tags/notes stay out of static page snapshots.
- **Cover contributions:** the table exists as a reserved structure. The current workflow stores/chooses local covers; it does not write to Open Library or populate contribution records automatically.

## Supporting tables

`mutation_requests` protects retries: one request key and payload hash identify the recorded result. `result_id` has different meanings for different operations and has no foreign key or entity-type column. No relationship is drawn as though it always points to a book.

`location_versions` stores revisions under the composite key `(kind, id)` where kind is `shelf` or `bookcase`. These references are maintained by application transactions rather than SQL foreign keys. Deleted location IDs retain revision tombstones; they are not active shelf/bookcase records.

The metadata disk cache, immutable image files, generated static pages and external Open Library works are outside these SQLite entities. They are not database tables or owned copies.

## Editable Mermaid source

```mermaid
erDiagram
    books ||--o{ copies : has
    bookcases ||--o{ shelves : contains
    shelves o|--o{ copies : home_location
    copies ||--o{ loans : history
    borrowers ||--o{ loans : borrows
    books ||--o| book_discovery : describes
    books ||--o{ cover_assets : owns
    cover_assets o|--o{ books : selected_FK
    books ||--o{ cover_contributions : reserved_target
    cover_assets ||--o{ cover_contributions : reserved_upload

    books {
        INTEGER id PK
        TEXT isbn UK "nullable; canonical ISBN"
        TEXT title
        TEXT authors "JSON string array"
        TEXT publisher
        TEXT publication_date "source text"
        TEXT source_url
        TEXT source_id "external edition ID"
        INTEGER revision
        INTEGER selected_cover_asset_id FK "nullable; app validates same book"
        TEXT created_at
    }
    copies {
        INTEGER id PK
        INTEGER book_id FK
        INTEGER shelf_id FK "nullable"
        TEXT label
        INTEGER revision
        TEXT archived_at "nullable"
    }
    bookcases {
        INTEGER id PK
        TEXT name
        TEXT room
    }
    shelves {
        INTEGER id PK
        INTEGER bookcase_id FK
        TEXT name "unique within bookcase"
    }
    borrowers {
        INTEGER id PK
        TEXT name
        TEXT notes
        TEXT archived_at "nullable"
    }
    loans {
        INTEGER id PK
        INTEGER copy_id FK
        INTEGER borrower_id FK
        TEXT checked_out_at
        TEXT due_date "nullable"
        TEXT returned_at "nullable; active when null"
    }
    book_discovery {
        INTEGER book_id PK,FK
        TEXT description
        TEXT subjects "JSON string array"
        TEXT tags "JSON string array; private"
        TEXT notes "private"
        INTEGER revision
    }
    cover_assets {
        INTEGER id PK
        INTEGER book_id FK
        TEXT local_path
        TEXT source_kind
        INTEGER source_cover_id "nullable"
        TEXT source_edition_id "nullable"
        TEXT checksum "unique with book_id"
    }
    cover_contributions {
        INTEGER id PK
        INTEGER book_id FK
        INTEGER asset_id FK
        TEXT target_edition_id
        TEXT state
        INTEGER remote_cover_id "nullable"
        TEXT last_error "nullable"
    }
    mutation_requests {
        TEXT key PK
        TEXT payload_hash
        INTEGER result_id "operation-specific; no FK"
    }
    location_versions {
        TEXT kind PK "shelf or bookcase"
        INTEGER id PK "application-managed reference"
        INTEGER revision "greater than zero"
    }
```

The drawing shows 11 domain/support entities. The derived `book_search` FTS5 virtual table and its SQLite shadow tables are excluded: they contain rebuildable search documents keyed by `books.id`, maintained by insert/update/delete triggers, rather than separate owned-book records.
