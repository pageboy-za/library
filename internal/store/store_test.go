package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestRetryDuplicateAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	in := Input{ISBN: "9780140328721", Title: "Matilda", Authors: []string{"Roald Dahl"}, RequestKey: "first-save-token"}
	b, e := s.Save(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	replayed, e := s.Save(ctx, in)
	if e != nil || replayed.ID != b.ID || replayed.Count != 1 {
		t.Fatalf("retry: %+v %v", replayed, e)
	}
	in.RequestKey = "second-save-token"
	in.Title = "Must not overwrite"
	same, e := s.Save(ctx, in)
	if e != nil || same.ID != b.ID || same.Count != 1 || same.Title != "Matilda" {
		t.Fatalf("duplicate: %+v %v", same, e)
	}
	in.Title = "changed-payload"
	if _, e = s.Save(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("expected conflict: %v", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.AddCopy(ctx, b.ID, nil, "same-copy-request"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	got, e := s.Book(ctx, b.ID)
	if e != nil || got.Count != 2 {
		t.Fatalf("copy retry %+v %v", got, e)
	}
	s.DB.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	got, e = s.Book(ctx, b.ID)
	if e != nil || got.Count != 2 {
		t.Fatalf("persistence %+v %v", got, e)
	}
	if _, e = s.Edit(ctx, b.ID, Input{Title: "updated", Authors: []string{}, Revision: 99}); !errors.Is(e, ErrConflict) {
		t.Fatal("stale edit accepted")
	}
}
func TestLocationsAndLoanConstraint(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ctx := context.Background()
	if e = s.AddShelf(ctx, "Study", "Shelf 1"); e != nil {
		t.Fatal(e)
	}
	sh, _ := s.Shelves(ctx)
	b, e := s.Save(ctx, Input{Title: "No ISBN", Authors: []string{}, ShelfID: &sh[0].ID, RequestKey: "manual-token"})
	if e != nil {
		t.Fatal(e)
	}
	if b.Copies[0].Shelf != "Study / Shelf 1" {
		t.Fatal(b)
	}
	if _, e = s.DB.Exec("INSERT INTO borrowers(name) VALUES('Alex'); INSERT INTO loans(copy_id,borrower_id) VALUES(?,1)", b.Copies[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO loans(copy_id,borrower_id) VALUES(?,1)", b.Copies[0].ID); e == nil {
		t.Fatal("duplicate active loan allowed")
	}
	b, e = s.Book(ctx, b.ID)
	if e != nil || b.Available != 0 || !b.Copies[0].Out {
		t.Fatal(b, e)
	}
	list, total, e := s.List(ctx, "no isbn", sh[0].ID, "", 1)
	if e != nil || total != 1 || len(list) != 1 {
		t.Fatal(list, total, e)
	}
}
