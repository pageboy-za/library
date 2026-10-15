package backup

import (
	"context"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestBibliographySurvivesBackupRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, err := store.Open(filepath.Join(source, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	b, err := s.Save(ctx, store.Input{Title: "Bibliography fixture", PublicationDate: "2001", RequestKey: "bibliography-backup"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDiscovery(ctx, b.ID, store.Discovery{Subjects: []string{"Science"}, Notes: "Private fixture note"})
	if err != nil {
		t.Fatal(err)
	}
	year := 1970
	_, _, err = s.SaveEnrichment(ctx, b.ID, d, store.Bibliography{FirstPublishedYear: &year, SourceURL: "https://openlibrary.org/works/OL1W"}, b.Revision, false, true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err = Create(ctx, source, snapshot); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if err = Restore(ctx, snapshot, restored); err != nil {
		t.Fatal(err)
	}
	r, err := store.Open(filepath.Join(restored, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.DB.Close()
	got, err := r.Book(ctx, b.ID)
	if err != nil || got.FirstPublishedYear == nil || *got.FirstPublishedYear != year || got.FirstPublishedYearSourceURL != "https://openlibrary.org/works/OL1W" || got.PublicationDate != "2001" || got.Count != 1 || len(got.Subjects) != 1 {
		t.Fatal(got, err)
	}
	private, err := r.Discovery(ctx, b.ID)
	if err != nil || private.Notes != "Private fixture note" {
		t.Fatal(private, err)
	}
}
