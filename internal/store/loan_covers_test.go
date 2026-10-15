package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestActiveLoanCoversFollowSelectedArtwork(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	b, err := s.Save(ctx, Input{Title: "Cover loan fixture", Authors: []string{}, RequestKey: "cover-loan-book"})
	if err != nil {
		t.Fatal(err)
	}
	borrower, err := s.AddBorrower(ctx, "Fixture borrower", "", "cover-loan-borrower")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Checkout(ctx, b.Copies[0].ID, borrower, "", "cover-loan-checkout"); err != nil {
		t.Fatal(err)
	}
	page, err := s.ActiveLoans(ctx, 0, "", 1, "2026-10-15")
	if err != nil || len(page.Loans) != 1 {
		t.Fatalf("missing-cover loan: %+v %v", page, err)
	}
	if page.Loans[0].CoverURL != "" || page.Loans[0].CoverThumbnailURL != "" {
		t.Fatal("book without artwork has cover URLs")
	}
	first, err := s.SaveCover(ctx, CoverAsset{BookID: b.ID, LocalPath: "first.png", SourceKind: "upload", Checksum: "first-cover"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveCover(ctx, CoverAsset{BookID: b.ID, LocalPath: "second.png", SourceKind: "upload", Checksum: "second-cover"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.ActiveLoans(ctx, borrower, "", 1, "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Loans) != 1 || page.Loans[0].CoverURL != first.URL || page.Loans[0].CoverThumbnailURL != first.ThumbnailURL {
		t.Fatalf("selected cover: %+v", page)
	}
	if err = s.SelectCover(ctx, b.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	page, err = s.ActiveLoans(ctx, 0, "", 1, "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if page.Loans[0].CoverURL != second.URL || page.Loans[0].CoverThumbnailURL != second.ThumbnailURL {
		t.Fatalf("changed cover: %+v", page)
	}
}
