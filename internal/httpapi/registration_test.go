package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"home-library/internal/auth"
	"home-library/internal/membership"
	"home-library/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationHTTPPrivacyPermissionsAndCards(t *testing.T) {
	db, e := store.Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	ctx := context.Background()
	access := auth.New(db.DB)
	_, e = access.Bootstrap(ctx, "admin", "Administrator", "fictional-admin-password")
	if e != nil {
		t.Fatal(e)
	}
	librarian, e := access.CreateUser(ctx, "librarian", "Librarian", auth.RoleLibrarian, "fictional-staff-password")
	if e != nil {
		t.Fatal(e)
	}
	staff, _ := access.CreateSession(ctx, librarian.ID)
	server := &Server{Store: db, Access: access, Membership: membership.New(db.DB), SecureCookies: true, PublicURL: "https://library.local"}
	handler := server.Handler()
	cookies := map[string]*http.Cookie{}
	csrf := ""
	request := func(method, path, body string, member, staffCookie, memberCSRF bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if member {
			for _, c := range cookies {
				r.AddCookie(c)
			}
		}
		if memberCSRF {
			r.Header.Set("X-Member-CSRF", csrf)
		}
		if staffCookie {
			r.AddCookie(&http.Cookie{Name: "library_session", Value: staff.Token})
			r.Header.Set("X-CSRF-Token", staff.CSRFToken)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if member {
			for _, c := range w.Result().Cookies() {
				cookies[c.Name] = c
			}
		}
		return w
	}
	w := request("GET", "/api/member/registration", "", true, false, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Registration map[string]any `json:"registration"`
		CSRF         string         `json:"csrf_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	csrf = response.CSRF
	if csrf == "" || response.Registration != nil {
		t.Fatal(w.Body.String())
	}
	if c := cookies[joinCookie]; c == nil || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe form cookie")
	}
	payload := `{"name":"Fictional Member","email":"fictional@example.test","telephone":""}`
	if w = request("POST", "/api/member/registration", payload, true, false, false); w.Code != 403 {
		t.Fatal("signup without CSRF", w.Code)
	}
	if w = request("POST", "/api/member/registration", payload, true, false, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fictional@example.test") || strings.Contains(w.Body.String(), "receipt_token") || strings.Contains(w.Body.String(), "borrower_id") {
		t.Fatal("private registration projection", w.Body.String())
	}
	for _, p := range []string{"/api/member/card/qr", "/api/member/card/apple"} {
		if w = request("GET", p, "", true, false, false); w.Code != 403 {
			t.Fatal("unapproved card", p, w.Code)
		}
	}
	if w = request("GET", "/api/members", "", true, false, false); w.Code != 401 {
		t.Fatal("member read staff queue", w.Code)
	}
	if w = request("POST", "/api/members/1/review", `{"action":"approve"}`, true, false, true); w.Code != 401 {
		t.Fatal("member approval", w.Code)
	}
	if w = request("POST", "/api/members/1/review", `{"action":"approve"}`, false, true, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("GET", "/api/member/registration", "", true, false, false)
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Registration["status"] != "approved" {
		t.Fatal(w.Body.String())
	}
	code := response.Registration["card_code"].(string)
	if w = request("GET", "/api/member/card/qr", "", false, false, false); w.Code != 401 {
		t.Fatal("anonymous card access", w.Code)
	}
	w = request("GET", "/api/member/card/qr", "", true, false, false)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(w.Body.String(), "\x89PNG") {
		t.Fatal("approved QR", w.Code)
	}
	if w = request("GET", "/api/member/card/apple", "", true, false, false); w.Code != 503 {
		t.Fatal("unconfigured Apple", w.Code)
	}
	if w = request("POST", "/api/member/card/google", "{}", true, false, false); w.Code != 403 {
		t.Fatal("Google without CSRF", w.Code)
	}
	if w = request("POST", "/api/member/card/google", "{}", true, false, true); w.Code != 503 {
		t.Fatal("unconfigured Google", w.Code)
	}
	if w = request("POST", "/api/members/lookup", fmt.Sprintf(`{"card_code":%q}`, code), false, true, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// The card code is a public identifier, never a member receipt or staff cookie.
	saved := cookies[memberCookie]
	cookies[memberCookie] = &http.Cookie{Name: memberCookie, Value: code}
	if w = request("GET", "/api/member/card/qr", "", true, false, false); w.Code != 401 {
		t.Fatal("card used as receipt", w.Code)
	}
	cookies[memberCookie] = saved
	if w = request("POST", "/api/members/1/review", `{"action":"suspend"}`, false, true, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request("GET", "/api/member/card/qr", "", true, false, false); w.Code != 403 {
		t.Fatal("suspended card", w.Code)
	}
	if w = request("POST", "/api/members/lookup", fmt.Sprintf(`{"card_code":%q}`, code), false, true, false); w.Code != 404 {
		t.Fatal("suspended lookup", w.Code)
	}
	// Suspension must also prevent a manual checkout using the linked borrower.
	b, e := db.Save(ctx, store.Input{Title: "A book", RequestKey: "membership-book"})
	if e != nil {
		t.Fatal(e)
	}
	members, _ := server.Membership.List(ctx, "suspended")
	if _, e = db.Checkout(ctx, b.Copies[0].ID, *members[0].BorrowerID, "", "suspended-loan"); e == nil {
		t.Fatal("suspended member borrowed using manual ID")
	}
	if w = request("POST", "/api/members/1/review", `{"action":"reactivate"}`, false, true, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = db.Checkout(ctx, b.Copies[0].ID, *members[0].BorrowerID, "", "reactivated-loan"); e != nil {
		t.Fatal(e)
	}
	if w = request("GET", "/api/members/config", "", false, true, false); w.Code != 200 || !strings.Contains(w.Body.String(), "https://library.local/join/") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestRegistrationOriginAndLimits(t *testing.T) {
	for _, u := range []string{"http://library.local", "https://user:secret@library.local", "https://library.local/private", "https://library.local?x=1", "javascript:alert(1)"} {
		if _, e := RegistrationURL(u); e == nil {
			t.Fatal("accepted unsafe origin", u)
		}
	}
	for _, u := range []string{"https://library.local", "http://127.0.0.1:18980"} {
		if _, e := RegistrationURL(u); e != nil {
			t.Fatal(u, e)
		}
	}
	var limiter registrationLimiter
	for i := 0; i < 5; i++ {
		if !limiter.allow("form") {
			t.Fatal(i)
		}
	}
	if limiter.allow("form") {
		t.Fatal("no form limit")
	}
	for i := 0; i < 5; i++ {
		if !limiter.allow(fmt.Sprint(i)) {
			t.Fatal(i)
		}
	}
	if limiter.allow("extra") {
		t.Fatal("no global limit")
	}
}
