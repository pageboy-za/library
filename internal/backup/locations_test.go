package backup

import (
	"context"
	"errors"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestLocationTombstonesSurviveRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = s.AddShelf(ctx, "Study", "One"); e != nil {
		t.Fatal(e)
	}
	shelves, e := s.Shelves(ctx)
	if e != nil {
		t.Fatal(e)
	}
	old := shelves[0]
	if e = s.RemoveShelf(ctx, old.ID, old.Name, old.Revision, "remove-before-backup"); e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if e = Create(ctx, source, snapshot); e != nil {
		t.Fatal(e)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if e = Restore(ctx, snapshot, restored); e != nil {
		t.Fatal(e)
	}
	r, e := store.Open(filepath.Join(restored, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer r.DB.Close()
	if e = r.AddShelf(ctx, "Study", "One"); e != nil {
		t.Fatal(e)
	}
	shelves, e = r.Shelves(ctx)
	if e != nil {
		t.Fatal(e)
	}
	current := shelves[0]
	if current.ID != old.ID || current.Revision <= old.Revision {
		t.Fatalf("Tombstone lost after restore: old %+v current %+v", old, current)
	}
	if e = r.RemoveShelf(ctx, old.ID, old.Name, old.Revision, "stale-after-restore"); !errors.Is(e, store.ErrConflict) {
		t.Fatal("Restored library accepted stale location", e)
	}
}
