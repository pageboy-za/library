package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDefaultShelfPersistsRejectsStaleChangesAndClearsOnDeletion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AddShelf(ctx, "Fixture", "One"); e != nil {
		t.Fatal(e)
	}
	shelves, e := s.Shelves(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id := shelves[0].ID
	p, e := s.Preferences(ctx)
	if e != nil {
		t.Fatal(e)
	}
	initial := p.Revision
	p, e = s.SavePreferences(ctx, &id, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SavePreferences(ctx, nil, initial); !errors.Is(e, ErrConflict) {
		t.Fatal("stale settings accepted", e)
	}
	invalid := int64(99999)
	if _, e = s.SavePreferences(ctx, &invalid, p.Revision); e == nil {
		t.Fatal("missing shelf accepted")
	}
	s.DB.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	p, e = s.Preferences(ctx)
	if e != nil || p.DefaultShelfID == nil || *p.DefaultShelfID != id {
		t.Fatal("default shelf lost", e)
	}
	previous := p.Revision
	if _, e = s.DB.ExecContext(ctx, "DELETE FROM shelves WHERE id=?", id); e != nil {
		t.Fatal(e)
	}
	p, e = s.Preferences(ctx)
	if e != nil || p.DefaultShelfID != nil || p.Revision <= previous {
		t.Fatal("deleted shelf default not safely cleared", e)
	}
}
