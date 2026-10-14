package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDiscoveryRevisionsAndPrivateFields(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	if e = EnsureDiscoverySchema(s); e != nil {
		t.Fatal(e)
	}
	if e = EnsureDiscoverySchema(s); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	b, e := s.Save(ctx, Input{Title: "Thinking", Authors: []string{}, RequestKey: "book-discovery"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.Discovery(ctx, b.ID)
	if e != nil || d.Revision != 0 {
		t.Fatalf("empty %v %v", d, e)
	}
	d.Tags = []string{" ideas ", "Ideas", ""}
	d.Notes = "Private reading note"
	saved, e := s.SaveDiscovery(ctx, b.ID, d)
	if e != nil || saved.Revision != 1 || len(saved.Tags) != 1 || saved.Notes != d.Notes {
		t.Fatalf("save %v %v", saved, e)
	}
	if _, e = s.SaveDiscovery(ctx, b.ID, d); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale create %v", e)
	}
	saved.Description = "Something new"
	updated, e := s.SaveDiscovery(ctx, b.ID, saved)
	if e != nil || updated.Revision != 2 {
		t.Fatalf("edit %v %v", updated, e)
	}
	if _, e = s.SaveDiscovery(ctx, b.ID, saved); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale edit %v", e)
	}
	if _, e = s.Discovery(ctx, 999); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestDiscoveryProviderSaveGuardsBothRevisions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	book, err := s.Save(ctx, Input{Title: "Edition one", ISBN: "9781857028898", RequestKey: "provider-revisions"})
	if err != nil {
		t.Fatal(err)
	}
	// An unchanged book accepts both first insert and subsequent enrichment.
	saved, err := s.SaveDiscoveryForBookRevision(ctx, book.ID, Discovery{Notes: "Private note", Tags: []string{"private"}}, book.Revision)
	if err != nil {
		t.Fatal(err)
	}
	saved.Description = "Reviewed description"
	saved, err = s.SaveDiscoveryForBookRevision(ctx, book.ID, saved, book.Revision)
	if err != nil {
		t.Fatal(err)
	}
	prior := saved
	edited, err := s.Edit(ctx, book.ID, Input{Title: "Edition two", ISBN: "9780140328721", Revision: book.Revision})
	if err != nil {
		t.Fatal(err)
	}
	saved.Subjects = []string{"Old edition subject"}
	if _, err = s.SaveDiscoveryForBookRevision(ctx, book.ID, saved, book.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale book revision accepted", err)
	}
	actual, err := s.Discovery(ctx, book.ID)
	if err != nil || actual.Revision != prior.Revision || len(actual.Subjects) != 0 || actual.Description != prior.Description || actual.Notes != prior.Notes || actual.Tags[0] != "private" {
		t.Fatal("book conflict changed discovery", actual, err)
	}
	// The latest book revision must not bypass a stale discovery revision.
	newer := actual
	newer.Notes = "Later private note"
	newer, err = s.SaveDiscovery(ctx, book.ID, newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDiscoveryForBookRevision(ctx, book.ID, saved, edited.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale discovery revision accepted", err)
	}
	actual, err = s.Discovery(ctx, book.ID)
	if err != nil || actual.Revision != newer.Revision || actual.Notes != newer.Notes || len(actual.Subjects) != 0 {
		t.Fatal("discovery conflict changed notes", actual, err)
	}
	// A stale book revision cannot initialize a previously absent discovery row.
	empty, err := s.Save(ctx, Input{Title: "Empty", RequestKey: "provider-empty"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Edit(ctx, empty.ID, Input{Title: "Changed", Revision: empty.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDiscoveryForBookRevision(ctx, empty.ID, Discovery{Description: "Stale"}, empty.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale insert accepted", err)
	}
	actual, err = s.Discovery(ctx, empty.ID)
	if err != nil || actual.Revision != 0 || actual.Description != "" {
		t.Fatal("stale insert created data", actual, err)
	}
}
