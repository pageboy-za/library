package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
)

var ErrMemberPass = errors.New("This pass is unavailable. Scan a current approved member pass, or ask a librarian for help")
var ErrNoLendableCopy = errors.New("No copy is available to lend. Refresh the book or ask a librarian for help")
var memberPassPattern = regexp.MustCompile(`^mbr_[A-Za-z0-9_-]{32}$`)

// MemberCheckout resolves the pass and allocates a copy in one transaction.
// The caller cannot choose a different borrower or bypass membership approval.
func (s *Store) MemberCheckout(ctx context.Context, bookID int64, cardCode, key string) (int64, error) {
	if !memberPassPattern.MatchString(cardCode) {
		return 0, ErrMemberPass
	}
	if bookID <= 0 {
		return 0, ErrNoLendableCopy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	loanID, err := loanMutation(ctx, tx, key, []any{"member-checkout", bookID, cardCode}, func() (int64, error) {
		var borrowerID int64
		err := tx.QueryRowContext(ctx, `SELECT b.id FROM member_registrations mr
			JOIN borrowers b ON b.id=mr.borrower_id
			WHERE mr.card_code=? AND mr.status='approved' AND b.archived_at IS NULL
			AND NOT EXISTS(SELECT 1 FROM member_registrations other WHERE other.borrower_id=b.id AND other.status!='approved')`, cardCode).Scan(&borrowerID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrMemberPass
		}
		if err != nil {
			return 0, err
		}
		var copyID int64
		err = tx.QueryRowContext(ctx, `SELECT c.id FROM copies c JOIN books b ON b.id=c.book_id
			WHERE b.id=? AND b.deleted_at IS NULL AND c.archived_at IS NULL
			AND NOT EXISTS(SELECT 1 FROM loans l WHERE l.copy_id=c.id AND l.returned_at IS NULL)
			ORDER BY c.id LIMIT 1`, bookID).Scan(&copyID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNoLendableCopy
		}
		if err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO loans(copy_id,borrower_id) VALUES(?,?)", copyID, borrowerID)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	})
	if err != nil {
		return 0, err
	}
	return loanID, tx.Commit()
}
