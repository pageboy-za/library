package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type Discovery struct {
	BookID      int64    `json:"book_id"`
	Description string   `json:"description"`
	Subjects    []string `json:"subjects"`
	Tags        []string `json:"tags"`
	Notes       string   `json:"notes"`
	Revision    int      `json:"revision"`
}

// EnsureDiscoverySchema is additive and safe to run against existing catalogues.
func EnsureDiscoverySchema(s *Store) error {
	tx, e := s.DB.BeginTx(context.Background(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = ensureDiscoverySchema(context.Background(), tx); e != nil {
		return e
	}
	return tx.Commit()
}
func ensureDiscoverySchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS book_discovery(book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,description TEXT NOT NULL DEFAULT '',subjects TEXT NOT NULL DEFAULT '[]',tags TEXT NOT NULL DEFAULT '[]',notes TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 1)`)
	return err
}

func (s *Store) Discovery(ctx context.Context, id int64) (Discovery, error) {
	d := Discovery{BookID: id, Subjects: []string{}, Tags: []string{}}
	var subjects, tags string
	err := s.DB.QueryRowContext(ctx, `SELECT description,subjects,tags,notes,revision FROM book_discovery WHERE book_id=?`, id).Scan(&d.Description, &subjects, &tags, &d.Notes, &d.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		err = s.DB.QueryRowContext(ctx, "SELECT 1 FROM books WHERE id=?", id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return d, ErrNotFound
		}
		return d, err
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(subjects), &d.Subjects); err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(tags), &d.Tags)
	return d, err
}
func cleanLabels(labels []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label != "" && !seen[fold(label)] {
			out = append(out, label)
			seen[fold(label)] = true
		}
	}
	return out
}
func (s *Store) SaveDiscovery(ctx context.Context, id int64, d Discovery) (Discovery, error) {
	return s.saveDiscovery(ctx, id, d, nil)
}

// SaveDiscoveryForBookRevision guards provider enrichment against a book edit
// during its lookup. Both book and discovery revisions are checked in one write.
func (s *Store) SaveDiscoveryForBookRevision(ctx context.Context, id int64, d Discovery, bookRevision int) (Discovery, error) {
	return s.saveDiscovery(ctx, id, d, &bookRevision)
}

func (s *Store) saveDiscovery(ctx context.Context, id int64, d Discovery, bookRevision *int) (Discovery, error) {
	guarded, expectedBookRevision := 0, 0
	if bookRevision != nil {
		guarded, expectedBookRevision = 1, *bookRevision
	}
	if len(d.Description) > 20000 || len(d.Notes) > 20000 || len(d.Subjects) > 100 || len(d.Tags) > 100 {
		return Discovery{}, errors.New("Keep descriptions and notes under 20,000 characters and use at most 100 labels")
	}
	d.Subjects = cleanLabels(d.Subjects)
	d.Tags = cleanLabels(d.Tags)
	for _, label := range append(append([]string{}, d.Subjects...), d.Tags...) {
		if len(label) > 200 {
			return Discovery{}, errors.New("Keep each subject or tag under 200 characters")
		}
	}
	subjects, _ := json.Marshal(d.Subjects)
	tags, _ := json.Marshal(d.Tags)
	var result sql.Result
	var err error
	if d.Revision == 0 {
		result, err = s.DB.ExecContext(ctx, `INSERT INTO book_discovery(book_id,description,subjects,tags,notes) SELECT id,?,?,?,? FROM books WHERE id=? AND (?=0 OR revision=?) ON CONFLICT(book_id) DO NOTHING`, d.Description, string(subjects), string(tags), d.Notes, id, guarded, expectedBookRevision)
	} else {
		result, err = s.DB.ExecContext(ctx, `UPDATE book_discovery SET description=?,subjects=?,tags=?,notes=?,revision=revision+1 WHERE book_id=? AND revision=? AND EXISTS(SELECT 1 FROM books WHERE id=book_discovery.book_id AND (?=0 OR revision=?))`, d.Description, string(subjects), string(tags), d.Notes, id, d.Revision, guarded, expectedBookRevision)
	}
	if err != nil {
		return Discovery{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		if _, err = s.Book(ctx, id); err != nil {
			return Discovery{}, err
		}
		return Discovery{}, ErrConflict
	}
	return s.Discovery(ctx, id)
}
