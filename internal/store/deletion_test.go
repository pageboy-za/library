package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDeleteRestorePreservesCopiesAndLoanHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogue.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.DB.Close() }()
	ctx := context.Background()
	b, e := s.Save(ctx, Input{Title: "Delete fixture", ISBN: "9780140328721", RequestKey: "delete-save-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	b, e = s.AddCopy(ctx, b.ID, nil, "delete-extra-copy")
	if e != nil {
		t.Fatal(e)
	}
	archived := b.Copies[1]
	if _, e = s.ArchiveCopy(ctx, archived.ID, archived.Revision, "archive-before-delete"); e != nil {
		t.Fatal(e)
	}
	borrower, e := s.AddBorrower(ctx, "Fixture borrower", "", "delete-borrower")
	if e != nil {
		t.Fatal(e)
	}
	loan, e := s.Checkout(ctx, b.Copies[0].ID, borrower, "", "delete-checkout")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, b.Revision, true, "delete-active-loan"); e == nil {
		t.Fatal("Deleted an active loan")
	}
	if _, e = s.ReturnLoan(ctx, loan, "delete-return"); e != nil {
		t.Fatal(e)
	}
	deleted, e := s.SetBookDeleted(ctx, b.ID, b.Revision, true, "delete-book-fixture")
	if e != nil {
		t.Fatal(e)
	}
	if deleted.DeletedAt == nil || deleted.Count != 0 || deleted.TotalCopyCount != 2 {
		t.Fatalf("Deletion state %+v", deleted)
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, b.Revision, true, "delete-book-fixture"); e != nil {
		t.Fatalf("Retry %v", e)
	}
	if _, e = s.Book(ctx, b.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("Deleted book visible", e)
	}
	for _, query := range []string{"", "Delete", "9780140328721"} {
		_, total, e := s.List(ctx, query, 0, "", 1)
		if e != nil || total != 0 {
			t.Fatalf("Search leaked deleted book %q %d %v", query, total, e)
		}
	}
	found, e := s.ByISBN(ctx, b.ISBN)
	if e != nil || found.ID != b.ID || found.DeletedAt == nil {
		t.Fatal("Scan must identify deleted record", e)
	}
	if _, e = s.Save(ctx, Input{Title: "Duplicate", ISBN: b.ISBN, RequestKey: "save-deleted-isbn"}); !errors.Is(e, ErrNotFound) {
		t.Fatal("Silent restoration", e)
	}
	if _, e = s.AddCopy(ctx, b.ID, nil, "add-deleted-copy"); !errors.Is(e, ErrNotFound) {
		t.Fatal("Added copy to deleted book", e)
	}
	if _, e = s.Checkout(ctx, b.Copies[0].ID, borrower, "", "checkout-deleted"); e == nil {
		t.Fatal("Lent deleted copy")
	}
	if e = s.MoveCopy(ctx, b.Copies[0].ID, nil, b.Copies[0].Revision+1); e == nil {
		t.Fatal("Moved deleted copy")
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, b.Revision, false, "stale-restore-key"); !errors.Is(e, ErrConflict) {
		t.Fatal("Stale restore accepted", e)
	}
	s.DB.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	// Simulate an explicit Add Copy in an older app, which ignores deleted_at.
	if _, e = s.DB.Exec("INSERT INTO copies(book_id,label) VALUES(?,'Older version copy')", b.ID); e != nil {
		t.Fatal(e)
	}
	var laterCopy int64
	if e = s.DB.QueryRow("SELECT id FROM copies WHERE label='Older version copy'").Scan(&laterCopy); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO loans(copy_id,borrower_id) VALUES(?,?)", laterCopy, borrower); e != nil {
		t.Fatal(e)
	}
	if e = s.AddShelf(ctx, "Later location", "Shelf"); e != nil {
		t.Fatal(e)
	}
	shelves, e := s.Shelves(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE copies SET shelf_id=?,revision=revision+1 WHERE id=?", shelves[0].ID, b.Copies[0].ID); e != nil {
		t.Fatal(e)
	}
	restored, e := s.SetBookDeleted(ctx, b.ID, deleted.Revision, false, "restore-book-fixture")
	if e != nil {
		t.Fatal(e)
	}
	if restored.DeletedAt != nil || restored.Count != 2 || restored.TotalCopyCount != 3 {
		t.Fatalf("Restore resurrected old archive or lost newer copy %+v", restored)
	}
	if restored.Copies[0].ID != b.Copies[0].ID || restored.Copies[0].ShelfID == nil || *restored.Copies[0].ShelfID != shelves[0].ID {
		t.Fatal("Original identity lost")
	}
	history, e := s.Loans(ctx, b.ID)
	if e != nil || len(history) != 2 || history[0].ReturnedAt != nil || history[1].ReturnedAt == nil {
		t.Fatalf("History lost %+v %v", history, e)
	}
	_, total, e := s.List(ctx, "Delete", 0, "", 1)
	if e != nil || total != 1 {
		t.Fatal("Restore not searchable", e)
	}
	if _, e = s.ReturnLoan(ctx, history[0].ID, "return-later-copy"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, restored.Revision, true, "delete-second-cycle"); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteRejectsChangedCopyCount(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ctx := context.Background()
	b, e := s.Save(ctx, Input{Title: "Count fixture", RequestKey: "count-fixture-save"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AddCopy(ctx, b.ID, nil, "count-fixture-copy"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, b.Revision, true, "count-fixture-delete", b.TotalCopyCount); !errors.Is(e, ErrConflict) {
		t.Fatal("Stale confirmation deleted a newer copy", e)
	}
	after, e := s.Book(ctx, b.ID)
	if e != nil || after.Count != 2 {
		t.Fatal("Rejected delete changed state", after, e)
	}
}
