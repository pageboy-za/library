package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"home-library/internal/metrics"
	_ "modernc.org/sqlite"
	"strings"
	"sync"
)

//go:embed schema.sql
var schema string
var ErrConflict = errors.New("This record changed or this request was already used. Reload and try again")
var ErrNotFound = errors.New("Record not found")

type Store struct {
	DB *sql.DB
	mu sync.Mutex
}
type Book struct {
	TotalCopyCount    int      `json:"total_copy_count"`
	DeletedAt         *string  `json:"deleted_at"`
	ID                int64    `json:"id"`
	ISBN              string   `json:"isbn"`
	Title             string   `json:"title"`
	Subtitle          string   `json:"subtitle"`
	Authors           []string `json:"authors"`
	Publisher         string   `json:"publisher"`
	PublicationDate   string   `json:"publication_date"`
	SourceURL         string   `json:"source_url"`
	SourceID          string   `json:"source_id"`
	Revision          int      `json:"revision"`
	Copies            []Copy   `json:"copies"`
	CoverURL          string   `json:"cover_url"`
	CoverThumbnailURL string   `json:"cover_thumbnail_url"`
	Count             int      `json:"count"`
	Available         int      `json:"available"`
}
type Copy struct {
	ID       int64  `json:"id"`
	ShelfID  *int64 `json:"shelf_id"`
	Shelf    string `json:"shelf"`
	Label    string `json:"label"`
	Out      bool   `json:"out"`
	Revision int    `json:"revision"`
}
type Shelf struct {
	Revision   int    `json:"revision"`
	ID         int64  `json:"id"`
	BookcaseID int64  `json:"bookcase_id"`
	Name       string `json:"name"`
	Bookcase   string `json:"bookcase"`
}
type Input struct {
	ISBN            string   `json:"isbn"`
	Title           string   `json:"title"`
	Subtitle        *string  `json:"subtitle,omitempty"`
	Authors         []string `json:"authors"`
	Publisher       string   `json:"publisher"`
	PublicationDate string   `json:"publication_date"`
	SourceURL       string   `json:"source_url"`
	SourceID        string   `json:"source_id"`
	ShelfID         *int64   `json:"shelf_id"`
	RequestKey      string   `json:"request_key"`
	Revision        int      `json:"revision"`
}

func Open(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = prepareDatabase(context.Background(), db); e != nil {
		db.Close()
		return nil, e
	}
	return &Store{DB: db}, nil
}

