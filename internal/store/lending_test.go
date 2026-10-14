package store

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLendingRetryConcurrencyAndHistory(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(ctx, Input{Title: "Test lending", Authors: []string{}, RequestKey: "lending-book"})
	if e != nil {
		t.Fatal(e)
	}
	borrower, e := s.AddBorrower(ctx, "Ada", "", "borrower-key")
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.AddBorrower(ctx, "Ada", "", "borrower-key")
	if e != nil || again != borrower {
		t.Fatal("Borrower retry failed")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Checkout(ctx, b.Copies[0].ID, borrower, "", "same-checkout"); e == nil {
				successes.Add(1)
			} else {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 8 {
		t.Fatal("Retries failed")
	}
	loans, e := s.Loans(ctx, b.ID)
	if e != nil || len(loans) != 1 {
		t.Fatalf("Duplicate loans: %+v %v", loans, e)
	}
	if e = s.ArchiveBorrower(ctx, borrower); e == nil {
		t.Fatal("Archived borrower with loan")
	}
	if _, e = s.Checkout(ctx, b.Copies[0].ID, borrower, "", "different-key"); e == nil {
		t.Fatal("Double checkout accepted")
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ReturnLoan(ctx, loans[0].ID, "same-return-key"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	got, e := s.Book(ctx, b.ID)
	if e != nil || got.Available != 1 {
		t.Fatal("Return did not free copy")
	}
	history, _ := s.Loans(ctx, b.ID)
	if len(history) != 1 || history[0].ReturnedAt == nil {
		t.Fatal("History lost")
	}
	if e = s.ArchiveBorrower(ctx, borrower); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Checkout(ctx, b.Copies[0].ID, borrower, "", "archived-checkout"); e == nil {
		t.Fatal("Archived borrower accepted")
	}
}
func TestLendingCompetingCheckout(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, _ := s.Save(ctx, Input{Title: "Competition", Authors: []string{}, RequestKey: "competing-book"})
	borrower, _ := s.AddBorrower(ctx, "Grace", "", "competing-borrower")
	var won atomic.Int32
	var wg sync.WaitGroup
	for _, key := range []string{"first-request", "second-request"} {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if _, e := s.Checkout(ctx, b.Copies[0].ID, borrower, "2026-12-01", k); e == nil {
				won.Add(1)
			}
		}(key)
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("Expected one winner, got %d", won.Load())
	}
}

func TestBorrowerEditingAndLoanFilters(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ada, e := s.AddBorrower(ctx, "Ada", "Initial note", "edit-borrower-ada")
	if e != nil {
		t.Fatal(e)
	}
	grace, e := s.AddBorrower(ctx, "Grace", "", "edit-borrower-grace")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.EditBorrower(ctx, ada, "Ada Lovelace", "Updated", "Ada", "Initial note"); e != nil {
		t.Fatal(e)
	}
	if e = s.EditBorrower(ctx, ada, "Clobber", "bad", "Ada", "Initial note"); e == nil {
		t.Fatal("Stale update accepted")
	}
	b, e := s.Save(ctx, Input{Title: "Library loans", Authors: []string{}, RequestKey: "filter-book-request"})
	if e != nil {
		t.Fatal(e)
	}
	s.AddCopy(ctx, b.ID, nil, "filter-second-copy")
	s.AddCopy(ctx, b.ID, nil, "filter-third-copy")
	b, _ = s.Book(ctx, b.ID)
	s.Checkout(ctx, b.Copies[0].ID, ada, "2026-10-01", "filter-overdue-loan")
	s.Checkout(ctx, b.Copies[1].ID, grace, "2026-10-10", "filter-today-loan")
	s.Checkout(ctx, b.Copies[2].ID, ada, "", "filter-no-date-loan")
	overdue, e := s.ActiveLoans(ctx, 0, "overdue", 1, "2026-10-10")
	if e != nil || overdue.Total != 1 || overdue.Loans[0].Borrower != "Ada Lovelace" {
		t.Fatalf("Overdue %+v %v", overdue, e)
	}
	upcoming, e := s.ActiveLoans(ctx, 0, "upcoming", 1, "2026-10-10")
	if e != nil || upcoming.Total != 1 || upcoming.Loans[0].BookID != b.ID {
		t.Fatalf("Upcoming %+v %v", upcoming, e)
	}
	selected, e := s.ActiveLoans(ctx, ada, "", 1, "2026-10-10")
	if e != nil || selected.Total != 2 {
		t.Fatalf("Borrower %+v %v", selected, e)
	}
	empty, e := s.ActiveLoans(ctx, 0, "", 2, "2026-10-10")
	if e != nil || empty.Total != 3 || len(empty.Loans) != 0 {
		t.Fatalf("Pagination %+v %v", empty, e)
	}
	if _, e = s.ActiveLoans(ctx, 0, "wrong", 1, "2026-10-10"); e == nil {
		t.Fatal("Invalid due filter accepted")
	}
	// Historical loan reads deliberately survive copy archival (future UI must
	// prohibit archiving a copy while it is still checked out).
	if _, e = s.DB.Exec("UPDATE copies SET archived_at=CURRENT_TIMESTAMP WHERE id=?", b.Copies[0].ID); e != nil {
		t.Fatal(e)
	}
	all, e := s.ActiveLoans(ctx, 0, "", 1, "2026-10-10")
	if e != nil || all.Total != 3 {
		t.Fatal("Archived copy lost outstanding loan")
	}
}

func TestCopyArchiveGuardRetryAndHistory(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	book, e := s.Save(ctx, Input{Title: "Archive guard", Authors: []string{}, RequestKey: "archive-test-book"})
	if e != nil {
		t.Fatal(e)
	}
	copy := book.Copies[0]
	borrower, e := s.AddBorrower(ctx, "Ada", "", "archive-borrower")
	if e != nil {
		t.Fatal(e)
	}
	loan, e := s.Checkout(ctx, copy.ID, borrower, "", "archive-checkout")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ArchiveCopy(ctx, copy.ID, copy.Revision, "archive-active-key"); e == nil {
		t.Fatal("Archived checked-out copy")
	}
	if _, e = s.ReturnLoan(ctx, loan, "archive-return-key"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ArchiveCopy(ctx, copy.ID, copy.Revision+1, "archive-stale-key"); e == nil {
		t.Fatal("Archived using stale revision")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ArchiveCopy(ctx, copy.ID, copy.Revision, "archive-copy-retry"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	got, e := s.Book(ctx, book.ID)
	if e != nil || got.Count != 0 || got.Available != 0 {
		t.Fatalf("Archived inventory %+v %v", got, e)
	}
	history, e := s.Loans(ctx, book.ID)
	if e != nil || len(history) != 1 || history[0].ReturnedAt == nil || history[0].CopyID != copy.ID {
		t.Fatalf("History lost %+v %v", history, e)
	}
	if _, e = s.Checkout(ctx, copy.ID, borrower, "", "checkout-archived"); e == nil {
		t.Fatal("Checked out archived copy")
	}
	if _, e = s.ArchiveCopy(ctx, copy.ID, copy.Revision, "archive-new-key"); e == nil {
		t.Fatal("Duplicate archival with new request accepted")
	}
	if _, e = s.ArchiveCopy(ctx, copy.ID, copy.Revision+1, "archive-copy-retry"); e == nil {
		t.Fatal("Changed request payload accepted")
	}
}

func TestArchiveCompetesWithCheckout(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	book, _ := s.Save(ctx, Input{Title: "Archive race", Authors: []string{}, RequestKey: "archive-race-book"})
	borrower, _ := s.AddBorrower(ctx, "Ada", "", "archive-race-person")
	copy := book.Copies[0]
	var won atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, e := s.Checkout(ctx, copy.ID, borrower, "", "race-checkout-key"); e == nil {
			won.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		if _, e := s.ArchiveCopy(ctx, copy.ID, copy.Revision, "race-archive-key"); e == nil {
			won.Add(1)
		}
	}()
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("Expected exactly one mutation to succeed: %d", won.Load())
	}
	var invalid int
	s.DB.QueryRow("SELECT COUNT(*) FROM loans l JOIN copies c ON c.id=l.copy_id WHERE l.returned_at IS NULL AND c.archived_at IS NOT NULL").Scan(&invalid)
	if invalid != 0 {
		t.Fatal("Active loan with archived copy")
	}
}
