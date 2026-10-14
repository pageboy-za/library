package catalogue

import (
	"context"
	"home-library/internal/store"
	"path/filepath"
	"testing"
)

func TestExplainedOwnedKeywordSearch(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = store.EnsureDiscoverySchema(s); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	a, e := s.Save(ctx, store.Input{Title: "Clear writing", Authors: []string{"An Author"}, RequestKey: "discovery-a"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Save(ctx, store.Input{Title: "Other book", Authors: []string{}, RequestKey: "discovery-b"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDiscovery(ctx, b.ID, store.Discovery{Tags: []string{"communication"}, Notes: "Practical writing advice"}); e != nil {
		t.Fatal(e)
	}
	svc := DiscoveryService{s}
	r, e := svc.Search(ctx, "books about communication")
	if e != nil || len(r) != 1 || r[0].Book.ID != b.ID || r[0].Reasons[0] != "Matches your tag: communication" {
		t.Fatalf("matches %v %v", r, e)
	}
	r, e = svc.Search(ctx, "writing")
	if e != nil || len(r) != 2 || r[0].Book.ID != a.ID || r[0].Book.Count != 1 {
		t.Fatalf("ranking %v %v", r, e)
	}
	r, e = svc.Search(ctx, "% _ ' OR 1=1")
	if e != nil || len(r) != 0 {
		t.Fatalf("wildcards are not SQL: %v %v", r, e)
	}
	if _, e = s.DB.ExecContext(ctx, "UPDATE copies SET archived_at=CURRENT_TIMESTAMP WHERE book_id=?", b.ID); e != nil {
		t.Fatal(e)
	}
	r, e = svc.Search(ctx, "communication")
	if e != nil || len(r) != 0 {
		t.Fatalf("archived copies %v %v", r, e)
	}
}
