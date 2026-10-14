package store

import (
	"context"
	"errors"
	"home-library/internal/library"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditProviderReviewAndChangedISBNClearsProvenance(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	original, e := s.Save(ctx, Input{ISBN: "9780140328721", Title: "Original title", Authors: []string{"A"}, Publisher: "First", PublicationDate: "1988", SourceURL: "https://openlibrary.org/books/OL111M", SourceID: "OL111M", RequestKey: "edit-provenance-book"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO cover_assets(id,book_id,local_path,source_kind,checksum) VALUES(1,?,'covers/local.jpg','upload','local-checksum')", original.ID); e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE books SET selected_cover_asset_id=1 WHERE id=?", original.ID)
	review, e := s.Edit(ctx, original.ID, Input{ISBN: original.ISBN, Title: "  Reviewed title  ", Authors: []string{"A"}, Publisher: "New publisher", PublicationDate: "2000", SourceURL: "https://openlibrary.org/books/OL222M", SourceID: "OL222M", Revision: original.Revision})
	if e != nil || review.SourceURL != "https://openlibrary.org/books/OL222M" || review.SourceID != "OL222M" || review.Title != "Reviewed title" || review.PublicationDate != "2000" {
		t.Fatalf("Review failed %+v %v", review, e)
	}
	omitted, e := s.Edit(ctx, review.ID, Input{ISBN: review.ISBN, Title: review.Title, Authors: review.Authors, Publisher: review.Publisher, PublicationDate: "2000", Revision: review.Revision})
	if e != nil || omitted.SourceURL != review.SourceURL || omitted.SourceID != review.SourceID {
		t.Fatalf("Omitted source lost %+v %v", omitted, e)
	}
	canonical, e := library.ISBN("1-85702-889-9")
	if e != nil {
		t.Fatal(e)
	}
	changed, e := s.Edit(ctx, omitted.ID, Input{ISBN: canonical, Title: "Changed edition", Authors: omitted.Authors, Publisher: omitted.Publisher, PublicationDate: "2000", SourceURL: review.SourceURL, SourceID: review.SourceID, Revision: omitted.Revision})
	if e != nil || changed.ISBN != "9781857028898" || changed.SourceURL != "" || changed.SourceID != "" || changed.PublicationDate != "2000" {
		t.Fatalf("Stale provenance retained %+v %v", changed, e)
	}
	if changed.Count != 1 || changed.Copies[0].ID != original.Copies[0].ID {
		t.Fatal("Copies changed")
	}
	var selected int
	s.DB.QueryRow("SELECT selected_cover_asset_id FROM books WHERE id=?", changed.ID).Scan(&selected)
	if selected != 1 {
		t.Fatal("Local cover selection lost")
	}
}
func TestEditLimitsStaleAndUniqueConflictAreAtomic(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	first, e := s.Save(ctx, Input{ISBN: "9780140328721", Title: "Keep me", Authors: []string{}, PublicationDate: "1988", SourceURL: "https://openlibrary.org/books/OL111M", SourceID: "OL111M", RequestKey: "edit-first-book"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Save(ctx, Input{ISBN: "9781857028898", Title: "Other", Authors: []string{}, RequestKey: "edit-other-book"})
	if e != nil {
		t.Fatal(e)
	}
	in := Input{ISBN: second.ISBN, Title: "Must not persist", PublicationDate: "2099", SourceURL: "new-url", SourceID: "new-id", Revision: first.Revision}
	if _, e = s.Edit(ctx, first.ID, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("Expected unique conflict, got %v", e)
	}
	in.ISBN = first.ISBN
	in.Revision = first.Revision + 1
	if _, e = s.Edit(ctx, first.ID, in); !errors.Is(e, ErrConflict) {
		t.Fatal("Stale edit accepted")
	}
	for _, title := range []string{"  ", strings.Repeat("x", 501), strings.Repeat("é", 251)} {
		in.Title = title
		in.Revision = first.Revision
		if _, e = s.Edit(ctx, first.ID, in); e == nil {
			t.Fatal("Invalid or oversized title accepted")
		}
	}
	got, e := s.Book(ctx, first.ID)
	if e != nil || got.Title != first.Title || got.ISBN != first.ISBN || got.SourceURL != first.SourceURL || got.SourceID != first.SourceID || got.PublicationDate != "1988" || got.Count != 1 || got.Revision != first.Revision {
		t.Fatalf("Failed edit changed record %+v %v", got, e)
	}
	in.Title = strings.Repeat("x", 500)
	in.ISBN = first.ISBN
	in.Revision = first.Revision
	in.SourceURL = ""
	in.SourceID = ""
	in.PublicationDate = "1988"
	success, e := s.Edit(ctx, first.ID, in)
	if e != nil || len(success.Title) != 500 || len(success.Authors) != 0 || success.SourceID != first.SourceID {
		t.Fatalf("Boundary edit %+v %v", success, e)
	}
}
