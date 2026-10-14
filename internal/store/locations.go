package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Bookcase struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Room     string `json:"room"`
	Revision int    `json:"revision"`
}

func (s *Store) Bookcases(ctx context.Context) ([]Bookcase, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT b.id,b.name,b.room,v.revision FROM bookcases b JOIN location_versions v ON v.kind='bookcase' AND v.id=b.id ORDER BY b.name COLLATE NOCASE,b.id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Bookcase{}
	for rows.Next() {
		var b Bookcase
		if e = rows.Scan(&b.ID, &b.Name, &b.Room, &b.Revision); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func validLocationName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 200 }

// Location mutations preserve stable IDs and protect the last-read name. Request
// keys replay successful operations even after the old name or row disappears.
func (s *Store) RenameShelf(ctx context.Context, id int64, name, expected string, revision int, key string) error {
	name = strings.TrimSpace(name)
	if id <= 0 || revision <= 0 || !validLocationName(name) {
		return errors.New("Enter a shelf name of at most 200 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = loanMutation(ctx, tx, key, []any{"rename-shelf", id, name, expected, revision}, func() (int64, error) {
		if e := advanceLocation(ctx, tx, "shelf", id, revision); e != nil {
			return 0, e
		}

		var caseID int64
		if e := tx.QueryRowContext(ctx, "SELECT bookcase_id FROM shelves WHERE id=? AND name=?", id, expected).Scan(&caseID); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return 0, ErrConflict
			}
			return 0, e
		}
		var duplicate int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM shelves WHERE bookcase_id=? AND name=? COLLATE NOCASE AND id<>?", caseID, name, id).Scan(&duplicate); e != nil {
			return 0, e
		}
		if duplicate > 0 {
			return 0, errors.New("That shelf name already exists in this bookcase")
		}
		if _, e := tx.ExecContext(ctx, "UPDATE shelves SET name=? WHERE id=?", name, id); e != nil {
			return 0, e
		}
		return id, nil
	})
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RenameBookcase(ctx context.Context, id int64, name, expected string, revision int, key string) error {
	name = strings.TrimSpace(name)
	if id <= 0 || revision <= 0 || !validLocationName(name) {
		return errors.New("Enter a bookcase name of at most 200 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = loanMutation(ctx, tx, key, []any{"rename-bookcase", id, name, expected, revision}, func() (int64, error) {
		if e := advanceLocation(ctx, tx, "bookcase", id, revision); e != nil {
			return 0, e
		}

		var found int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookcases WHERE id=? AND name=?", id, expected).Scan(&found); e != nil {
			return 0, e
		}
		if found == 0 {
			return 0, ErrConflict
		}
		var duplicate int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookcases WHERE name=? COLLATE NOCASE AND id<>?", name, id).Scan(&duplicate); e != nil {
			return 0, e
		}
		if duplicate > 0 {
			return 0, errors.New("That bookcase name already exists")
		}
		if _, e := tx.ExecContext(ctx, "UPDATE bookcases SET name=? WHERE id=?", name, id); e != nil {
			return 0, e
		}
		return id, nil
	})
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RemoveShelf(ctx context.Context, id int64, expected string, revision int, key string) error {
	if id <= 0 || revision <= 0 {
		return errors.New("A shelf ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = loanMutation(ctx, tx, key, []any{"remove-shelf", id, expected, revision}, func() (int64, error) {
		if e := advanceLocation(ctx, tx, "shelf", id, revision); e != nil {
			return 0, e
		}

		var referenced int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM copies WHERE shelf_id=?", id).Scan(&referenced); e != nil {
			return 0, e
		}
		if referenced > 0 {
			return 0, errors.New("This shelf is still assigned to a copy, including archived copies. Move those copies before removing it")
		}
		r, e := tx.ExecContext(ctx, "DELETE FROM shelves WHERE id=? AND name=?", id, expected)
		if e != nil {
			return 0, e
		}
		n, _ := r.RowsAffected()
		if n == 0 {
			return 0, ErrConflict
		}
		return id, nil
	})
	if e != nil {
		return e
	}
	return tx.Commit()
}

// RemoveBookcase deletes only a bookcase whose shelves have no copy references.
// Its empty shelves are removed together, with no silent reassignment of books.
func (s *Store) RemoveBookcase(ctx context.Context, id int64, expected string, revision int, key string) error {
	if id <= 0 || revision <= 0 {
		return errors.New("A bookcase ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = loanMutation(ctx, tx, key, []any{"remove-bookcase", id, expected, revision}, func() (int64, error) {
		if e := advanceLocation(ctx, tx, "bookcase", id, revision); e != nil {
			return 0, e
		}

		var found int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bookcases WHERE id=? AND name=?", id, expected).Scan(&found); e != nil {
			return 0, e
		}
		if found == 0 {
			return 0, ErrConflict
		}
		var referenced int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM copies c JOIN shelves sh ON sh.id=c.shelf_id WHERE sh.bookcase_id=?", id).Scan(&referenced); e != nil {
			return 0, e
		}
		if referenced > 0 {
			return 0, errors.New("This bookcase has shelves assigned to copies, including archived copies. Move those copies before removing it")
		}
		if _, e := tx.ExecContext(ctx, "UPDATE location_versions SET revision=revision+1 WHERE kind='shelf' AND id IN(SELECT id FROM shelves WHERE bookcase_id=?)", id); e != nil {
			return 0, e
		}
		if _, e := tx.ExecContext(ctx, "DELETE FROM shelves WHERE bookcase_id=?", id); e != nil {
			return 0, e
		}
		if _, e := tx.ExecContext(ctx, "DELETE FROM bookcases WHERE id=?", id); e != nil {
			return 0, e
		}
		return id, nil
	})
	if e != nil {
		return e
	}
	return tx.Commit()
}

// EnsureLocationSchema initializes live rows once. Tombstones deliberately have
// no foreign key and survive deletion, preventing stale writes when SQLite reuses IDs.
func EnsureLocationSchema(s *Store) error {
	ctx := context.Background()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = ensureLocationSchema(ctx, tx); e != nil {
		return e
	}
	return tx.Commit()
}
func ensureLocationSchema(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS location_versions(kind TEXT NOT NULL CHECK(kind IN('shelf','bookcase')),id INTEGER NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),PRIMARY KEY(kind,id))`, `INSERT OR IGNORE INTO location_versions(kind,id,revision) SELECT 'shelf',id,1 FROM shelves`, `INSERT OR IGNORE INTO location_versions(kind,id,revision) SELECT 'bookcase',id,1 FROM bookcases`} {
		if _, e := tx.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return nil
}

func createLocationVersion(ctx context.Context, tx *sql.Tx, kind string, id int64) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO location_versions(kind,id,revision) VALUES(?,?,1) ON CONFLICT(kind,id) DO UPDATE SET revision=revision+1`, kind, id)
	return e
}
func advanceLocation(ctx context.Context, tx *sql.Tx, kind string, id int64, expected int) error {
	r, e := tx.ExecContext(ctx, `UPDATE location_versions SET revision=revision+1 WHERE kind=? AND id=? AND revision=?`, kind, id, expected)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
