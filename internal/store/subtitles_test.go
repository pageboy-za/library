package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitlesFillOnlyPreservesInventoryAndEdits(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ctx := context.Background()
	b, e := s.Save(ctx, Input{Title: "Fixture", Authors: []string{"A Writer"}, ISBN: "9780140328721", RequestKey: "subtitle-fixture-save"})
	if e != nil {
		t.Fatal(e)
	}
	_, before, err := s.List(ctx, "Fixture A Writer", 0, "", 1)
	if err != nil || before != 1 {
		t.Fatal("Empty subtitle changed title-author phrase search", before, err)
	}
	u := SubtitleUpdate{ID: b.ID, ISBN: b.ISBN, SourceID: b.SourceID, Revision: b.Revision, Subtitle: "A distinct searchable phrase"}
	r, e := s.FillSubtitles(ctx, []SubtitleUpdate{u})
	if e != nil || r.Changed != 1 {
		t.Fatal(r, e)
	}
	got, e := s.Book(ctx, b.ID)
	if e != nil || got.Subtitle != u.Subtitle || len(got.Copies) != 1 || got.Copies[0] != b.Copies[0] {
		t.Fatal(got, e)
	}
	rows, n, e := s.List(ctx, "distinct searchable", 0, "", 1)
	if e != nil || n != 1 || rows[0].ID != b.ID {
		t.Fatal(n, e)
	}
	r, e = s.FillSubtitles(ctx, []SubtitleUpdate{u})
	if e != nil || r.Changed != 0 || r.Skipped != 1 {
		t.Fatal(r, e)
	}
	// An older client omitting subtitle must not erase it.
	got, e = s.Edit(ctx, b.ID, Input{Title: b.Title, ISBN: b.ISBN, Revision: got.Revision})
	if e != nil || got.Subtitle != u.Subtitle {
		t.Fatal(got, e)
	}
	blank := ""
	got, e = s.Edit(ctx, b.ID, Input{Title: b.Title, ISBN: b.ISBN, Revision: got.Revision, Subtitle: &blank})
	if e != nil || got.Subtitle != "" {
		t.Fatal(got, e)
	}
	r, e = s.FillSubtitles(ctx, []SubtitleUpdate{u})
	if e != nil || r.Changed != 0 {
		t.Fatal("Stale migration overwrote manual edit", r, e)
	}
	u.Revision = got.Revision
	u.ISBN = "9780000000002"
	r, e = s.FillSubtitles(ctx, []SubtitleUpdate{u})
	if e != nil || r.Changed != 0 {
		t.Fatal("Edition guard failed", r, e)
	}
	tooLong := strings.Repeat("x", 1001)
	if _, e = s.Edit(ctx, b.ID, Input{Title: b.Title, Revision: got.Revision, Subtitle: &tooLong}); e == nil {
		t.Fatal("Accepted oversized subtitle")
	}
	// Deletion state stays intact when historical books are backfilled.
	got, e = s.SetBookDeleted(ctx, b.ID, got.Revision, true, "delete-subtitle-fixture")
	if e != nil {
		t.Fatal(e)
	}
	u.ISBN = b.ISBN
	u.Revision = got.Revision
	r, e = s.FillSubtitles(ctx, []SubtitleUpdate{u})
	if e != nil || r.Changed != 1 {
		t.Fatal(r, e)
	}
	got, e = s.BookIncludingDeleted(ctx, b.ID)
	if e != nil || got.DeletedAt == nil || got.Count != 0 || got.TotalCopyCount != 1 {
		t.Fatal(got, e)
	}
}
