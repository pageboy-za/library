package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Borrower struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Notes      string  `json:"notes"`
	ArchivedAt *string `json:"archived_at"`
}
type Loan struct {
	ID           int64   `json:"id"`
	CopyID       int64   `json:"copy_id"`
	BookID       int64   `json:"book_id"`
	BorrowerID   int64   `json:"borrower_id"`
	Borrower     string  `json:"borrower"`
	CheckedOutAt string  `json:"checked_out_at"`
	DueDate      *string `json:"due_date"`
	ReturnedAt   *string `json:"returned_at"`
}

func (s *Store) Borrowers(ctx context.Context) ([]Borrower, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,name,notes,archived_at FROM borrowers WHERE archived_at IS NULL ORDER BY name COLLATE NOCASE,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Borrower{}
	for rows.Next() {
		var b Borrower
		if e = rows.Scan(&b.ID, &b.Name, &b.Notes, &b.ArchivedAt); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) Loans(ctx context.Context, bookID int64) ([]Loan, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT l.id,l.copy_id,c.book_id,l.borrower_id,b.name,l.checked_out_at,l.due_date,l.returned_at FROM loans l JOIN copies c ON c.id=l.copy_id JOIN borrowers b ON b.id=l.borrower_id WHERE c.book_id=? ORDER BY l.id DESC`, bookID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Loan{}
	for rows.Next() {
		var l Loan
		if e = rows.Scan(&l.ID, &l.CopyID, &l.BookID, &l.BorrowerID, &l.Borrower, &l.CheckedOutAt, &l.DueDate, &l.ReturnedAt); e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func loanMutation(ctx context.Context, tx *sql.Tx, key string, payload any, write func() (int64, error)) (int64, error) {
	if len(key) < 8 || len(key) > 128 {
		return 0, errors.New("A valid request key is required")
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	var prior string
	var id int64
	e := tx.QueryRowContext(ctx, "SELECT payload_hash,result_id FROM mutation_requests WHERE key=?", key).Scan(&prior, &id)
	if e == nil {
		if prior != hash {
			return 0, ErrConflict
		}
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	id, e = write()
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO mutation_requests(key,payload_hash,result_id) VALUES(?,?,?)", key, hash, id)
	return id, e
}
func (s *Store) AddBorrower(ctx context.Context, name, notes, key string) (int64, error) {
	name = strings.TrimSpace(name)
	notes = strings.TrimSpace(notes)
	if name == "" || len(name) > 200 || len(notes) > 2000 {
		return 0, errors.New("Enter a name of at most 200 characters and notes of at most 2000 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	id, e := loanMutation(ctx, tx, key, []any{"borrower", name, notes}, func() (int64, error) {
		r, e := tx.ExecContext(ctx, "INSERT INTO borrowers(name,notes) VALUES(?,?)", name, notes)
		if e != nil {
			return 0, e
		}
		return r.LastInsertId()
	})
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}
func (s *Store) ArchiveBorrower(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.DB.ExecContext(ctx, `UPDATE borrowers SET archived_at=COALESCE(archived_at,CURRENT_TIMESTAMP) WHERE id=? AND NOT EXISTS(SELECT 1 FROM loans WHERE borrower_id=? AND returned_at IS NULL)`, id, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return errors.New("Borrower missing or still has books on loan")
	}
	return nil
}
func (s *Store) Checkout(ctx context.Context, copyID, borrowerID int64, due, key string) (int64, error) {
	if copyID <= 0 || borrowerID <= 0 {
		return 0, errors.New("Choose a copy and borrower")
	}
	if due != "" {
		if _, e := time.Parse("2006-01-02", due); e != nil {
			return 0, errors.New("Due date must use YYYY-MM-DD")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	id, e := loanMutation(ctx, tx, key, []any{"checkout", copyID, borrowerID, due}, func() (int64, error) {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM copies c,borrowers b WHERE c.id=? AND c.archived_at IS NULL AND EXISTS(SELECT 1 FROM books WHERE id=c.book_id AND deleted_at IS NULL) AND b.id=? AND b.archived_at IS NULL`, copyID, borrowerID).Scan(&count); e != nil {
			return 0, e
		}
		if count != 1 {
			return 0, errors.New("Copy or borrower is missing or archived")
		}
		var date any
		if due != "" {
			date = due
		}
		r, e := tx.ExecContext(ctx, "INSERT INTO loans(copy_id,borrower_id,due_date) VALUES(?,?,?)", copyID, borrowerID, date)
		if e != nil {
			return 0, errors.New("This copy is already on loan; refresh its status")
		}
		return r.LastInsertId()
	})
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}
func (s *Store) ReturnLoan(ctx context.Context, loanID int64, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	id, e := loanMutation(ctx, tx, key, []any{"return", loanID}, func() (int64, error) {
		r, e := tx.ExecContext(ctx, "UPDATE loans SET returned_at=CURRENT_TIMESTAMP WHERE id=? AND returned_at IS NULL", loanID)
		if e != nil {
			return 0, e
		}
		n, _ := r.RowsAffected()
		if n == 0 {
			return 0, errors.New("Loan is missing or already returned")
		}
		return loanID, nil
	})
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}

