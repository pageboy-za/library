package backup

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDerivedSearchExportAndRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	s, e := store.Open(filepath.Join(source, "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(ctx, store.Input{Title: "UniqueIndexedTitle", Authors: []string{"Search Author"}, RequestKey: "indexed-backup"})
	if e != nil {
		t.Fatal(e)
	}
	var indexed int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM book_search WHERE rowid=?", b.ID).Scan(&indexed); e != nil || indexed != 1 {
		t.Fatal("Search index missing", indexed, e)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if e = Create(ctx, source, snapshot); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(snapshot, "catalogue.json"))
	if e != nil {
		t.Fatal(e)
	}
	var exported Export
	if e = json.Unmarshal(raw, &exported); e != nil {
		t.Fatal(e)
	}
	for name := range exported.Tables {
		if name == "book_search" || strings.HasPrefix(name, "book_search_") {
			t.Fatal("Derived index leaked into portable user export", name)
		}
	}
	if len(exported.Tables["books"]) != 1 {
		t.Fatal("User book missing from export")
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
	matches, total, e := r.List(ctx, "UniqueIndexed", 0, "", 1)
	if e != nil || total != 1 || len(matches) != 1 || matches[0].ID != b.ID {
		t.Fatal("Restored indexed search failed", matches, total, e)
	}
}
