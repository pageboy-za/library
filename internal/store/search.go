package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"modernc.org/sqlite"
)

const cataloguePageSize = 24

var publicationYearPattern = regexp.MustCompile(`(?:^|[^0-9])([0-9]{4})(?:$|[^0-9])`)

func publicationSortYear(value string) any {
	m := publicationYearPattern.FindStringSubmatch(value)
	if len(m) != 2 {
		return nil
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 {
		return nil
	}
	return n
}
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("library_publication_year", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if v, ok := args[0].(string); ok {
			year := publicationSortYear(v)
			if year != nil {
				return int64(year.(int)), nil
			}
		}
		return nil, nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("library_label_fold", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		switch v := args[0].(type) {
		case string:
			return fold(strings.TrimSpace(v)), nil
		case []byte:
			return fold(strings.TrimSpace(string(v))), nil
		default:
			return "", nil
		}
	})
	sqlite.MustRegisterDeterministicScalarFunction("library_search_text", 3, searchDocument)
	// Scalar arguments are passed as BLOB because modernc TEXT conversion stops
	// at NUL. FTS tokenization also stops at NUL. Replacing it only in candidate documents
	// keeps text after it searchable; instr() still verifies the original text.
	sqlite.MustRegisterDeterministicScalarFunction("library_search_index", 3, func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		v, e := searchDocument(ctx, args)
		if e != nil {
			return nil, e
		}
		return strings.ReplaceAll(v.(string), "\x00", " "), nil
	})
}
func searchDocument(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	asText := func(v driver.Value) (string, error) {
		if v == nil {
			return "", nil
		}
		switch v := v.(type) {
		case string:
			return v, nil
		case []byte:
			return string(v), nil
		default:
			return "", errors.New("catalogue search text must be a string")
		}
	}
	title, e := asText(args[0])
	if e != nil {
		return nil, e
	}
	rawAuthors, e := asText(args[1])
	if e != nil {
		return nil, e
	}
	isbn, e := asText(args[2])
	if e != nil {
		return nil, e
	}
	var authors []string
	if e = json.Unmarshal([]byte(rawAuthors), &authors); e != nil {
		return nil, e
	}
	return fold(title + " " + strings.Join(authors, " ") + " " + isbn), nil
}

// searchIDs returns total and page IDs from one SQLite read snapshot. Book()
// hydration happens only after rows and the transaction close (one connection).
func (s *Store) searchIDs(ctx context.Context, q string, shelf int64, availability string, page int) ([]int64, int, error) {
	return s.searchIDsOptions(ctx, q, shelf, availability, page, ListOptions{})
}

type ListOptions struct {
	Subject string
	Sort    string
}

