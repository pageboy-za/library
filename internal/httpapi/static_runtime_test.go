package httpapi

import (
	"encoding/json"
	"home-library/internal/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise a prebuilt installation with no generator, source tree or Node.
// Records created after the frontend build must open through the live shell.
func TestPrebuiltRuntimeNewBookAndEdit(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	web := t.TempDir()
	for name, body := range map[string]string{"index.html": "catalogue", "book-view/index.html": "live book shell", "_astro/app.js": "frontend asset"} {
		file := filepath.Join(web, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{Store: db, Web: web}
	handler := server.Handler()
	call := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		return res
	}
	call("GET", "/", "", 200)
	call("GET", "/_astro/app.js", "", 200)
	call("POST", "/api/books", `{"title":"After build","request_key":"prebuilt-create"}`, 200)
	book := call("GET", "/api/books/1", "", 200)
	var saved store.Book
	if err := json.Unmarshal(book.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Title != "After build" {
		t.Fatal(saved.Title)
	}
	if res := call("GET", "/books/1/", "", 200); res.Body.String() != "live book shell" {
		t.Fatal(res.Body.String())
	}
	call("GET", "/books/999/", "", 404)
	call("GET", "/books/1/index.html", "", 303)
	call("GET", "/book/?id=1", "", 308)
	call("PATCH", "/api/books/1", `{"title":"Edited after build","revision":1}`, 200)
	if res := call("GET", "/api/books/1", "", 200); !strings.Contains(res.Body.String(), "Edited after build") {
		t.Fatal(res.Body.String())
	}
	if server.ActiveWrites.Load() != 0 {
		t.Fatal("Static runtime unexpectedly queued publication")
	}
}
