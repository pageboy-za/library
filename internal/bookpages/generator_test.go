package bookpages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPublishMetadataOnlyAndRecover(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(ctx, store.Input{Title: "Before <script>", Authors: []string{"Author"}, RequestKey: "first-save"})
	if e != nil {
		t.Fatal(e)
	}
	g, e := New(s, t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	fail := false
	incomplete := false
	g.Run = func(_ context.Context, snapshot, out string) error {
		calls++
		data, e := os.ReadFile(snapshot)
		if e != nil {
			return e
		}
		if strings.Contains(string(data), "copies") || strings.Contains(string(data), "available") || strings.Contains(string(data), "private thought") {
			t.Fatal("live inventory leaked into static snapshot")
		}
		if fail {
			return errors.New("build failed")
		}
		if e = os.MkdirAll(out, 0700); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(out, "index.html"), []byte("home"), 0600); e != nil {
			return e
		}
		if incomplete {
			return nil
		}
		var books []Metadata
		if e = json.Unmarshal(data, &books); e != nil {
			return e
		}
		for _, b := range books {
			dir := filepath.Join(out, "books", fmt.Sprint(b.ID))
			if e = os.MkdirAll(dir, 0700); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(dir, "index.html"), []byte(b.Title), 0600); e != nil {
				return e
			}
		}
		return nil
	}
	if e = g.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	old, _ := os.Readlink(g.Directory())
	if _, e = s.AddCopy(ctx, b.ID, nil, "copy-token"); e != nil {
		t.Fatal(e)
	}
	if e = g.Ensure(ctx); e != nil || calls != 1 {
		t.Fatalf("copy rebuilt pages %d %v", calls, e)
	}
	b, e = s.Edit(ctx, b.ID, store.Input{Title: "After", Authors: []string{"Author"}, Revision: b.Revision})
	if e != nil {
		t.Fatal(e)
	}
	fail = true
	if e = g.Ensure(ctx); e == nil {
		t.Fatal("build failure accepted")
	}
	current, _ := os.Readlink(g.Directory())
	if current != old {
		t.Fatal("failed build replaced live output")
	}
	fail = false
	incomplete = true
	if e = g.Ensure(ctx); e == nil {
		t.Fatal("missing per-book output published")
	}
	incomplete = false
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := g.Ensure(ctx); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls != 4 {
		t.Fatalf("concurrent builds were not coalesced: %d", calls)
	}
	d, e := s.SaveDiscovery(ctx, b.ID, store.Discovery{Tags: []string{"private thought"}, Notes: "private thought"})
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Ensure(ctx); e != nil || calls != 4 {
		t.Fatalf("private notes rebuilt static pages: %d %v", calls, e)
	}
	d.Description = "Public description"
	d.Subjects = []string{"Communication"}
	if _, e = s.SaveDiscovery(ctx, b.ID, d); e != nil {
		t.Fatal(e)
	}
	if e = g.Ensure(ctx); e != nil || calls != 5 {
		t.Fatalf("public description did not rebuild: %d %v", calls, e)
	}
	page, e := os.ReadFile(filepath.Join(g.Directory(), "books", fmt.Sprint(b.ID), "index.html"))
	if e != nil || string(page) != "After" {
		t.Fatalf("new metadata missing %s %v", page, e)
	}
}
