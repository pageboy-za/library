package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemberSelfCheckoutPermissionsPrivacyAndReplay(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	ctx := context.Background()
	book, err := db.Save(ctx, store.Input{Title: "Member lending fixture", RequestKey: "self-http-book"})
	if err != nil {
		t.Fatal(err)
	}
	borrower, err := db.AddBorrower(ctx, "Private fixture name", "Private fixture notes", "self-http-borrower")
	if err != nil {
		t.Fatal(err)
	}
	card := "mbr_0123456789abcdefghijklmnopqrstuv"
	_, err = db.DB.Exec(`INSERT INTO member_registrations(name,email,status,receipt_token_hash,receipt_expires_at,submitted_at,borrower_id,card_code) VALUES('Private fixture name','private@example.test','approved','self-http-receipt',9999999999,CURRENT_TIMESTAMP,?,?)`, borrower, card)
	if err != nil {
		t.Fatal(err)
	}
	server := (&Server{Store: db}).Handler()
	initial := httptest.NewRecorder()
	server.ServeHTTP(initial, httptest.NewRequest("GET", "http://localhost:8080/api/member/registration", nil))
	var receipt struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(initial.Body.Bytes(), &receipt); err != nil || receipt.CSRF == "" {
		t.Fatal(initial.Body.String(), err)
	}
	request := func(path, body, origin string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8080"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if csrf {
			for _, c := range initial.Result().Cookies() {
				r.AddCookie(c)
			}
			r.Header.Set("X-Member-CSRF", receipt.CSRF)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	body := fmt.Sprintf(`{"book_id":%d,"card_code":%q,"request_key":"self-http-checkout"}`, book.ID, card)
	if w := request("/api/member/loans", body, "", false); w.Code != 403 {
		t.Fatal("no CSRF", w.Code)
	}
	if w := request("/api/member/loans", body, "http://evil.example", true); w.Code != 403 {
		t.Fatal("foreign origin", w.Code)
	}
	// Arbitrary borrower or copy IDs cannot be supplied to the member endpoint.
	if w := request("/api/member/loans", strings.TrimSuffix(body, "}")+`,"borrower_id":999}`, "", true); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("/api/member/loans", strings.Replace(body, card, "mbr_abcdefghijklmnopqrstuvwxyz012345", 1), "", true); w.Code != 403 {
		t.Fatal("unknown pass", w.Code, w.Body.String())
	}
	for i := 0; i < 2; i++ {
		w := request("/api/member/loans", body, "", true)
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"borrowed":true}` || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	loans, err := db.Loans(ctx, book.ID)
	if err != nil || len(loans) != 1 || loans[0].BorrowerID != borrower {
		t.Fatal(loans, err)
	}
	// Private staff APIs stay closed even when an approved card is supplied.
	for _, path := range []string{"/api/loans", "/api/members/lookup", "/api/borrowers"} {
		w := request(path, body, "", true)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("staff boundary opened", path, w.Code)
		}
	}
}
