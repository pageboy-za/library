package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLocationRenameRetryGuardsAndHistory(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	s.AddShelf(ctx, "Study", "One")
	s.AddShelf(ctx, "Study", "Two")
	s.AddShelf(ctx, "Other", "One")
	shelves, _ := s.Shelves(ctx)
	var one, two Shelf
	for _, sh := range shelves {
		if sh.Bookcase == "Study" && sh.Name == "One" {
			one = sh
		}
		if sh.Bookcase == "Study" && sh.Name == "Two" {
			two = sh
		}
	}
	book, e := s.Save(ctx, Input{Title: "Location book", Authors: []string{}, ShelfID: &one.ID, RequestKey: "locations-book"})
	if e != nil {
		t.Fatal(e)
	}
	person, _ := s.AddBorrower(ctx, "Ada", "", "locations-person")
	loan, _ := s.Checkout(ctx, book.Copies[0].ID, person, "", "locations-loan")
	if e = s.RenameShelf(ctx, one.ID, "Top", "One", 1, "rename-shelf-key"); e != nil {
		t.Fatal(e)
	}
	if e = s.RenameShelf(ctx, one.ID, "Top", "One", 1, "rename-shelf-key"); e != nil {
		t.Fatal("Retry failed")
	}
	if e = s.RenameBookcase(ctx, one.BookcaseID, "Office", "Study", 1, "rename-case-key"); e != nil {
		t.Fatal(e)
	}
	got, e := s.Book(ctx, book.ID)
	if e != nil || got.Copies[0].Shelf != "Office / Top" || got.Copies[0].ShelfID == nil || *got.Copies[0].ShelfID != one.ID {
		t.Fatalf("Location lost %+v %v", got, e)
	}
	if e = s.RenameShelf(ctx, one.ID, "Unexpected", "One", 1, "stale-shelf-key"); e == nil {
		t.Fatal("Stale shelf rename accepted")
	}
	if e = s.RenameShelf(ctx, one.ID, "Two", "Top", 2, "duplicate-shelf-key"); e == nil {
		t.Fatal("Duplicate shelf name accepted")
	}
	if e = s.RenameBookcase(ctx, one.BookcaseID, "Other", "Office", 2, "duplicate-case-key"); e == nil {
		t.Fatal("Duplicate bookcase accepted")
	}
	if e = s.RemoveShelf(ctx, one.ID, "Top", 2, "remove-live-shelf"); e == nil {
		t.Fatal("Removed live reference")
	}
	if e = s.RemoveBookcase(ctx, one.BookcaseID, "Office", 2, "remove-live-case"); e == nil {
		t.Fatal("Removed referenced case")
	}
	s.ReturnLoan(ctx, loan, "locations-return")
	got, _ = s.Book(ctx, book.ID)
	s.ArchiveCopy(ctx, got.Copies[0].ID, got.Copies[0].Revision, "locations-archive")
	if e = s.RemoveShelf(ctx, one.ID, "Top", 2, "remove-archived-shelf"); e == nil {
		t.Fatal("Removed archived reference")
	}
	history, _ := s.Loans(ctx, book.ID)
	if len(history) != 1 || history[0].ReturnedAt == nil {
		t.Fatal("History lost")
	}
	if e = s.RemoveShelf(ctx, two.ID, "Two", two.Revision, "remove-empty-shelf"); e != nil {
		t.Fatal(e)
	}
	if e = s.RemoveShelf(ctx, two.ID, "Two", two.Revision, "remove-empty-shelf"); e != nil {
		t.Fatal("Delete retry failed")
	}
	cases, _ := s.Bookcases(ctx)
	for _, bc := range cases {
		if bc.Name == "Other" {
			if e = s.RemoveBookcase(ctx, bc.ID, bc.Name, bc.Revision, "remove-empty-case"); e != nil {
				t.Fatal(e)
			}
			if e = s.RemoveBookcase(ctx, bc.ID, bc.Name, bc.Revision, "remove-empty-case"); e != nil {
				t.Fatal("Case removal retry failed")
			}
		}
	}
	remaining, _ := s.Shelves(ctx)
	if len(remaining) != 1 || remaining[0].ID != one.ID {
		t.Fatalf("Wrong remaining shelves %+v", remaining)
	}
}
func TestLocationConcurrentRenames(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	s.AddShelf(ctx, "Study", "One")
	sh, _ := s.Shelves(ctx)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, name := range []string{"Left", "Right"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if e := s.RenameShelf(ctx, sh[0].ID, n, "One", sh[0].Revision, "rename-key-"+n); e == nil {
				successes.Add(1)
			}
		}(name)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("Rename collision: %d", successes.Load())
	}
}
func TestShelfRemovalCompetesWithCopyAssignment(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	s.AddShelf(ctx, "Study", "One")
	sh, _ := s.Shelves(ctx)
	b, _ := s.Save(ctx, Input{Title: "Moving", Authors: []string{}, RequestKey: "moving-book-key"})
	var success atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if e := s.RemoveShelf(ctx, sh[0].ID, "One", sh[0].Revision, "remove-race-key"); e == nil {
			success.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		if e := s.MoveCopy(ctx, b.Copies[0].ID, &sh[0].ID, b.Copies[0].Revision); e == nil {
			success.Add(1)
		}
	}()
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("Referential race: %d", success.Load())
	}
	var invalid int
	s.DB.QueryRow("SELECT COUNT(*) FROM copies c LEFT JOIN shelves s ON s.id=c.shelf_id WHERE c.shelf_id IS NOT NULL AND s.id IS NULL").Scan(&invalid)
	if invalid != 0 {
		t.Fatal("Dangling shelf reference")
	}
}

