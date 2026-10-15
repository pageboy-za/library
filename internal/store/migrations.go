package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const supportedSchemaVersion = 1

// Core v1 is the original library schema. Additive feature tables leave its
// compatibility version unchanged; a newer core version is never downgraded.
func prepareDatabase(ctx context.Context, db *sql.DB) error {
	var version int
	if e := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return e
	}
	if version > supportedSchemaVersion {
		return errors.New("Database schema is newer than this application; use a matching version")
	}
	if version < 0 {
		return errors.New("Unsupported database schema version")
	}
	// Recognize an existing catalogue before any persistent pragma or schema write.
	if version == 0 {
		var count int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return errors.New("Unversioned database is not empty; refusing to initialize over existing tables")
		}
	} else if e := validateCore(ctx, db); e != nil {
		return e
	}
	for _, p := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL"} {
		if _, e := db.ExecContext(ctx, p); e != nil {
			return e
		}
	}
	var enabled int
	if e := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); e != nil {
		return e
	}
	if enabled != 1 {
		return errors.New("SQLite foreign key enforcement is unavailable")
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if version == 0 {
		if _, e = tx.ExecContext(ctx, schema); e != nil {
			return fmt.Errorf("Initialize core schema: %w", e)
		}
		if _, e = tx.ExecContext(ctx, "PRAGMA user_version=1"); e != nil {
			return e
		}
	}
	if e = validateCore(ctx, tx); e != nil {
		return e
	}
	if e = checkForeignKeys(ctx, tx); e != nil {
		return e
	}
	if e = ensureDiscoverySchema(ctx, tx); e != nil {
		return fmt.Errorf("Initialize discovery schema: %w", e)
	}
	if e = ensureBibliographySchema(ctx, tx); e != nil {
		return fmt.Errorf("Initialize bibliography schema: %w", e)
	}
	if e = ensureLocationSchema(ctx, tx); e != nil {
		return fmt.Errorf("Initialize location versions: %w", e)
	}
	var subtitleColumn int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('books') WHERE name='subtitle'").Scan(&subtitleColumn); e != nil {
		return e
	}
	if subtitleColumn == 0 {
		if _, e = tx.ExecContext(ctx, "ALTER TABLE books ADD COLUMN subtitle TEXT NOT NULL DEFAULT ''"); e != nil {
			return e
		}
	}
	if e = validateColumns(ctx, tx, map[string]string{"books": "subtitle"}); e != nil {
		return e
	}
	if e = ensureSearchSchema(ctx, tx); e != nil {
		return fmt.Errorf("Initialize derived catalogue search: %w", e)
	}
	if _, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS library_preferences (
		id INTEGER PRIMARY KEY CHECK(id=1),
		default_shelf_id INTEGER REFERENCES shelves(id) ON DELETE SET NULL,
		revision INTEGER NOT NULL DEFAULT 1
	); INSERT OR IGNORE INTO library_preferences(id) VALUES(1);
	CREATE TRIGGER IF NOT EXISTS preferences_shelf_removed BEFORE DELETE ON shelves
	BEGIN UPDATE library_preferences SET default_shelf_id=NULL,revision=revision+1 WHERE default_shelf_id=OLD.id; END;`); e != nil {
		return e
	}
	var deletionColumn int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('books') WHERE name='deleted_at'").Scan(&deletionColumn); e != nil {
		return e
	}
	if deletionColumn == 0 {
		if _, e = tx.ExecContext(ctx, "ALTER TABLE books ADD COLUMN deleted_at TEXT"); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS book_deleted_copies (
        book_id INTEGER NOT NULL REFERENCES books(id), copy_id INTEGER PRIMARY KEY REFERENCES copies(id), archived_at TEXT NOT NULL
    )`); e != nil {
		return e
	}
	if e = validateColumns(ctx, tx, map[string]string{"books": "deleted_at", "book_deleted_copies": "book_id,copy_id,archived_at"}); e != nil {
		return e
	}
	if e = validateFeatures(ctx, tx); e != nil {
		return e
	}
	if e = checkForeignKeys(ctx, tx); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	// WAL is a persistent database setting and cannot change inside a transaction.
	// Set it only after compatible schema/data have passed validation and upgrade.
	if _, e = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); e != nil {
		return e
	}
	return nil
}

type schemaReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func validateColumns(ctx context.Context, q schemaReader, tables map[string]string) error {
	for table, columns := range tables {
		rows, e := q.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" LIMIT 0")
		if e != nil {
			return fmt.Errorf("Incompatible library schema at %s: %w", table, e)
		}
		rows.Close()
	}
	return nil
}
func validateCore(ctx context.Context, q schemaReader) error {
	if e := validateColumns(ctx, q, map[string]string{
		"books":     "id,isbn,title,authors,publisher,publication_date,source_url,source_id,revision,selected_cover_asset_id,created_at",
		"bookcases": "id,name,room", "shelves": "id,bookcase_id,name", "copies": "id,book_id,shelf_id,label,revision,archived_at",
		"borrowers": "id,name,notes,archived_at", "loans": "id,copy_id,borrower_id,checked_out_at,due_date,returned_at",
		"cover_assets":        "id,book_id,local_path,source_kind,source_cover_id,source_edition_id,checksum",
		"cover_contributions": "id,book_id,asset_id,target_edition_id,state,remote_cover_id,last_error",
		"mutation_requests":   "key,payload_hash,result_id",
	}); e != nil {
		return e
	}
	if e := validateCoreKeys(ctx, q); e != nil {
		return e
	}
	return validateCoreIndexes(ctx, q)
}
func validateFeatures(ctx context.Context, q schemaReader) error {
	return validateColumns(ctx, q, map[string]string{"book_discovery": "book_id,description,subjects,tags,notes,revision", "location_versions": "kind,id,revision", "book_bibliography": "book_id,first_published_year,source_url,revision"})
}
func checkForeignKeys(ctx context.Context, q schemaReader) error {
	rows, e := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var row sql.NullInt64
		var constraint int
		if e = rows.Scan(&table, &row, &parent, &constraint); e != nil {
			return e
		}
		return fmt.Errorf("Library database has a foreign key violation in %s referencing %s; repair or restore a known-good backup", table, parent)
	}
	return rows.Err()
}