func (s *Store) Book(ctx context.Context, id int64) (Book, error) {
	b, e := s.BookIncludingDeleted(ctx, id)
	if e == nil && b.DeletedAt != nil {
		return Book{}, ErrNotFound
	}
	return b, e
}
func (s *Store) BookIncludingDeleted(ctx context.Context, id int64) (Book, error) {
	b := Book{Authors: []string{}, Copies: []Copy{}}
	var a string
	e := s.DB.QueryRowContext(ctx, "SELECT id,COALESCE(isbn,''),title,subtitle,authors,publisher,publication_date,source_url,source_id,revision,deleted_at,(SELECT COUNT(*) FROM copies WHERE copies.book_id=books.id) FROM books WHERE id=?", id).Scan(&b.ID, &b.ISBN, &b.Title, &b.Subtitle, &a, &b.Publisher, &b.PublicationDate, &b.SourceURL, &b.SourceID, &b.Revision, &b.DeletedAt, &b.TotalCopyCount)
	if errors.Is(e, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if e != nil {
		return b, e
	}
	if e = json.Unmarshal([]byte(a), &b.Authors); e != nil {
		return b, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT c.id,c.shelf_id,COALESCE(bc.name||' / '||sh.name,'Unassigned'),c.label,EXISTS(SELECT 1 FROM loans l WHERE l.copy_id=c.id AND returned_at IS NULL),c.revision FROM copies c LEFT JOIN shelves sh ON sh.id=c.shelf_id LEFT JOIN bookcases bc ON bc.id=sh.bookcase_id WHERE c.book_id=? AND c.archived_at IS NULL ORDER BY c.id`, id)
	if e != nil {
		return b, e
	}
	defer rows.Close()
	for rows.Next() {
		var c Copy
		if e = rows.Scan(&c.ID, &c.ShelfID, &c.Shelf, &c.Label, &c.Out, &c.Revision); e != nil {
			return b, e
		}
		b.Copies = append(b.Copies, c)
		b.Count++
		if !c.Out {
			b.Available++
		}
	}
	if e = rows.Err(); e != nil {
		return b, e
	}
	rows.Close()
	b.CoverURL, e = s.SelectedCoverURL(ctx, id)
	if e != nil {
		return b, e
	}
	b.CoverThumbnailURL, e = s.SelectedCoverThumbnailURL(ctx, id)
	return b, e
}
func (s *Store) ByISBN(ctx context.Context, isbn string) (Book, error) {
	var id int64
	e := s.DB.QueryRowContext(ctx, "SELECT id FROM books WHERE isbn=?", isbn).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if e != nil {
		return Book{}, e
	}
	return s.BookIncludingDeleted(ctx, id)
}
func fold(s string) string { return strings.ToLower(norm.NFKC.String(s)) }
func (s *Store) List(ctx context.Context, q string, shelf int64, availability string, page int) ([]Book, int, error) {
	ids, total, e := s.searchIDs(ctx, q, shelf, availability, page)
	if e != nil {
		return nil, 0, e
	}
	result := make([]Book, 0, len(ids))
	for _, id := range ids {
		b, e := s.Book(ctx, id)
		if e != nil {
			return nil, 0, e
		}
		result = append(result, b)
	}
	return result, total, nil
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func replay(tx *sql.Tx, key, hash string) (int64, error) {
	var id int64
	var previous string
	e := tx.QueryRow("SELECT payload_hash,result_id FROM mutation_requests WHERE key=?", key).Scan(&previous, &id)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	if previous != hash {
		return 0, ErrConflict
	}
	return id, nil
}
func (s *Store) Save(ctx context.Context, in Input) (Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.Title = strings.TrimSpace(in.Title)
	if in.Subtitle != nil {
		value := strings.TrimSpace(*in.Subtitle)
		if len(value) > 1000 {
			return Book{}, errors.New("Subtitle must contain at most 1000 bytes")
		}
		in.Subtitle = &value
	}
	if in.Title == "" || len(in.Title) > 500 || len(in.RequestKey) < 8 || len(in.RequestKey) > 100 {
		return Book{}, errors.New("A title and request key are required")
	}
	if in.Authors == nil {
		in.Authors = []string{}
	}
	hash := fingerprint(in)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Book{}, e
	}
	defer tx.Rollback()
	id, e := replay(tx, in.RequestKey, hash)
	if e != nil {
		return Book{}, e
	}
	if id > 0 {
		tx.Rollback()
		return s.Book(ctx, id)
	}
	if in.ISBN != "" {
		e = tx.QueryRow("SELECT id FROM books WHERE isbn=?", in.ISBN).Scan(&id)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return Book{}, e
		}
	}
	if id == 0 {
		a, _ := json.Marshal(in.Authors)
		r, e := tx.Exec("INSERT INTO books(isbn,title,subtitle,authors,publisher,publication_date,source_url,source_id) VALUES(NULLIF(?,''),?,COALESCE(?,''),?,?,?,?,?)", in.ISBN, in.Title, in.Subtitle, string(a), in.Publisher, in.PublicationDate, in.SourceURL, in.SourceID)
		if e != nil {
			return Book{}, e
		}
		id, e = r.LastInsertId()
		if e != nil {
			return Book{}, e
		}
		if _, e = tx.Exec("INSERT INTO copies(book_id,shelf_id,label) VALUES(?,?,?)", id, in.ShelfID, "Copy 1"); e != nil {
			return Book{}, errors.New("Choose an existing shelf")
		}
	}
	if _, e = tx.Exec("INSERT INTO mutation_requests VALUES(?,?,?)", in.RequestKey, hash, id); e != nil {
		return Book{}, e
	}
	if e = metrics.Measure(ctx, "database_commit", tx.Commit); e != nil {
		return Book{}, e
	}
	return s.Book(ctx, id)
}
func (s *Store) Edit(ctx context.Context, id int64, in Input) (Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.Title = strings.TrimSpace(in.Title)
	if in.Subtitle != nil {
		value := strings.TrimSpace(*in.Subtitle)
		if len(value) > 1000 {
			return Book{}, errors.New("Subtitle must contain at most 1000 bytes")
		}
		in.Subtitle = &value
	}
	if in.Title == "" || len(in.Title) > 500 {
		return Book{}, errors.New("Title is required and must contain at most 500 bytes")
	}
	if in.Authors == nil {
		in.Authors = []string{}
	}
	in.SourceURL = strings.TrimSpace(in.SourceURL)
	in.SourceID = strings.TrimSpace(in.SourceID)
	a, _ := json.Marshal(in.Authors)
	// SQL CASE expressions inspect the previous ISBN in this one revision-checked
	// write. Changing edition identity clears stale provider provenance; an omitted
	// source on ordinary edits preserves it for compatibility with older clients.
	r, e := s.DB.ExecContext(ctx, `UPDATE books SET isbn=NULLIF(?,''),title=?,subtitle=COALESCE(?,subtitle),authors=?,publisher=?,publication_date=?,source_url=CASE WHEN COALESCE(isbn,'')<>? THEN '' WHEN ?<>'' THEN ? ELSE source_url END,source_id=CASE WHEN COALESCE(isbn,'')<>? THEN '' WHEN ?<>'' THEN ? ELSE source_id END,revision=revision+1 WHERE id=? AND revision=? AND deleted_at IS NULL`, in.ISBN, in.Title, in.Subtitle, string(a), in.Publisher, in.PublicationDate, in.ISBN, in.SourceURL, in.SourceURL, in.ISBN, in.SourceID, in.SourceID, id, in.Revision)
	if e != nil {
		return Book{}, ErrConflict
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return Book{}, ErrConflict
	}
	return s.Book(ctx, id)
}

func (s *Store) AddCopy(ctx context.Context, id int64, shelf *int64, key string) (Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(key) < 8 || len(key) > 100 {
		return Book{}, errors.New("Request key required")
	}
	hash := fingerprint(struct {
		Operation string
		Book      int64
		Shelf     *int64
	}{"copy", id, shelf})
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Book{}, e
	}
	defer tx.Rollback()
	existing, e := replay(tx, key, hash)
	if e != nil {
		return Book{}, e
	}
	if existing > 0 {
		tx.Rollback()
		return s.Book(ctx, id)
	}
	var present int
	if e = tx.QueryRow("SELECT count(*) FROM books WHERE id=? AND deleted_at IS NULL", id).Scan(&present); e != nil {
		return Book{}, e
	}
	if present != 1 {
		return Book{}, ErrNotFound
	}
	var count int
	if e = tx.QueryRow("SELECT count(*) FROM copies WHERE book_id=?", id).Scan(&count); e != nil {
		return Book{}, e
	}
	if _, e = tx.Exec("INSERT INTO copies(book_id,shelf_id,label) VALUES(?,?,?)", id, shelf, fmt.Sprintf("Copy %d", count+1)); e != nil {
		return Book{}, errors.New("Book or shelf not found")
	}
	if _, e = tx.Exec("INSERT INTO mutation_requests VALUES(?,?,?)", key, hash, id); e != nil {
		return Book{}, e
	}
	if e = metrics.Measure(ctx, "database_commit", tx.Commit); e != nil {
		return Book{}, e
	}
	return s.Book(ctx, id)
}
func (s *Store) Shelves(ctx context.Context) ([]Shelf, error) {
	out := []Shelf{}
	r, e := s.DB.QueryContext(ctx, "SELECT sh.id,sh.bookcase_id,sh.name,b.name,v.revision FROM shelves sh JOIN bookcases b ON b.id=sh.bookcase_id JOIN location_versions v ON v.kind='shelf' AND v.id=sh.id ORDER BY b.name,sh.name")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	for r.Next() {
		var sh Shelf
		if e = r.Scan(&sh.ID, &sh.BookcaseID, &sh.Name, &sh.Bookcase, &sh.Revision); e != nil {
			return nil, e
		}
		out = append(out, sh)
	}
	return out, r.Err()
}
func (s *Store) AddShelf(ctx context.Context, bookcase, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bookcase = strings.TrimSpace(bookcase)
	name = strings.TrimSpace(name)
	if !validLocationName(bookcase) || !validLocationName(name) {
		return errors.New("Bookcase and shelf names must contain 1 to 200 characters")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id int64
	e = tx.QueryRow("SELECT id FROM bookcases WHERE name=? COLLATE NOCASE ORDER BY id LIMIT 1", bookcase).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		r, e := tx.Exec("INSERT INTO bookcases(name) VALUES(?)", bookcase)
		if e != nil {
			return e
		}
		id, _ = r.LastInsertId()
		if e = createLocationVersion(ctx, tx, "bookcase", id); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	var duplicate int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM shelves WHERE bookcase_id=? AND name=? COLLATE NOCASE", id, name).Scan(&duplicate); e != nil {
		return e
	}
	if duplicate > 0 {
		return errors.New("That shelf already exists")
	}
	r, e := tx.Exec("INSERT INTO shelves(bookcase_id,name) VALUES(?,?)", id, name)
	if e != nil {
		return errors.New("That shelf already exists")
	}
	shelfID, e := r.LastInsertId()
	if e != nil {
		return e
	}
	if e = createLocationVersion(ctx, tx, "shelf", shelfID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) MoveCopy(ctx context.Context, id int64, shelf *int64, revision int) error {
	r, e := s.DB.ExecContext(ctx, "UPDATE copies SET shelf_id=?,revision=revision+1 WHERE id=? AND revision=? AND EXISTS(SELECT 1 FROM books WHERE books.id=copies.book_id AND deleted_at IS NULL)", shelf, id, revision)
	if e != nil {
		return errors.New("Choose an existing shelf")
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}
