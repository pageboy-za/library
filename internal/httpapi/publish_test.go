package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"home-library/internal/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recoveringPages struct {
	directory string
	fail      bool
}

func (p *recoveringPages) Directory() string { return p.directory }
func (p *recoveringPages) Ensure(context.Context) error {
	if p.fail {
		return errors.New("private build failure detail")
	}
	return nil
}
func TestFailedPublicationPreservesEditionAndRetryDoesNotAddCopy(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	pages := &recoveringPages{t.TempDir(), true}
	h := (&Server{Store: s, Pages: pages}).Handler()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"title":"Saved despite failed build","authors":["Author"],"request_key":"failed-build-save"}`
	w := call("POST", "/api/books", body)
	if w.Code != 202 || !strings.Contains(w.Body.String(), "page_warning") || strings.Contains(w.Body.String(), "private build failure") {
		t.Fatal(w.Code, w.Body.String())
	}
	var b store.Book
	if e = json.Unmarshal(w.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	before, e := s.Book(ctx, b.ID)
	if e != nil || before.Count != 1 || before.Title != "Saved despite failed build" {
		t.Fatal(before, e)
	}
	w = call("GET", "/books/1/", "")
	if w.Code != 503 {
		t.Fatal("Failed build served stale book", w.Code)
	}
	dir := filepath.Join(pages.directory, "books", "1")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("Recovered static book"), 0600)
	pages.fail = false
	w = call("POST", "/api/books", body)
	if w.Code != 200 {
		t.Fatal("Safe retry failed", w.Code, w.Body.String())
	}
	after, e := s.Book(ctx, b.ID)
	if e != nil || after.Count != 1 || after.ID != before.ID {
		t.Fatal("Retry changed inventory", after, e)
	}
	w = call("GET", "/books/1/", "")
	if w.Code != 200 || w.Body.String() != "Recovered static book" {
		t.Fatal("Book route did not recover", w.Code, w.Body.String())
	}
}
