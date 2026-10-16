package httpapi

import (
	"context"
	"home-library/internal/metadata"
	"home-library/internal/store"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type assetPages struct{ site, assets string }

func (p assetPages) Ensure(context.Context) error { return nil }
func (p assetPages) Directory() string            { return p.site }
func (p assetPages) AssetsDirectory() string      { return p.assets }

func TestPreservedIslandAssetsAndNoDirectoryListing(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	assets := t.TempDir()
	os.WriteFile(filepath.Join(assets, "old.123.js"), []byte("old island"), 0600)
	h := (&Server{Store: s, Pages: assetPages{t.TempDir(), assets}}).testHandler(t)
	for _, test := range []struct {
		path   string
		status int
	}{{"/_astro/old.123.js", 200}, {"/_astro/missing.js", 404}, {"/_astro/", 404}} {
		r := httptest.NewRequest("GET", "http://localhost:8080"+test.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s returned%d", test.path, w.Code)
		}
		if test.status == 200 && (w.Body.String() != "old island" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable")) {
			t.Fatal("Preserved bundle missing or not immutable")
		}
	}
}

func TestBoundaryAndScan(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	h := (&Server{Store: s, Metadata: metadata.New(), Web: t.TempDir()}).testHandler(t)
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
	if w := call("GET", "/api/missing", "", ""); w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatal(w)
	}
	if w := call("POST", "/api/books", `{"title":"Book","request_key":"request-token"}`, "https://evil.example"); w.Code != 403 {
		t.Fatal(w)
	}
	body := `{"title":"Matilda","authors":["Roald Dahl"],"isbn":"0140328726","request_key":"request-token"}`
	if w := call("POST", "/api/books", body, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/api/isbn/9780140328721", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"existing"`) || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/api/isbn/9780140328722", "", ""); w.Code != 400 {
		t.Fatal(w)
	}
	if w := call("POST", "/api/books", body+`{}`, ""); w.Code != 400 {
		t.Fatal(w)
	}
}
