package locations

import (
	"context"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocationHTTPValidationAndGuard(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	s.AddShelf(ctx, "Study", "One")
	mux := http.NewServeMux()
	(&Service{Store: s}).Register(mux)
	request := func(method, path, body, content string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if content != "" {
			r.Header.Set("Content-Type", content)
		}
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/api/bookcases", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Study") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	for _, test := range []struct {
		body, content string
		status        int
	}{{`{}`, "", 415}, {`{"unknown":1}`, "application/json", 400}, {`{} {}`, "application/json", 400}, {`{"name":1}`, "application/json", 400}} {
		w = request("PATCH", "/api/shelves/1", test.body, test.content)
		if w.Code != test.status {
			t.Fatalf("Bad body got %d", w.Code)
		}
	}
	w = request("PATCH", "/api/shelves/1", `{"name":"Top","expected_name":"One","expected_revision":1,"request_key":"http-shelf-rename"}`, "application/json")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	sh, _ := s.Shelves(ctx)
	_, e = s.Save(ctx, store.Input{Title: "Occupied", Authors: []string{}, ShelfID: &sh[0].ID, RequestKey: "http-location-book"})
	if e != nil {
		t.Fatal(e)
	}
	w = request("POST", "/api/shelves/1/remove", `{"expected_name":"Top","expected_revision":2,"request_key":"http-shelf-remove"}`, "application/json")
	if w.Code != 409 {
		t.Fatal("Occupied shelf removed")
	}
	w = request("PATCH", "/api/bookcases/1", `{"name":"Office","expected_name":"Study","expected_revision":1,"request_key":"http-case-rename"}`, "application/json")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request("POST", "/api/bookcases/1/remove", `{"expected_name":"Office","expected_revision":2,"request_key":"http-case-remove"}`, "application/json")
	if w.Code != 409 {
		t.Fatal("Occupied case removed")
	}
}
