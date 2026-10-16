package httpapi

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBibliographyAndSubjectHTTPContract(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	b, err := s.Save(ctx, store.Input{Title: "Fixture", PublicationDate: "2005", RequestKey: "bibliography-http"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDiscovery(ctx, b.ID, store.Discovery{Subjects: []string{"Science"}, Notes: "Never public"}); err != nil {
		t.Fatal(err)
	}
	handler := (&Server{Store: s}).testHandler(t)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/book-subjects", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Science","count":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w := call("PATCH", "/api/books/1/bibliography", `{"first_published_year":1978,"revision":0}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"first_published_year_source_url":""`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("PATCH", "/api/books/1/bibliography", `{"first_published_year":1977,"revision":0}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("PATCH", "/api/books/1/bibliography", `{"first_published_year":0,"revision":1}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/api/books?subject=sCIENCE&sort=first_published_oldest", ""); w.Code != 200 || strings.Contains(w.Body.String(), "Never public") {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Books []store.Book `json:"books"`
		Total int          `json:"total"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Total != 1 || response.Books[0].FirstPublishedYear == nil || *response.Books[0].FirstPublishedYear != 1978 || response.Books[0].BibliographyRevision != 1 || response.Books[0].PublicationDate != "2005" {
		t.Fatal(response, err)
	}
	if w = call("GET", "/api/books?subject=Other", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/api/books?sort=invalid", ""); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("PATCH", "/api/books/1/bibliography", `{"first_published_year":null,"revision":1}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"first_published_year":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMetadataBackfillHTTPIsStrictAndRetrySafe(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	_, err = s.Save(context.Background(), store.Input{Title: "Backfill fixture", RequestKey: "backfill-http"})
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{Store: s}).testHandler(t)
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8080/api/metadata/backfill", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"updates":[{"id":1,"isbn":"","source_id":"","revision":1,"discovery_revision":0,"bibliography_revision":0,"subjects":["Science"],"first_published_year":1978,"first_published_year_source_url":"https://openlibrary.org/works/OL99W"}]}`
	if w := call(body); w.Code != 200 || w.Body.String() != "{\"changed\":1,\"subjects_changed\":1,\"years_changed\":1,\"skipped\":0}\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(body); w.Code != 200 || !strings.Contains(w.Body.String(), `"skipped":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(`{"updates":[{"id":1,"revision":1,"notes":"cannot write notes"}]}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