func TestLocationVersionsBlockABAAndIDReuse(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s.AddShelf(ctx, "Study", "One")
	shelves, _ := s.Shelves(ctx)
	original := shelves[0]
	cases, _ := s.Bookcases(ctx)
	originalCase := cases[0]
	if e = s.RenameShelf(ctx, original.ID, "Top", "One", original.Revision, "aba-rename-top"); e != nil {
		t.Fatal(e)
	}
	if e = s.RenameShelf(ctx, original.ID, "One", "Top", original.Revision+1, "aba-rename-back"); e != nil {
		t.Fatal(e)
	}
	if e = s.RemoveShelf(ctx, original.ID, "One", original.Revision, "aba-stale-removal"); e == nil {
		t.Fatal("ABA stale removal accepted")
	}
	shelves, _ = s.Shelves(ctx)
	current := shelves[0]
	if e = s.RemoveShelf(ctx, current.ID, current.Name, current.Revision, "valid-shelf-remove"); e != nil {
		t.Fatal(e)
	}
	if e = s.AddShelf(ctx, "Study", "One"); e != nil {
		t.Fatal(e)
	}
	shelves, _ = s.Shelves(ctx)
	reused := shelves[0]
	if reused.ID != original.ID || reused.Revision <= current.Revision {
		t.Fatalf("Expected reused ID with higher revision: original %+v new %+v", original, reused)
	}
	if e = s.RenameShelf(ctx, reused.ID, "Bad", "One", original.Revision, "reused-stale-rename"); e == nil {
		t.Fatal("Stale rename targeted reused ID")
	}
	if e = s.RemoveShelf(ctx, current.ID, current.Name, current.Revision, "valid-shelf-remove"); e != nil {
		t.Fatal("Successful old request did not replay")
	}
	shelves, _ = s.Shelves(ctx)
	if len(shelves) != 1 {
		t.Fatal("Replay deleted replacement")
	}
	if e = s.RemoveBookcase(ctx, originalCase.ID, "Study", originalCase.Revision, "valid-case-remove"); e != nil {
		t.Fatal(e)
	}
	if e = s.AddShelf(ctx, "Study", "One"); e != nil {
		t.Fatal(e)
	}
	cases, _ = s.Bookcases(ctx)
	if cases[0].ID != originalCase.ID || cases[0].Revision <= originalCase.Revision {
		t.Fatalf("Case reused revision %+v", cases)
	}
	if e = s.RemoveBookcase(ctx, originalCase.ID, "Study", originalCase.Revision, "case-stale-removal"); e == nil {
		t.Fatal("Old case revision removed replacement")
	}
	// Ordinary stopped-app database copies preserve both live revisions and deleted
	// location tombstones; opening the restored copy must never reset either.
	if e = s.AddShelf(ctx, "Study", "Temporary"); e != nil {
		t.Fatal(e)
	}
	extraShelves, _ := s.Shelves(ctx)
	var tombstoneID int64
	for _, sh := range extraShelves {
		if sh.Name == "Temporary" {
			tombstoneID = sh.ID
			if e = s.RemoveShelf(ctx, sh.ID, sh.Name, sh.Revision, "remove-tombstone-fixture"); e != nil {
				t.Fatal(e)
			}
		}
	}
	var tombstoneRevision int
	if e = s.DB.QueryRow("SELECT revision FROM location_versions WHERE kind='shelf' AND id=?", tombstoneID).Scan(&tombstoneRevision); e != nil {
		t.Fatal(e)
	}
	beforeShelves, _ := s.Shelves(ctx)
	beforeCases, _ := s.Bookcases(ctx)
	if e = s.DB.Close(); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.sqlite")
	if e = os.WriteFile(restoredPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	restored, e := Open(restoredPath)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.DB.Close()
	afterShelves, _ := restored.Shelves(ctx)
	afterCases, _ := restored.Bookcases(ctx)
	var restoredTombstone int
	if e = restored.DB.QueryRow("SELECT revision FROM location_versions WHERE kind='shelf' AND id=?", tombstoneID).Scan(&restoredTombstone); e != nil || restoredTombstone != tombstoneRevision {
		t.Fatalf("Restore lost removed-location tombstone: %d %v", restoredTombstone, e)
	}
	if afterShelves[0].Revision != beforeShelves[0].Revision || afterCases[0].Revision != beforeCases[0].Revision {
		t.Fatal("Restore reset revisions")
	}
	if e = restored.RemoveShelf(ctx, original.ID, "One", original.Revision, "restored-stale-key"); e == nil {
		t.Fatal("Restore lost stale guard")
	}
}
func TestExistingLocationsReceiveVersionsWithoutChangingIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(schema); e != nil {
		t.Fatal(e)
	}
	db.Exec("PRAGMA user_version=1")
	db.Exec("INSERT INTO bookcases(id,name) VALUES(5,'Study')")
	db.Exec("INSERT INTO shelves(id,bookcase_id,name) VALUES(8,5,'One')")
	db.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	sh, e := s.Shelves(context.Background())
	if e != nil || len(sh) != 1 || sh[0].ID != 8 || sh[0].Revision != 1 {
		t.Fatalf("Old locations %+v %v", sh, e)
	}
	if e = EnsureLocationSchema(s); e != nil {
		t.Fatal(e)
	}
	sh, _ = s.Shelves(context.Background())
	if sh[0].Revision != 1 {
		t.Fatal("Migration repeated revision")
	}
}