func validateCoreIndexes(ctx context.Context, q schemaReader) error {
	for _, query := range []string{
		`SELECT 1 FROM pragma_index_list('books') il WHERE il."unique"=1 AND il.partial=0 AND EXISTS(SELECT 1 FROM pragma_index_info(il.name) WHERE seqno=0 AND name='isbn') AND NOT EXISTS(SELECT 1 FROM pragma_index_info(il.name) WHERE seqno>0) LIMIT 1`,
		`SELECT 1 FROM pragma_index_list('shelves') il WHERE il."unique"=1 AND il.partial=0 AND EXISTS(SELECT 1 FROM pragma_index_info(il.name) WHERE seqno=0 AND name='bookcase_id') AND EXISTS(SELECT 1 FROM pragma_index_info(il.name) WHERE seqno=1 AND name='name') AND NOT EXISTS(SELECT 1 FROM pragma_index_info(il.name) WHERE seqno>1) LIMIT 1`,
	} {
		rows, e := q.QueryContext(ctx, query)
		if e != nil {
			return e
		}
		found := rows.Next()
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if !found {
			return errors.New("Incompatible library schema: missing required unique catalogue or shelf constraint")
		}
	}
	rows, e := q.QueryContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='index' AND name='one_active_loan'`)
	if e != nil {
		return e
	}
	defer rows.Close()
	if !rows.Next() {
		return errors.New("Incompatible library schema: missing active-loan uniqueness constraint")
	}
	var ddl string
	if e = rows.Scan(&ddl); e != nil {
		return e
	}
	compact := strings.Join(strings.Fields(strings.ToLower(ddl)), "")
	if compact != "createuniqueindexone_active_loanonloans(copy_id)wherereturned_atisnull" {
		return errors.New("Incompatible library schema: active-loan uniqueness constraint differs")
	}
	return rows.Err()
}

// Validate declared keys as well as data: foreign_key_check cannot detect a
// constraint that was removed from an otherwise similarly shaped table.
func validateCoreKeys(ctx context.Context, q schemaReader) error {
	for table, column := range map[string]string{"books": "id", "bookcases": "id", "shelves": "id", "copies": "id", "borrowers": "id", "loans": "id", "cover_assets": "id", "cover_contributions": "id", "mutation_requests": "key"} {
		rows, e := q.QueryContext(ctx, `SELECT name,pk FROM pragma_table_info(?) WHERE pk>0 ORDER BY pk`, table)
		if e != nil {
			return e
		}
		count := 0
		correct := false
		for rows.Next() {
			var name string
			var position int
			if e = rows.Scan(&name, &position); e != nil {
				rows.Close()
				return e
			}
			count++
			if name == column && position == 1 {
				correct = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if count != 1 || !correct {
			return fmt.Errorf("Incompatible library schema: %s requires primary key %s", table, column)
		}
	}
	type link struct{ from, parent, to string }
	required := map[string][]link{
		"books":               {{"selected_cover_asset_id", "cover_assets", "id"}},
		"shelves":             {{"bookcase_id", "bookcases", "id"}},
		"copies":              {{"book_id", "books", "id"}, {"shelf_id", "shelves", "id"}},
		"loans":               {{"copy_id", "copies", "id"}, {"borrower_id", "borrowers", "id"}},
		"cover_assets":        {{"book_id", "books", "id"}},
		"cover_contributions": {{"book_id", "books", "id"}, {"asset_id", "cover_assets", "id"}},
	}
	for table, wanted := range required {
		rows, e := q.QueryContext(ctx, `SELECT id,seq,"from","table","to" FROM pragma_foreign_key_list(?)`, table)
		if e != nil {
			return e
		}
		actual := map[int][]link{}
		for rows.Next() {
			var id, sequence int
			var item link
			if e = rows.Scan(&id, &sequence, &item.from, &item.parent, &item.to); e != nil {
				rows.Close()
				return e
			}
			actual[id] = append(actual[id], item)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, want := range wanted {
			found := false
			for _, group := range actual {
				if len(group) == 1 && strings.EqualFold(group[0].from, want.from) && strings.EqualFold(group[0].parent, want.parent) && strings.EqualFold(group[0].to, want.to) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("Incompatible library schema: %s.%s requires a foreign key to %s.%s", table, want.from, want.parent, want.to)
			}
		}
	}
	return nil
}
