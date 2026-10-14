package store

import (
	"context"
	"database/sql"
	"errors"
)

// SetBookDeleted keeps catalogue identity, copies, covers and loan history intact.
// The version and loan checks share the transaction with the state change.
func (s *Store) SetBookDeleted(ctx context.Context, id int64, revision int, deleted bool, key string, expectedCount ...int) (Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Book{}, e
	}
	defer tx.Rollback()
	_, e = loanMutation(ctx, tx, key, []any{"book-deleted", id, revision, deleted, expectedCount}, func() (int64, error) {
		var current int
		var stamp *string
		if e := tx.QueryRowContext(ctx, "SELECT revision,deleted_at FROM books WHERE id=?", id).Scan(&current, &stamp); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return 0, ErrNotFound
			}
			return 0, e
		}
		if current != revision || (stamp != nil) == deleted {
			return 0, ErrConflict
		}
		if len(expectedCount) > 0 {
			var copies int
			if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM copies WHERE book_id=?", id).Scan(&copies); e != nil {
				return 0, e
			}
			if copies != expectedCount[0] {
				return 0, ErrConflict
			}
		}
		if deleted {
			var active int
			if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM loans l JOIN copies c ON c.id=l.copy_id WHERE c.book_id=? AND l.returned_at IS NULL", id).Scan(&active); e != nil {
				return 0, e
			}
			if active > 0 {
				return 0, errors.New("Return all copies on loan before deleting this book")
			}
		}
		value := "NULL"
		if deleted {
			value = "CURRENT_TIMESTAMP"
			var archivedAt string
			if e := tx.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&archivedAt); e != nil {
				return 0, e
			}
			if _, e := tx.ExecContext(ctx, "INSERT INTO book_deleted_copies(book_id,copy_id,archived_at) SELECT book_id,id,? FROM copies WHERE book_id=? AND archived_at IS NULL", archivedAt, id); e != nil {
				return 0, e
			}
			if _, e := tx.ExecContext(ctx, "UPDATE copies SET archived_at=?,revision=revision+1 WHERE book_id=? AND archived_at IS NULL", archivedAt, id); e != nil {
				return 0, e
			}
		} else {
			// Only reverse this deletion's archives. Pre-existing archives and new copies
			// deliberately added by an older app are left as they are.
			if _, e := tx.ExecContext(ctx, `UPDATE copies SET archived_at=NULL,revision=revision+1 WHERE book_id=? AND EXISTS(SELECT 1 FROM book_deleted_copies d WHERE d.copy_id=copies.id AND d.archived_at=copies.archived_at)`, id); e != nil {
				return 0, e
			}
			if _, e := tx.ExecContext(ctx, "DELETE FROM book_deleted_copies WHERE book_id=?", id); e != nil {
				return 0, e
			}
		}
		_, e := tx.ExecContext(ctx, "UPDATE books SET deleted_at="+value+",revision=revision+1 WHERE id=? AND revision=?", id, revision)
		return id, e
	})
	if e != nil {
		return Book{}, e
	}
	if e = tx.Commit(); e != nil {
		return Book{}, e
	}
	return s.BookIncludingDeleted(ctx, id)
}

func (s *Store) DeletedBooks(ctx context.Context) ([]Book, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id FROM books WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC,id DESC")
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Book{}
	for _, id := range ids {
		b, e := s.BookIncludingDeleted(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, nil
}