func TestAddLocationValidationCaseAndRevision(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.AddShelf(ctx, " Study ", " One "); e != nil {
		t.Fatal(e)
	}
	initial, _ := s.Shelves(ctx)
	if len(initial) != 1 || initial[0].Name != "One" || initial[0].Bookcase != "Study" || initial[0].Revision != 1 {
		t.Fatalf("Trim labels %+v", initial)
	}
	if e = s.AddShelf(ctx, "study", "one"); e == nil {
		t.Fatal("Case-insensitive duplicate allowed")
	}
	cases, _ := s.Bookcases(ctx)
	if len(cases) != 1 || cases[0].Revision != 1 {
		t.Fatalf("Duplicate created or changed case %+v", cases)
	}
	if e = s.AddShelf(ctx, "STUDY", "Two"); e != nil {
		t.Fatal(e)
	}
	cases, _ = s.Bookcases(ctx)
	if len(cases) != 1 || cases[0].Name != "Study" || cases[0].Revision != 1 {
		t.Fatal("Existing case label/version changed")
	}
	if e = s.AddShelf(ctx, string(make([]byte, 201)), "Three"); e == nil {
		t.Fatal("Oversize bookcase accepted")
	}
	if e = s.AddShelf(ctx, "Study", string(make([]byte, 201))); e == nil {
		t.Fatal("Oversize shelf accepted")
	}
	if e = s.AddShelf(ctx, " ", "Three"); e == nil {
		t.Fatal("Blank bookcase accepted")
	}
	if e = s.AddShelf(ctx, "Study", " "); e == nil {
		t.Fatal("Blank shelf accepted")
	}
}
