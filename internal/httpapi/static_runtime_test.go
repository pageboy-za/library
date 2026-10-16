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
	handler := server.testHandler(t)
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

func TestSvelteKitStaticRoutesAndCache(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	web := t.TempDir()
	for name, body := range map[string]string{
		"index.html": "catalogue", "book-view/index.html": "book fallback",
		"settings/index.html": "settings", "scan/index.html": "scan",
		"_app/immutable/entry/start.ABC.js": "kit entry", "_app/version.json": `{"version":"new"}`,
		"assets/image.svg": "image",
	} {
		file := filepath.Join(web, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	handler := (&Server{Store: db, Web: web}).testHandler(t)
	for _, test := range []struct {
		method, path string
		status       int
		body         string
		immutable    bool
	}{
		{"GET", "/", 200, "catalogue", false},
		{"GET", "/settings/", 200, "settings", false},
		{"GET", "/scan/", 200, "scan", false},
		{"GET", "/book-view/", 200, "book fallback", false},
		{"GET", "/_app/immutable/entry/start.ABC.js", 200, "kit entry", true},
		{"HEAD", "/_app/immutable/entry/start.ABC.js", 200, "", true},
		{"GET", "/_app/version.json", 200, `{"version":"new"}`, false},
		{"GET", "/assets/", 404, "", false},
		{"GET", "/_app/", 404, "", false},
		{"GET", "/_app/immutable/entry/", 404, "", false},
		{"GET", "/_app/immutable/missing.js", 404, "", false},
		{"GET", "/unknown/", 404, "", false},
		{"GET", "/books/1/unexpected/", 404, "", false},
		{"GET", "/api/missing", 404, "", false},
		{"POST", "/settings/", 405, "", false},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(test.method, "http://localhost:8080"+test.path, nil))
			if res.Code != test.status {
				t.Fatalf("status %d, want %d: %s", res.Code, test.status, res.Body.String())
			}
			if test.body != "" && res.Body.String() != test.body {
				t.Fatalf("body %q, want %q", res.Body.String(), test.body)
			}
			cache := res.Header().Get("Cache-Control")
			if strings.Contains(cache, "immutable") != test.immutable {
				t.Fatalf("unexpected cache policy: %q", cache)
			}
			if test.status == 200 && !test.immutable && cache != "no-cache" {
				t.Fatalf("mutable resource must revalidate: %q", cache)
			}
			if test.path == "/api/missing" && !strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") {
				t.Fatal("unknown API route was swallowed by frontend")
			}
		})
	}
}