// EditBorrower uses the last-read name and notes as an optimistic concurrency
// check, avoiding a schema migration for this initial lending slice.
func (s *Store) EditBorrower(ctx context.Context, id int64, name, notes, expectedName, expectedNotes string) error {
	name = strings.TrimSpace(name)
	notes = strings.TrimSpace(notes)
	if id <= 0 || name == "" || len(name) > 200 || len(notes) > 2000 {
		return errors.New("Enter a borrower name of at most 200 characters and notes of at most 2000 characters")
	}
	r, e := s.DB.ExecContext(ctx, `UPDATE borrowers SET name=?,notes=? WHERE id=? AND archived_at IS NULL AND name=? AND notes=?`, name, notes, id, expectedName, expectedNotes)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

type LoanOverview struct {
	Loan
	BookTitle string `json:"book_title"`
	CopyLabel string `json:"copy_label"`
	Shelf     string `json:"shelf"`
}
type ActiveLoanPage struct {
	Loans    []LoanOverview `json:"loans"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Today    string         `json:"today"`
}

// ActiveLoans includes every outstanding loan, including copies later archived.
// Due-date comparisons use a supplied ISO date so tests and callers agree.
func (s *Store) ActiveLoans(ctx context.Context, borrowerID int64, due string, page int, today string) (ActiveLoanPage, error) {
	out := ActiveLoanPage{Loans: []LoanOverview{}, Page: page, PageSize: 24, Today: today}
	if borrowerID < 0 || page < 1 || page > 100000 {
		return out, errors.New("Invalid borrower or page")
	}
	if _, e := time.Parse("2006-01-02", today); e != nil {
		return out, e
	}
	where := " WHERE l.returned_at IS NULL"
	args := []any{}
	if borrowerID > 0 {
		where += " AND l.borrower_id=?"
		args = append(args, borrowerID)
	}
	switch due {
	case "":
	case "overdue":
		where += " AND l.due_date < ?"
		args = append(args, today)
	case "upcoming":
		where += " AND l.due_date >= ?"
		args = append(args, today)
	default:
		return out, errors.New("Due filter must be overdue or upcoming")
	}
	if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM loans l"+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	query := `SELECT l.id,l.copy_id,c.book_id,l.borrower_id,b.name,l.checked_out_at,l.due_date,l.returned_at,bo.title,c.label,COALESCE(bc.name||' / '||sh.name,'Unassigned') FROM loans l JOIN copies c ON c.id=l.copy_id JOIN books bo ON bo.id=c.book_id JOIN borrowers b ON b.id=l.borrower_id LEFT JOIN shelves sh ON sh.id=c.shelf_id LEFT JOIN bookcases bc ON bc.id=sh.bookcase_id` + where + ` ORDER BY l.due_date IS NULL,l.due_date,l.id LIMIT 24 OFFSET ?`
	args = append(args, (page-1)*24)
	rows, e := s.DB.QueryContext(ctx, query, args...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var l LoanOverview
		if e = rows.Scan(&l.ID, &l.CopyID, &l.BookID, &l.BorrowerID, &l.Borrower, &l.CheckedOutAt, &l.DueDate, &l.ReturnedAt, &l.BookTitle, &l.CopyLabel, &l.Shelf); e != nil {
			return out, e
		}
		out.Loans = append(out.Loans, l)
	}
	return out, rows.Err()
}

// ArchiveCopy removes a physical copy from the current inventory while keeping
// every loan in history. It must be returned before archival. Revision checking
// prevents archiving a copy that another screen has moved or changed.
func (s *Store) ArchiveCopy(ctx context.Context, copyID int64, revision int, key string) (int64, error) {
	if copyID <= 0 || revision <= 0 {
		return 0, errors.New("A copy ID and current revision are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	id, e := loanMutation(ctx, tx, key, []any{"archive-copy", copyID, revision}, func() (int64, error) {
		var active int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM loans WHERE copy_id=? AND returned_at IS NULL", copyID).Scan(&active); e != nil {
			return 0, e
		}
		if active > 0 {
			return 0, errors.New("Return this copy before archiving it")
		}
		result, e := tx.ExecContext(ctx, "UPDATE copies SET archived_at=CURRENT_TIMESTAMP,revision=revision+1 WHERE id=? AND revision=? AND archived_at IS NULL", copyID, revision)
		if e != nil {
			return 0, e
		}
		changed, _ := result.RowsAffected()
		if changed == 0 {
			return 0, ErrConflict
		}
		return copyID, nil
	})
	if e != nil {
		return 0, e
	}
	return id, tx.Commit()
}
