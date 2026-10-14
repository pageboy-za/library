package bookpages

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestDeletedBookExcludedFromStaticSnapshot(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(ctx, store.Input{Title: "Deleted snapshot fixture", RequestKey: "snapshot-delete-save"})
	if e != nil {
		t.Fatal(e)
	}
	g, e := New(s, t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	deleted, e := s.SetBookDeleted(ctx, b.ID, b.Revision, true, "snapshot-delete-book")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := g.snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var books []Metadata
	if e = json.Unmarshal(raw, &books); e != nil || len(books) != 0 {
		t.Fatal("Deleted title leaked into generation", string(raw), e)
	}
	if _, e = s.SetBookDeleted(ctx, b.ID, deleted.Revision, false, "snapshot-restore-book"); e != nil {
		t.Fatal(e)
	}
	raw, e = g.snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &books); e != nil || len(books) != 1 || books[0].ID != b.ID {
		t.Fatal("Restored title missing", string(raw), e)
	}
}