func (s *Store) searchIDsOptions(ctx context.Context, q string, shelf int64, availability string, page int, options ListOptions) ([]int64, int, error) {
	order, err := catalogueOrder(options.Sort)
	if err != nil {
		return nil, 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	predicates := []string{"b.deleted_at IS NULL"}
	args := []any{}
	if q != "" {
		normalized := fold(q)
		if trigramEligible(normalized) {
			ready, e := searchIndexReady(ctx, tx)
			if e != nil {
				return nil, 0, e
			}
			if ready {
				predicates = append(predicates, "b.id IN(SELECT rowid FROM book_search WHERE book_search MATCH ?)")
				args = append(args, `"`+strings.ReplaceAll(normalized, `"`, `""`)+`"`)
			}
		}
		predicates = append(predicates, "instr(library_search_text(CAST(CASE WHEN b.subtitle='' THEN b.title ELSE b.title||' '||b.subtitle END AS BLOB),CAST(b.authors AS BLOB),CAST(b.isbn AS BLOB)),?)>0")
		args = append(args, normalized)
	}
	if availability == "available" {
		predicates = append(predicates, `EXISTS(SELECT 1 FROM copies c WHERE c.book_id=b.id AND c.archived_at IS NULL AND NOT EXISTS(SELECT 1 FROM loans l WHERE l.copy_id=c.id AND l.returned_at IS NULL))`)
	}
	if shelf > 0 {
		predicates = append(predicates, `EXISTS(SELECT 1 FROM copies c WHERE c.book_id=b.id AND c.archived_at IS NULL AND c.shelf_id=?)`)
		args = append(args, shelf)
	}
	if strings.TrimSpace(options.Subject) != "" {
		predicates = append(predicates, `EXISTS(SELECT 1 FROM book_discovery d,json_each(d.subjects) j WHERE d.book_id=b.id AND library_label_fold(CAST(j.value AS BLOB))=?)`)
		args = append(args, fold(strings.TrimSpace(options.Subject)))
	}
	where := strings.Join(predicates, " AND ")
	var total int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM books b WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	ids := []int64{}
	if page < 1 {
		page = 1
	}
	if total == 0 || page > (total-1)/cataloguePageSize+1 {
		err = tx.Commit()
		return ids, total, err
	}
	queryArgs := append(append([]any{}, args...), cataloguePageSize, (page-1)*cataloguePageSize)
	rows, err := tx.QueryContext(ctx, "SELECT b.id FROM books b WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}

// Short/NUL queries cannot safely use a trigram phrase. The literal scan remains
// available for them and for databases whose SQLite lacks FTS5/trigram support.
func trigramEligible(q string) bool {
	return utf8.ValidString(q) && utf8.RuneCountInString(q) >= 3 && !strings.ContainsRune(q, 0) && len(q) <= 32768
}
func searchIndexReady(ctx context.Context, tx *sql.Tx) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='book_search' AND sql LIKE '%fts5%'`).Scan(&n)
	return n == 1, err
}

const searchTableSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS book_search USING fts5(document,tokenize='trigram case_sensitive 1')`
const searchInsertTrigger = `CREATE TRIGGER IF NOT EXISTS book_search_ai AFTER INSERT ON books BEGIN INSERT INTO book_search(rowid,document) VALUES(new.id,library_search_index(CAST(CASE WHEN new.subtitle='' THEN new.title ELSE new.title||' '||new.subtitle END AS BLOB),CAST(new.authors AS BLOB),CAST(new.isbn AS BLOB))); END`
const searchUpdateTrigger = `CREATE TRIGGER IF NOT EXISTS book_search_au AFTER UPDATE OF id,title,subtitle,authors,isbn ON books BEGIN DELETE FROM book_search WHERE rowid=old.id; INSERT INTO book_search(rowid,document) VALUES(new.id,library_search_index(CAST(CASE WHEN new.subtitle='' THEN new.title ELSE new.title||' '||new.subtitle END AS BLOB),CAST(new.authors AS BLOB),CAST(new.isbn AS BLOB))); END`
const searchDeleteTrigger = `CREATE TRIGGER IF NOT EXISTS book_search_ad AFTER DELETE ON books BEGIN DELETE FROM book_search WHERE rowid=old.id; END`

// EnsureSearchSchema only adds derived data. Startup can invoke the transaction
// helper below alongside other migrations after a verified household backup.
func EnsureSearchSchema(s *Store) error {
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = ensureSearchSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func ensureSearchSchema(ctx context.Context, tx *sql.Tx) error {
	ready, err := searchIndexReady(ctx, tx)
	if err != nil {
		return err
	}
	triggers, err := recognizedSearchTriggers(ctx, tx)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, searchTableSQL); err != nil {
		if strings.Contains(err.Error(), "no such module: fts5") || strings.Contains(err.Error(), "no such tokenizer: trigram") {
			return nil
		}
		return err
	}
	if triggers != 3 {
		for _, name := range []string{"book_search_ai", "book_search_au", "book_search_ad"} {
			if _, err = tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+name); err != nil {
				return err
			}
		}
	}
	for _, sql := range []string{searchInsertTrigger, searchUpdateTrigger, searchDeleteTrigger} {
		if _, err = tx.ExecContext(ctx, sql); err != nil {
			return err
		}
	}
	if !ready || triggers != 3 {
		return rebuildSearchIndex(ctx, tx)
	}
	return nil
}
func rebuildSearchIndex(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM book_search"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO book_search(rowid,document) SELECT id,library_search_index(CAST(CASE WHEN subtitle='' THEN title ELSE title||' '||subtitle END AS BLOB),CAST(authors AS BLOB),CAST(isbn AS BLOB)) FROM books`)
	return err
}

// RebuildSearchIndex repairs derived search documents without changing book data.
func (s *Store) RebuildSearchIndex(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ready, err := searchIndexReady(ctx, tx)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("trigram search index is not available")
	}
	if err = rebuildSearchIndex(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizedTriggerSQL(sql string) string {
	normalized := strings.Join(strings.Fields(strings.ToLower(sql)), " ")
	return strings.Replace(normalized, "create trigger if not exists ", "create trigger ", 1)
}
func recognizedSearchTriggers(ctx context.Context, tx *sql.Tx) (int, error) {
	expected := map[string]string{"book_search_ai": searchInsertTrigger, "book_search_au": searchUpdateTrigger, "book_search_ad": searchDeleteTrigger}
	rows, e := tx.QueryContext(ctx, `SELECT name,sql FROM sqlite_master WHERE type='trigger' AND name IN('book_search_ai','book_search_au','book_search_ad')`)
	if e != nil {
		return 0, e
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, definition string
		if e = rows.Scan(&name, &definition); e != nil {
			return 0, e
		}
		if normalizedTriggerSQL(definition) == normalizedTriggerSQL(expected[name]) {
			count++
		}
	}
	return count, rows.Err()
}

func catalogueOrder(value string) (string, error) {
	switch value {
	case "", "title":
		return "b.title COLLATE NOCASE,b.id", nil
	case "author":
		return "CASE WHEN COALESCE(json_extract(b.authors,'$[0]'),'')='' THEN 1 ELSE 0 END,json_extract(b.authors,'$[0]') COLLATE NOCASE,b.title COLLATE NOCASE,b.id", nil
	case "published_newest", "published_oldest":
		order := "DESC"
		if value == "published_oldest" {
			order = "ASC"
		}
		return "CASE WHEN library_publication_year(b.publication_date) IS NULL THEN 1 ELSE 0 END,library_publication_year(b.publication_date) " + order + ",b.title COLLATE NOCASE,b.id", nil
	case "first_published_newest", "first_published_oldest":
		order := "DESC"
		if value == "first_published_oldest" {
			order = "ASC"
		}
		return "CASE WHEN (SELECT first_published_year FROM book_bibliography WHERE book_id=b.id) IS NULL THEN 1 ELSE 0 END,(SELECT first_published_year FROM book_bibliography WHERE book_id=b.id) " + order + ",b.title COLLATE NOCASE,b.id", nil
	default:
		return "", errors.New("Choose a supported catalogue sort")
	}
}
