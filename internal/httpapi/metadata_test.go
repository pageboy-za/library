package httpapi

import (
	"context"
	"fmt"
	"home-library/internal/metadata"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataRefreshOnlyPreparesDraft(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(context.Background(), store.Input{ISBN: "9780140328721", Title: "My local title", Authors: []string{"Local author"}, RequestKey: "refresh-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"ISBN:9780140328721":{"title":"Provider title","authors":[{"name":"Provider author"}],"publishers":[{"name":"Provider publisher"}]}}`)
	}))
	defer provider.Close()
	client := metadata.NewWithCache(t.TempDir())
	client.Base = provider.URL
	h := (&Server{Store: s, Metadata: client}).testHandler(t)
	call := func(method, path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := call("POST", fmt.Sprintf("/api/books/%d/metadata/refresh", b.ID), "{}", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Provider title") {
		t.Fatal(w.Code, w.Body.String())
	}
	unchanged, e := s.Book(context.Background(), b.ID)
	if e != nil || unchanged.Title != b.Title || unchanged.Revision != b.Revision || unchanged.Count != 1 || unchanged.ISBN != b.ISBN {
		t.Fatal("Refresh changed saved edition", unchanged, e)
	}
	// Rescanning existing books returns local data without provider traffic.
	w = call("GET", "/api/isbn/9780140328721", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "My local title") || calls != 1 {
		t.Fatal("Scan called provider or lost saved edits", w.Body.String(), calls)
	}
	w = call("POST", fmt.Sprintf("/api/books/%d/metadata/refresh", b.ID), "{}", "https://elsewhere.test")
	if w.Code != 403 || calls != 1 {
		t.Fatal("Cross origin refresh accepted", w.Code, calls)
	}
	w = call("POST", fmt.Sprintf("/api/books/%d/metadata/refresh", b.ID), `{"unexpected":true}`, "")
	if w.Code != 400 || calls != 1 {
		t.Fatal("Unexpected request accepted")
	}
	manual, e := s.Save(context.Background(), store.Input{Title: "Manual", Authors: []string{}, RequestKey: "manual-refresh-fixture"})
	if e != nil {
		t.Fatal(e)
	}
	w = call("POST", fmt.Sprintf("/api/books/%d/metadata/refresh", manual.ID), "{}", "")
	if w.Code != 400 || calls != 1 {
		t.Fatal("No-ISBN refresh accepted")
	}
}
