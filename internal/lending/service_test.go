package lending

import (
	"context"
	"encoding/json"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPCheckoutReturnAndValidation(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	book, e := s.Save(context.Background(), store.Input{Title: "HTTP lending", Authors: []string{}, RequestKey: "http-lending-book"})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	(&Service{Store: s}).Register(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		mux.ServeHTTP(w, r)
		return w
	}
	wBad := httptest.NewRecorder()
	mux.ServeHTTP(wBad, httptest.NewRequest("POST", "/api/loans", strings.NewReader("{}")))
	if wBad.Code != 415 {
		t.Fatal("Missing JSON content type accepted")
	}
	w := request("POST", "/api/borrowers", `{"name":"Ada","notes":"","request_key":"http-borrower"}`)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var b struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &b)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Borrower response cacheable")
	}
	edited := request("PATCH", "/api/borrowers/1", `{"name":"Ada Lovelace","notes":"","expected_name":"Ada","expected_notes":""}`)
	if edited.Code != 200 {
		t.Fatal(edited.Body.String())
	}
	stale := request("PATCH", "/api/borrowers/1", `{"name":"Clobber","notes":"","expected_name":"Ada","expected_notes":""}`)
	if stale.Code != 409 {
		t.Fatal("Stale borrower edit accepted")
	}

	raw, _ := json.Marshal(map[string]any{"copy_id": book.Copies[0].ID, "borrower_id": b.ID, "request_key": "http-checkout"})
	w = request("POST", "/api/loans", string(raw))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var loan struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &loan)
	w = request("GET", "/api/books/1/loans", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"borrower":"Ada Lovelace"`) {
		t.Fatal(w.Body.String())
	}
	listed := request("GET", "/api/loans?borrower_id=1", "")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"total":1`) {
		t.Fatal(listed.Body.String())
	}
	invalid := request("GET", "/api/loans?page=-1", "")
	if invalid.Code != 400 {
		t.Fatal("Bad loan page accepted")
	}
	w = request("POST", "/api/loans/1/return", `{"request_key":"http-return"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, body := range []string{`{"copy_id":"bad"}`, `{"unknown":1}`, `{} {}`} {
		w = request("POST", "/api/loans", body)
		if w.Code != 400 {
			t.Fatalf("Invalid body %q got %d", body, w.Code)
		}
	}
	w = request("POST", "/api/loans", `{"copy_id":1,"borrower_id":1,"due_date":"yesterday","request_key":"invalid-date"}`)
	if w.Code != 409 {
		t.Fatal("Invalid due date accepted")
	}
}

func TestCopyArchiveHTTP(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	book, _ := s.Save(ctx, store.Input{Title: "Archive API", Authors: []string{}, RequestKey: "archive-api-book"})
	person, _ := s.AddBorrower(ctx, "Ada", "", "archive-api-person")
	loan, _ := s.Checkout(ctx, book.Copies[0].ID, person, "", "archive-api-loan")
	mux := http.NewServeMux()
	(&Service{Store: s}).Register(mux)
	archive := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/copies/1/archive", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
		return w
	}
	body := `{"revision":1,"request_key":"archive-api-copy"}`
	w := archive(body)
	if w.Code != 409 {
		t.Fatal("Archived active loan via HTTP")
	}
	s.ReturnLoan(ctx, loan, "archive-api-return")
	w = archive(body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Cacheable archive response")
	}
	w = archive(body)
	if w.Code != 200 {
		t.Fatal("Retry did not succeed")
	}
	w = archive(`{"revision":1,"request_key":"another-api-archive"}`)
	if w.Code != 409 {
		t.Fatal("Stale archive accepted")
	}
}
