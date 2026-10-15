package httpapi

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeletionVisibilitySurvivesFailedStaticBuild(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	b, e := s.Save(context.Background(), store.Input{Title: "Deletion HTTP fixture", ISBN: "9780140328721", RequestKey: "http-delete-save"})
	if e != nil {
		t.Fatal(e)
	}
	pages := &recoveringPages{directory: t.TempDir(), fail: true}
	os.MkdirAll(filepath.Join(pages.directory, "books", "1"), 0700)
	os.WriteFile(filepath.Join(pages.directory, "books", "1", "index.html"), []byte(b.Title), 0600)
	os.WriteFile(filepath.Join(pages.directory, "index.html"), []byte("<h1>Your library</h1><div>Live catalogue shell</div>"), 0600)
	os.MkdirAll(filepath.Join(pages.directory, "settings"), 0700)
	os.WriteFile(filepath.Join(pages.directory, "settings", "index.html"), []byte("Settings recovery shell"), 0600)
	h := (&Server{Store: s, Pages: pages}).Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"revision":1,"expected_copy_count":1,"request_key":"http-delete-fixture"}`
	w := call("POST", "/api/books/1/delete", body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "page_warning") || strings.Contains(w.Body.String(), "private build") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("POST", "/api/books/1/delete", body); w.Code != 200 {
		t.Fatal("Retry failed", w.Body.String())
	}
	for _, path := range []string{"/api/books/1", "/books/1/", "/books/1/index.html"} {
		if w = call("GET", path, ""); w.Code != 404 {
			t.Fatal("Deleted record leaked", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/", "/index.html", "/index.html?from=bookmark"} {
		w = call("GET", path, "")
		if (w.Code != 200 && w.Code != 301) || strings.Contains(w.Body.String(), b.Title) {
			t.Fatal("Stale homepage leaked deleted title", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/settings/", "/settings/index.html"} {
		w = call("GET", path, "")
		if w.Code != 200 && w.Code != 301 {
			t.Fatal("Recovery settings blocked", path, w.Code)
		}
		if strings.Contains(w.Body.String(), b.Title) {
			t.Fatal("Settings leaked deleted title", path)
		}
	}
	w = call("GET", "/api/isbn/"+b.ISBN, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted"`) || strings.Contains(w.Body.String(), `"existing"`) {
		t.Fatal("Scan did not identify recovery", w.Code, w.Body.String())
	}
	w = call("GET", "/api/deleted-books", "")
	var deleted []store.Book
	if e = json.Unmarshal(w.Body.Bytes(), &deleted); e != nil || len(deleted) != 1 || deleted[0].TotalCopyCount != 1 {
		t.Fatal(w.Body.String(), e)
	}
	w = call("POST", "/api/books/1/restore", `{"revision":2,"expected_copy_count":1,"request_key":"http-restore-fixture"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after, e := s.Book(context.Background(), b.ID)
	if e != nil || after.Count != 1 {
		t.Fatal("Restore added or lost copy", after, e)
	}
}
